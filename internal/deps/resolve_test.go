package deps

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestLoadPOM(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "app", "1.0"): projectXML("com.example", "app", "1.0",
			`<dependencies>`+dependencyXML("com.example", "lib", "1.0")+`</dependencies>`),
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0", ""),
	}
	resolver := newTestResolver(t, poms)

	project, err := resolver.loadPOM(context.Background(), Coordinate{
		Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar",
	}, 0)
	if err != nil {
		t.Fatalf("loadPOM() error = %v", err)
	}
	if len(project.Dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(project.Dependencies))
	}
	if project.Dependencies[0].ArtifactID != "lib" {
		t.Errorf("dependency = %q, want lib", project.Dependencies[0].ArtifactID)
	}
}

func TestLoadPOMParent(t *testing.T) {
	parent := `<project>
  <groupId>com.example</groupId>
  <artifactId>parent</artifactId>
  <version>1.0</version>
  <packaging>pom</packaging>
  <properties><lib.version>2.0</lib.version></properties>
  <dependencies>` + dependencyXML("com.example", "lib", "${lib.version}") + `</dependencies>
</project>`
	child := `<project>
  <parent><groupId>com.example</groupId><artifactId>parent</artifactId><version>1.0</version></parent>
  <artifactId>app</artifactId>
  <dependencies>` + dependencyXML("com.example", "other", "1.0") + `</dependencies>
</project>`

	poms := map[string]string{
		pomPath("com.example", "parent", "1.0"): parent,
		pomPath("com.example", "app", "1.0"):    child,
	}
	resolver := newTestResolver(t, poms)

	project, err := resolver.loadPOM(context.Background(), Coordinate{
		Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar",
	}, 0)
	if err != nil {
		t.Fatalf("loadPOM() error = %v", err)
	}
	if project.GroupID != "com.example" || project.Version != "1.0" {
		t.Errorf("coordinates = %s:%s, want com.example:1.0", project.GroupID, project.Version)
	}
	if len(project.Dependencies) != 2 {
		t.Fatalf("dependencies = %d, want 2", len(project.Dependencies))
	}
	if project.Dependencies[0].ArtifactID != "lib" || project.Dependencies[0].Version != "2.0" {
		t.Errorf("inherited dependency = %+v, want lib 2.0", project.Dependencies[0])
	}
}

func TestLoadPOMDoesNotInheritPackaging(t *testing.T) {
	parent := `<project>
  <groupId>com.example</groupId>
  <artifactId>parent</artifactId>
  <version>1.0</version>
  <packaging>pom</packaging>
</project>`
	child := `<project>
  <parent><groupId>com.example</groupId><artifactId>parent</artifactId><version>1.0</version></parent>
  <artifactId>lib</artifactId>
</project>`

	poms := map[string]string{
		pomPath("com.example", "parent", "1.0"): parent,
		pomPath("com.example", "lib", "1.0"):    child,
	}
	resolver := newTestResolver(t, poms)

	project, err := resolver.loadPOM(context.Background(), Coordinate{
		Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar",
	}, 0)
	if err != nil {
		t.Fatalf("loadPOM() error = %v", err)
	}
	if project.Packaging != "jar" {
		t.Errorf("Packaging = %q, want jar", project.Packaging)
	}
}

func TestLoadPOMBOM(t *testing.T) {
	bom := `<project>
  <groupId>com.example</groupId>
  <artifactId>bom</artifactId>
  <version>1.0</version>
  <packaging>pom</packaging>
  <dependencyManagement><dependencies>` + dependencyXML("com.example", "lib", "2.0") + `</dependencies></dependencyManagement>
</project>`
	app := `<project>
  <groupId>com.example</groupId>
  <artifactId>app</artifactId>
  <version>1.0</version>
  <dependencyManagement><dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>bom</artifactId>
      <version>1.0</version>
      <type>pom</type>
      <scope>import</scope>
    </dependency>
  </dependencies></dependencyManagement>
  <dependencies>
    <dependency><groupId>com.example</groupId><artifactId>lib</artifactId></dependency>
  </dependencies>
</project>`

	poms := map[string]string{
		pomPath("com.example", "bom", "1.0"): bom,
		pomPath("com.example", "app", "1.0"): app,
	}
	resolver := newTestResolver(t, poms)

	project, err := resolver.loadPOM(context.Background(), Coordinate{
		Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar",
	}, 0)
	if err != nil {
		t.Fatalf("loadPOM() error = %v", err)
	}
	managed := managedMap(project.DependencyManagement)
	entry, ok := managed["com.example:lib:jar:"]
	if !ok {
		t.Fatalf("managed lib missing: %+v", project.DependencyManagement)
	}
	if entry.Version != "2.0" {
		t.Errorf("managed lib version = %q, want 2.0", entry.Version)
	}
	if len(project.Dependencies) != 1 || project.Dependencies[0].Version != "" {
		t.Errorf("dependency = %+v, want an unresolved version", project.Dependencies)
	}
}

func TestResolveSimple(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0", ""),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
	if artifacts[0].Coordinate.Version != "1.0" || artifacts[0].Scope != ScopeCompile {
		t.Errorf("artifact = %+v, want lib 1.0 compile", artifacts[0])
	}
}

func TestResolveTransitive(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "app", "1.0"): projectXML("com.example", "app", "1.0",
			`<dependencies>`+dependencyXML("com.example", "lib", "1.0")+`</dependencies>`),
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0",
			`<dependencies>`+dependencyXML("com.example", "util", "1.0")+`</dependencies>`),
		pomPath("com.example", "util", "1.0"): projectXML("com.example", "util", "1.0", ""),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("artifacts = %d, want 3", len(artifacts))
	}
}

func TestResolveHighestVersion(t *testing.T) {
	app := `<project>
  <groupId>com.example</groupId><artifactId>app</artifactId><version>1.0</version>
  <dependencies>
    ` + dependencyXML("com.example", "lib", "1.0") + `
    ` + dependencyXML("com.example", "util", "1.0") + `
  </dependencies>
</project>`
	lib := `<project>
  <groupId>com.example</groupId><artifactId>lib</artifactId><version>1.0</version>
  <dependencies>` + dependencyXML("com.example", "util", "2.0") + `</dependencies>
</project>`

	poms := map[string]string{
		pomPath("com.example", "app", "1.0"):  app,
		pomPath("com.example", "lib", "1.0"):  lib,
		pomPath("com.example", "util", "2.0"): projectXML("com.example", "util", "2.0", ""),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	for _, artifact := range artifacts {
		if artifact.Coordinate.Artifact == "util" && artifact.Coordinate.Version != "2.0" {
			t.Errorf("util version = %q, want 2.0", artifact.Coordinate.Version)
		}
	}
}

func TestResolveExclusion(t *testing.T) {
	app := `<project>
  <groupId>com.example</groupId><artifactId>app</artifactId><version>1.0</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId><artifactId>lib</artifactId><version>1.0</version>
      <exclusions><exclusion><groupId>com.example</groupId><artifactId>util</artifactId></exclusion></exclusions>
    </dependency>
  </dependencies>
</project>`
	poms := map[string]string{
		pomPath("com.example", "app", "1.0"): app,
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0",
			`<dependencies>`+dependencyXML("com.example", "util", "1.0")+`</dependencies>`),
		pomPath("com.example", "util", "1.0"): projectXML("com.example", "util", "1.0", ""),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	for _, artifact := range artifacts {
		if artifact.Coordinate.Artifact == "util" {
			t.Error("excluded dependency was resolved")
		}
	}
}

func TestResolveOptional(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "lib", "1.0"): `<project>
  <groupId>com.example</groupId><artifactId>lib</artifactId><version>1.0</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId><artifactId>util</artifactId><version>1.0</version>
      <optional>true</optional>
    </dependency>
  </dependencies>
</project>`,
		pomPath("com.example", "util", "1.0"): projectXML("com.example", "util", "1.0", ""),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
}

func TestResolveScopes(t *testing.T) {
	app := `<project>
  <groupId>com.example</groupId><artifactId>app</artifactId><version>1.0</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId><artifactId>lib</artifactId><version>1.0</version>
      <scope>runtime</scope>
    </dependency>
  </dependencies>
</project>`
	poms := map[string]string{
		pomPath("com.example", "app", "1.0"): app,
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0",
			`<dependencies>`+dependencyXML("com.example", "util", "1.0")+`</dependencies>`),
		pomPath("com.example", "util", "1.0"): projectXML("com.example", "util", "1.0", ""),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	scopes := make(map[string]Scope)
	for _, artifact := range artifacts {
		scopes[artifact.Coordinate.Artifact] = artifact.Scope
	}
	if scopes["lib"] != ScopeRuntime || scopes["util"] != ScopeRuntime {
		t.Errorf("scopes = %v, want lib and util runtime", scopes)
	}
}

func TestResolveCycle(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "a", "1.0"): projectXML("com.example", "a", "1.0",
			`<dependencies>`+dependencyXML("com.example", "b", "1.0")+`</dependencies>`),
		pomPath("com.example", "b", "1.0"): projectXML("com.example", "b", "1.0",
			`<dependencies>`+dependencyXML("com.example", "a", "1.0")+`</dependencies>`),
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "a", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(artifacts))
	}
}

func newTestResolver(t *testing.T, poms map[string]string) *resolver {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, ok := poms[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)

	cache, err := store.Open(t.TempDir(), filepath.Join(t.TempDir(), "cache"), store.Options{Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	return newResolver([]Repository{{ID: "test", URL: server.URL}}, cache, discardLogger())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func pomPath(group, artifact, version string) string {
	return "/" + strings.ReplaceAll(group, ".", "/") + "/" + artifact + "/" + version + "/" + artifact + "-" + version + ".pom"
}

func projectXML(group, artifact, version, body string) string {
	return `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>` + group + `</groupId>
  <artifactId>` + artifact + `</artifactId>
  <version>` + version + `</version>
  ` + body + `
</project>`
}

func dependencyXML(group, artifact, version string) string {
	return `<dependency>
    <groupId>` + group + `</groupId>
    <artifactId>` + artifact + `</artifactId>
    <version>` + version + `</version>
  </dependency>`
}

func TestResolveClassifierWithPOMPackaging(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "platform", "1.0"): `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>platform</artifactId>
  <version>1.0</version>
  <packaging>pom</packaging>
</project>`,
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{
			Group: "com.example", Artifact: "platform", Version: "1.0",
			Classifier: "natives", Extension: "jar",
		},
		Scope: ScopeRuntime,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
	if artifacts[0].Classifier != "natives" || artifacts[0].Scope != ScopeRuntime {
		t.Errorf("artifact = %+v", artifacts[0])
	}
}

func TestResolveBOMAddsNoArtifact(t *testing.T) {
	poms := map[string]string{
		pomPath("com.example", "bom", "1.0"): `<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>bom</artifactId>
  <version>1.0</version>
  <packaging>pom</packaging>
</project>`,
	}
	resolver := newTestResolver(t, poms)

	artifacts, err := resolver.resolveGraph(context.Background(), []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "bom", Version: "1.0"},
		Scope:      ScopeCompile,
	}})
	if err != nil {
		t.Fatalf("resolveGraph() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("artifacts = %d, want 0", len(artifacts))
	}
}
