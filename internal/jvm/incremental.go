package jvm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/fsutil"
	"github.com/gabriel-aplok/kine-build-java/internal/hash"
)

type javacState struct {
	Classpath string            `json:"classpath"`
	Sources   map[string]string `json:"sources"`
}

type compilePlan struct {
	Sources      []string
	Full         bool
	Classpath    string
	SourceHashes map[string]string
}

var classDeclaration = regexp.MustCompile(`\b(?:class|interface|enum|record)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

func planJavac(projectRoot, output string, sources, classpath []string) (compilePlan, error) {
	hashes, contents, err := readSources(projectRoot, sources)
	if err != nil {
		return compilePlan{}, err
	}
	classpathHash, err := hashClasspath(classpath)
	if err != nil {
		return compilePlan{}, err
	}

	plan := compilePlan{
		Classpath:    classpathHash,
		SourceHashes: hashes,
	}

	state, ok, err := readJavacState(output)
	if err != nil {
		return compilePlan{}, err
	}
	if !ok || state.Classpath != classpathHash || deleted(state.Sources, hashes) {
		plan.Full = true
		plan.Sources = append([]string(nil), sources...)
		return plan, nil
	}

	var changed []string
	for _, source := range sources {
		key := sourceKey(projectRoot, source)
		if state.Sources[key] != hashes[key] {
			changed = append(changed, source)
		}
	}
	if len(changed) == 0 {
		return plan, nil
	}

	seeds := make(map[string]struct{})
	for _, source := range changed {
		key := sourceKey(projectRoot, source)
		for _, match := range classDeclaration.FindAllStringSubmatch(contents[key], -1) {
			seeds[match[1]] = struct{}{}
		}
	}

	selected := make(map[string]struct{}, len(changed))
	for _, source := range changed {
		selected[source] = struct{}{}
	}
	if len(seeds) > 0 {
		names := make([]string, 0, len(seeds))
		for name := range seeds {
			names = append(names, regexp.QuoteMeta(name))
		}
		sort.Strings(names)
		pattern := regexp.MustCompile(`\b(?:` + strings.Join(names, "|") + `)\b`)
		for _, source := range sources {
			key := sourceKey(projectRoot, source)
			if pattern.MatchString(contents[key]) {
				selected[source] = struct{}{}
			}
		}
	}

	if len(selected)*4 >= len(sources)*3 {
		plan.Full = true
		plan.Sources = append([]string(nil), sources...)
		return plan, nil
	}

	plan.Sources = make([]string, 0, len(selected))
	for source := range selected {
		plan.Sources = append(plan.Sources, source)
	}
	sort.Strings(plan.Sources)
	return plan, nil
}

func writeJavacState(output string, plan compilePlan) error {
	state := javacState{
		Classpath: plan.Classpath,
		Sources:   plan.SourceHashes,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode javac state: %w", err)
	}
	path := javacStatePath(output)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("write javac state: %w", err)
	}
	if err := fsutil.Replace(temp, path); err != nil {
		os.Remove(temp)
		return fmt.Errorf("install javac state: %w", err)
	}
	return nil
}

func javacStatePath(output string) string {
	return output + ".javac-state.json"
}

func readJavacState(output string) (javacState, bool, error) {
	data, err := os.ReadFile(javacStatePath(output))
	if err != nil {
		if os.IsNotExist(err) {
			return javacState{}, false, nil
		}
		return javacState{}, false, fmt.Errorf("read javac state: %w", err)
	}
	var state javacState
	if err := json.Unmarshal(data, &state); err != nil {
		return javacState{}, false, nil
	}
	if state.Sources == nil {
		state.Sources = map[string]string{}
	}
	return state, true, nil
}

func readSources(projectRoot string, sources []string) (map[string]string, map[string]string, error) {
	hashes := make(map[string]string, len(sources))
	contents := make(map[string]string, len(sources))
	for _, source := range sources {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", source, err)
		}
		key := sourceKey(projectRoot, source)
		sum := sha256.Sum256(data)
		hashes[key] = hex.EncodeToString(sum[:])
		contents[key] = string(data)
	}
	return hashes, contents, nil
}

func sourceKey(projectRoot, source string) string {
	relative, err := filepath.Rel(projectRoot, source)
	if err != nil {
		return filepath.ToSlash(source)
	}
	return filepath.ToSlash(relative)
}

func hashClasspath(classpath []string) (string, error) {
	hasher := sha256.New()
	for _, entry := range classpath {
		info, err := os.Stat(entry)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("stat classpath %s: %w", entry, err)
		}
		sum := ""
		if info.IsDir() {
			sum, err = hash.Tree(entry)
		} else {
			sum, err = hash.File(entry)
		}
		if err != nil {
			return "", fmt.Errorf("hash classpath %s: %w", entry, err)
		}
		hasher.Write([]byte(entry))
		hasher.Write([]byte{0})
		hasher.Write([]byte(sum))
		hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func deleted(previous, current map[string]string) bool {
	for key := range previous {
		if _, ok := current[key]; !ok {
			return true
		}
	}
	return false
}
