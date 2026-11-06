package deps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

type resolver struct {
	repos       []Repository
	client      *client
	cache       *store.Store
	logger      *slog.Logger
	tempDir     string
	poms        map[string]*resolvedPOM
	modules     map[string]*moduleMetadata
	loading     map[string]struct{}
	selected    map[string]string
	constraints map[string]versionConstraint
	attributes  map[string]string
}

type versionConstraint struct {
	requires string
	strictly string
	prefers  string
	rejects  map[string]struct{}
}

type resolvedArtifact struct {
	Coordinate
	Scope Scope
}

func newResolver(repos []Repository, cache *store.Store, logger *slog.Logger) *resolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &resolver{
		repos:       repos,
		client:      newClient(),
		cache:       cache,
		logger:      logger,
		poms:        make(map[string]*resolvedPOM),
		modules:     make(map[string]*moduleMetadata),
		loading:     make(map[string]struct{}),
		selected:    make(map[string]string),
		constraints: make(map[string]versionConstraint),
	}
}

func (r *resolver) loadPOM(ctx context.Context, coord Coordinate, depth int) (*resolvedPOM, error) {
	if depth > 10 {
		return nil, fmt.Errorf("POM %s: parent depth exceeds 10", coord)
	}
	key := coord.Group + ":" + coord.Artifact + ":" + coord.Version
	if cached, ok := r.poms[key]; ok {
		return cached, nil
	}
	if _, ok := r.loading[key]; ok {
		return nil, fmt.Errorf("POM %s: parent or BOM cycle", coord)
	}
	r.loading[key] = struct{}{}
	defer delete(r.loading, key)

	data, err := r.fetchPOM(ctx, coord)
	if err != nil {
		return nil, err
	}
	raw, err := parsePOM(data)
	if err != nil {
		return nil, fmt.Errorf("POM %s: %w", coord, err)
	}
	resolved, err := r.resolvePOM(ctx, raw, coord, depth)
	if err != nil {
		return nil, err
	}
	r.poms[key] = resolved
	return resolved, nil
}

func (r *resolver) fetchPOM(ctx context.Context, coord Coordinate) ([]byte, error) {
	for _, repo := range r.repos {
		data, err := r.client.fetchBytes(ctx, repo, coord.POMPath())
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("POM %s from %s: %w", coord, repo.ID, err)
		}
		if err := r.verifyBytesChecksum(ctx, repo, coord.POMPath(), data); err != nil {
			return nil, err
		}
		if r.cache != nil {
			if _, err := r.cache.PutBytes(data); err != nil {
				r.logger.Warn("cache POM", "coordinate", coord.String(), "error", err)
			}
		}
		return data, nil
	}
	return nil, fmt.Errorf("POM %s: %w", coord, ErrNotFound)
}

func (r *resolver) resolvePOM(ctx context.Context, raw *pomProject, coord Coordinate, depth int) (*resolvedPOM, error) {
	rawProperties := projectProperties(raw.GroupID, raw.ArtifactID, raw.Version, raw.Properties)

	var parent *resolvedPOM
	if raw.Parent.ArtifactID != "" {
		parentCoord := Coordinate{
			Group:     interpolate(raw.Parent.GroupID, rawProperties),
			Artifact:  interpolate(raw.Parent.ArtifactID, rawProperties),
			Version:   interpolate(raw.Parent.Version, rawProperties),
			Extension: "pom",
		}
		if parentCoord.Group == "" || parentCoord.Artifact == "" || parentCoord.Version == "" {
			return nil, fmt.Errorf("POM %s: incomplete parent", coord)
		}
		var err error
		parent, err = r.loadPOM(ctx, parentCoord, depth+1)
		if err != nil {
			return nil, fmt.Errorf("POM %s: parent %s: %w", coord, parentCoord, err)
		}
	}

	group := firstNonEmpty(interpolate(raw.GroupID, rawProperties), parentGroup(parent))
	artifact := firstNonEmpty(interpolate(raw.ArtifactID, rawProperties), parentArtifact(parent))
	version := firstNonEmpty(interpolate(raw.Version, rawProperties), parentVersion(parent))
	// Maven packaging is not inherited from the parent POM. Only the child's
	// own packaging counts; otherwise a parent BOM would hide a child jar.
	packaging := firstNonEmpty(interpolate(raw.Packaging, rawProperties), "jar")

	properties := make(map[string]string)
	if parent != nil {
		for key, value := range parent.Properties {
			properties[key] = value
		}
	}
	for key, value := range raw.Properties {
		properties[key] = value
	}
	properties = projectProperties(group, artifact, version, properties)

	dependencies := resolveDependencies(raw.Dependencies.Items, properties)
	if parent != nil {
		dependencies = mergeDependencies(parent.Dependencies, dependencies)
	}

	managed := resolveDependencies(raw.DependencyManagement.Items, properties)
	if parent != nil {
		managed = mergeDependencies(parent.DependencyManagement, managed)
	}
	managed, err := r.resolveImports(ctx, managed, depth)
	if err != nil {
		return nil, fmt.Errorf("POM %s: %w", coord, err)
	}

	return &resolvedPOM{
		GroupID:              group,
		ArtifactID:           artifact,
		Version:              version,
		Packaging:            packaging,
		Properties:           properties,
		DependencyManagement: managed,
		Dependencies:         dependencies,
	}, nil
}

func (r *resolver) resolveImports(ctx context.Context, managed []pomDependency, depth int) ([]pomDependency, error) {
	var imports []pomDependency
	var direct []pomDependency

	for _, dependency := range managed {
		if dependency.Scope != "import" || dependency.Type != "pom" {
			direct = append(direct, dependency)
			continue
		}

		version, err := normalizeVersion(dependency.Version)
		if err != nil {
			return nil, fmt.Errorf("BOM import: %w", err)
		}
		coord := Coordinate{
			Group:     dependency.GroupID,
			Artifact:  dependency.ArtifactID,
			Version:   version,
			Extension: "pom",
		}
		if coord.Group == "" || coord.Artifact == "" || coord.Version == "" {
			return nil, fmt.Errorf("BOM import: incomplete coordinate")
		}
		pom, err := r.loadPOM(ctx, coord, depth+1)
		if err != nil {
			return nil, fmt.Errorf("BOM %s: %w", coord, err)
		}
		imports = mergeDependencies(imports, pom.DependencyManagement)
	}
	return mergeDependencies(imports, direct), nil
}

func (r *resolver) resolveGraph(ctx context.Context, roots []Dependency) ([]resolvedArtifact, error) {
	for _, root := range roots {
		if root.Group == "" || root.Artifact == "" || root.Version == "" {
			return nil, fmt.Errorf("root dependency %s: incomplete coordinate", root)
		}
		if root.Scope == "" {
			root.Scope = ScopeCompile
		}
		if !root.Scope.Valid() {
			return nil, fmt.Errorf("root dependency %s: unknown scope %q", root, root.Scope)
		}
		if _, err := r.selectVersion(root.Key(), root.Version); err != nil {
			return nil, err
		}
	}

	for pass := 0; pass < 100; pass++ {
		artifacts := make(map[string]*resolvedArtifact)
		changed := false
		for _, root := range roots {
			if err := r.traverse(ctx, root.Coordinate, root.Scope, nil, root.Exclusions, 0, make(map[string]struct{}), artifacts, &changed); err != nil {
				return nil, err
			}
		}
		if !changed {
			return sortedArtifacts(artifacts), nil
		}
	}
	return nil, fmt.Errorf("dependency resolution did not converge")
}

func (r *resolver) traverse(ctx context.Context, coord Coordinate, scope Scope, inherited map[string]struct{}, own []Exclusion, depth int, path map[string]struct{}, artifacts map[string]*resolvedArtifact, changed *bool) error {
	if depth > 100 {
		return fmt.Errorf("dependency graph depth exceeds 100 at %s", coord)
	}
	if excluded(coord.Group, coord.Artifact, inherited) {
		return nil
	}

	key := coord.Key()
	versionChanged, err := r.selectVersion(key, coord.Version)
	if err != nil {
		return err
	}
	if versionChanged {
		*changed = true
	}
	selected := r.selected[key]
	coord.Version = selected

	pathKey := key + "@" + selected
	if _, ok := path[pathKey]; ok {
		return nil
	}
	path[pathKey] = struct{}{}
	defer delete(path, pathKey)

	var variant *moduleVariant
	moduleCoord := coord
	for redirects := 0; redirects < 4; redirects++ {
		metadata, err := r.loadModuleMetadata(ctx, moduleCoord)
		if err != nil {
			return err
		}
		if metadata == nil {
			break
		}
		selectedVariant := r.selectModuleVariant(metadata)
		if selectedVariant == nil {
			break
		}
		if selectedVariant.AvailableAt != nil {
			target := selectedVariant.AvailableAt
			if target.Group == "" || target.Module == "" || target.Version == "" {
				return fmt.Errorf("module metadata %s: incomplete available-at", coord)
			}
			moduleCoord = Coordinate{
				Group:     target.Group,
				Artifact:  target.Module,
				Version:   target.Version,
				Extension: moduleCoord.Extension,
			}
			continue
		}
		variant = selectedVariant
		break
	}

	if variant != nil {
		artifact, ok := moduleArtifact(variant, moduleCoord)
		if ok {
			broadenArtifact(artifacts, artifact, scope)
		} else {
			pom, err := r.loadPOM(ctx, moduleCoord, 0)
			if err != nil {
				return err
			}
			extension := moduleCoord.Extension
			if extension == "" || extension == "jar" {
				if pom.Packaging == "aar" {
					extension = "aar"
				} else if extension == "" {
					extension = "jar"
				}
			}
			if pom.Packaging != "pom" {
				broadenArtifact(artifacts, Coordinate{
					Group:      moduleCoord.Group,
					Artifact:   moduleCoord.Artifact,
					Version:    moduleCoord.Version,
					Classifier: moduleCoord.Classifier,
					Extension:  extension,
				}, scope)
			}
		}
		if scope != ScopeCompile && scope != ScopeRuntime {
			return nil
		}
		r.recordVariantConstraints(variant)
		dependencies, err := r.moduleDependencies(variant)
		if err != nil {
			return fmt.Errorf("module metadata %s: %w", coord, err)
		}
		childExclusions := mergeExclusions(inherited, own)
		for _, child := range dependencies {
			if excluded(child.Group, child.Artifact, childExclusions) {
				continue
			}
			combined, ok := combineScope(scope, child.Scope)
			if !ok {
				continue
			}
			if err := r.traverse(ctx, child.Coordinate, combined, childExclusions, child.Exclusions, depth+1, path, artifacts, changed); err != nil {
				return err
			}
		}
		return nil
	}

	pom, err := r.loadPOM(ctx, coord, 0)
	if err != nil {
		return err
	}

	extension := coord.Extension
	if extension == "" {
		extension = "jar"
	}
	if extension == "jar" && pom.Packaging == "aar" {
		extension = "aar"
	}
	// a classifier requests a real file even when the pom is a bill of materials
	if pom.Packaging != "pom" || coord.Classifier != "" || extension != "jar" {
		artifactCoord := Coordinate{
			Group:      coord.Group,
			Artifact:   coord.Artifact,
			Version:    selected,
			Classifier: coord.Classifier,
			Extension:  extension,
		}
		broadenArtifact(artifacts, artifactCoord, scope)
	}

	if scope != ScopeCompile && scope != ScopeRuntime {
		return nil
	}

	r.recordManagedConstraints(pom.DependencyManagement)
	managed := managedMap(pom.DependencyManagement)
	childExclusions := mergeExclusions(inherited, own)
	for _, raw := range pom.Dependencies {
		child, optional, err := r.childDependency(raw, managed)
		if err != nil {
			return fmt.Errorf("dependency %s: %w", coord, err)
		}
		if optional {
			continue
		}
		if excluded(child.Group, child.Artifact, childExclusions) {
			continue
		}
		combined, ok := combineScope(scope, child.Scope)
		if !ok {
			continue
		}
		if err := r.traverse(ctx, child.Coordinate, combined, childExclusions, child.Exclusions, depth+1, path, artifacts, changed); err != nil {
			return err
		}
	}
	return nil
}

func (r *resolver) childDependency(raw pomDependency, managed map[string]pomDependency) (Dependency, bool, error) {
	version, err := normalizeVersion(raw.Version)
	if err != nil {
		return Dependency{}, false, err
	}
	dep := Dependency{
		Coordinate: Coordinate{
			Group:      raw.GroupID,
			Artifact:   raw.ArtifactID,
			Version:    version,
			Classifier: raw.Classifier,
			Extension:  extensionFromType(raw.Type),
		},
		Scope:      Scope(raw.Scope),
		Exclusions: convertExclusions(raw.Exclusions),
	}
	if dep.Version == "" {
		if managedDep, ok := managed[dependencyKey(raw)]; ok {
			dep.Version, err = normalizeVersion(managedDep.Version)
			if err != nil {
				return Dependency{}, false, err
			}
			if dep.Scope == "" {
				dep.Scope = Scope(managedDep.Scope)
			}
			if len(dep.Exclusions) == 0 {
				dep.Exclusions = convertExclusions(managedDep.Exclusions)
			}
		}
	}
	if dep.Scope == "" {
		dep.Scope = ScopeCompile
	}
	if dep.Group == "" || dep.Artifact == "" {
		return Dependency{}, false, fmt.Errorf("incomplete coordinate")
	}
	if dep.Version == "" {
		return Dependency{}, false, fmt.Errorf("%s:%s: version is empty", dep.Group, dep.Artifact)
	}
	if !dep.Scope.Valid() {
		return Dependency{}, false, fmt.Errorf("%s: unknown scope %q", dep, dep.Scope)
	}
	return dep, raw.Optional == "true", nil
}

func (r *resolver) selectVersion(key, version string) (bool, error) {
	constraint := r.constraints[moduleKey(key)]
	candidate := version
	for _, required := range []string{constraint.requires, constraint.prefers} {
		if required != "" && compareSelectedVersions(required, candidate) > 0 {
			candidate = required
		}
	}
	current, ok := r.selected[key]
	if ok && compareSelectedVersions(current, candidate) > 0 {
		candidate = current
	}
	if constraint.strictly != "" {
		if ok && compareSelectedVersions(current, constraint.strictly) > 0 {
			return false, fmt.Errorf("version conflict for %s: %s requires %s but strict constraint is %s", key, key, current, constraint.strictly)
		}
		if compareSelectedVersions(version, constraint.strictly) > 0 {
			return false, fmt.Errorf("version conflict for %s: requested %s but strict constraint is %s", key, version, constraint.strictly)
		}
		candidate = constraint.strictly
	}
	if _, rejected := constraint.rejects[candidate]; rejected {
		return false, fmt.Errorf("version conflict for %s: %s is rejected", key, candidate)
	}
	if !ok || compareSelectedVersions(candidate, current) != 0 {
		r.selected[key] = candidate
		return true, nil
	}
	return false, nil
}

func moduleKey(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 2 {
		return key
	}
	return parts[0] + ":" + parts[1]
}

func (r *resolver) recordConstraint(key string, version moduleVersion) {
	if version.value() == "" && len(version.Rejects) == 0 {
		return
	}
	constraint := r.constraints[key]
	if normalized, err := normalizeVersion(version.Requires); err == nil && normalized != "" {
		constraint.requires = maxVersion(constraint.requires, normalized)
	}
	if normalized, err := normalizeVersion(version.Prefers); err == nil && normalized != "" {
		constraint.prefers = maxVersion(constraint.prefers, normalized)
	}
	if normalized, err := normalizeVersion(version.Strictly); err == nil && normalized != "" {
		constraint.strictly = maxVersion(constraint.strictly, normalized)
	}
	if len(version.Rejects) > 0 {
		if constraint.rejects == nil {
			constraint.rejects = make(map[string]struct{}, len(version.Rejects))
		}
		for _, rejected := range version.Rejects {
			constraint.rejects[rejected] = struct{}{}
		}
	}
	r.constraints[key] = constraint
}

func maxVersion(current, candidate string) string {
	if candidate == "" {
		return current
	}
	if current == "" || compareSelectedVersions(candidate, current) > 0 {
		return candidate
	}
	return current
}

// compareSelectedVersions keeps version selection on a consistent order. Some
// qualifier pairs compare greater in both directions in CompareVersions, which
// would otherwise make the resolver oscillate between two versions.
func compareSelectedVersions(a, b string) int {
	forward := CompareVersions(a, b)
	if forward == 0 {
		return 0
	}
	reverse := CompareVersions(b, a)
	if reverse == 0 || (forward > 0) != (reverse < 0) {
		return forward
	}
	return strings.Compare(a, b)
}

func (r *resolver) recordManagedConstraints(values []pomDependency) {
	for _, value := range values {
		if value.GroupID == "" || value.ArtifactID == "" || value.Version == "" {
			continue
		}
		version, err := normalizeVersion(value.Version)
		if err != nil {
			continue
		}
		r.recordConstraint(value.GroupID+":"+value.ArtifactID, moduleVersion{Requires: version})
	}
}

func managedMap(values []pomDependency) map[string]pomDependency {
	result := make(map[string]pomDependency, len(values))
	for _, value := range values {
		result[dependencyKey(value)] = value
	}
	return result
}

func extensionFromType(kind string) string {
	if kind == "" {
		return "jar"
	}
	return kind
}

func convertExclusions(values []pomExclusion) []Exclusion {
	result := make([]Exclusion, 0, len(values))
	for _, value := range values {
		result = append(result, Exclusion{Group: value.GroupID, Artifact: value.ArtifactID})
	}
	return result
}

func mergeExclusions(base map[string]struct{}, extra []Exclusion) map[string]struct{} {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[string]struct{}, len(base)+len(extra))
	for key := range base {
		merged[key] = struct{}{}
	}
	for _, exclusion := range extra {
		merged[exclusion.Group+":"+exclusion.Artifact] = struct{}{}
	}
	return merged
}

func excluded(group, artifact string, exclusions map[string]struct{}) bool {
	_, ok := exclusions[group+":"+artifact]
	return ok
}

func combineScope(parent, child Scope) (Scope, bool) {
	if child == "" {
		child = ScopeCompile
	}
	switch parent {
	case ScopeCompile:
		switch child {
		case ScopeCompile:
			return ScopeCompile, true
		case ScopeRuntime:
			return ScopeRuntime, true
		default:
			return "", false
		}
	case ScopeRuntime:
		switch child {
		case ScopeCompile, ScopeRuntime:
			return ScopeRuntime, true
		default:
			return "", false
		}
	default:
		return "", false
	}
}

func scopeRank(scope Scope) int {
	switch scope {
	case ScopeCompile:
		return 0
	case ScopeRuntime:
		return 1
	case ScopeProvided:
		return 2
	case ScopeTest:
		return 3
	default:
		return 4
	}
}

func broadenArtifact(artifacts map[string]*resolvedArtifact, coord Coordinate, scope Scope) bool {
	key := coord.Key()
	current, ok := artifacts[key]
	if !ok {
		artifacts[key] = &resolvedArtifact{Coordinate: coord, Scope: scope}
		return false
	}
	if scopeRank(scope) < scopeRank(current.Scope) {
		current.Scope = scope
		return true
	}
	return false
}

func sortedArtifacts(values map[string]*resolvedArtifact) []resolvedArtifact {
	result := make([]resolvedArtifact, 0, len(values))
	for _, value := range values {
		result = append(result, *value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Scope != result[j].Scope {
			return scopeRank(result[i].Scope) < scopeRank(result[j].Scope)
		}
		a := result[i].Coordinate
		b := result[j].Coordinate
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Artifact != b.Artifact {
			return a.Artifact < b.Artifact
		}
		if a.Classifier != b.Classifier {
			return a.Classifier < b.Classifier
		}
		return a.Extension < b.Extension
	})
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parentGroup(parent *resolvedPOM) string {
	if parent == nil {
		return ""
	}
	return parent.GroupID
}

func parentArtifact(parent *resolvedPOM) string {
	if parent == nil {
		return ""
	}
	return parent.ArtifactID
}

func parentVersion(parent *resolvedPOM) string {
	if parent == nil {
		return ""
	}
	return parent.Version
}
