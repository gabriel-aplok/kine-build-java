package jvm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanJavacFirstRun(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "classes")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	source := writeIncrementalSource(t, root, "A.java", "class A {}")

	plan, err := planJavac(root, output, []string{source}, nil)
	if err != nil {
		t.Fatalf("planJavac() error = %v", err)
	}
	if !plan.Full {
		t.Error("Full = false, want true")
	}
	if len(plan.Sources) != 1 || plan.Sources[0] != source {
		t.Errorf("Sources = %v", plan.Sources)
	}
}

func TestPlanJavacIncremental(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "classes")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	a := writeIncrementalSource(t, root, "A.java", "class A {}")
	b := writeIncrementalSource(t, root, "B.java", "class B { A value; }")
	c := writeIncrementalSource(t, root, "C.java", "class C {}")
	sources := []string{a, b, c}

	first, err := planJavac(root, output, sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJavacState(output, first); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(a, []byte("class A { int value; }"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := planJavac(root, output, sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Full {
		t.Fatal("Full = true, want incremental")
	}
	if !contains(planSourceNames(second.Sources), "A.java") {
		t.Errorf("Sources = %v, want A", second.Sources)
	}
	if !contains(planSourceNames(second.Sources), "B.java") {
		t.Errorf("Sources = %v, want B", second.Sources)
	}
	if contains(planSourceNames(second.Sources), "C.java") {
		t.Errorf("Sources = %v, do not want C", second.Sources)
	}
}

func TestPlanJavacNoChanges(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "classes")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	a := writeIncrementalSource(t, root, "A.java", "class A {}")

	first, err := planJavac(root, output, []string{a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJavacState(output, first); err != nil {
		t.Fatal(err)
	}
	second, err := planJavac(root, output, []string{a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Full || len(second.Sources) != 0 {
		t.Errorf("plan = %+v, want no work", second)
	}
}

func TestPlanJavacClasspathChange(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "classes")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	a := writeIncrementalSource(t, root, "A.java", "class A {}")
	dep := filepath.Join(root, "dep.jar")
	if err := os.WriteFile(dep, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := planJavac(root, output, []string{a}, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJavacState(output, first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dep, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := planJavac(root, output, []string{a}, []string{dep})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Full {
		t.Error("Full = false after a classpath change")
	}
}

func TestPlanJavacDeletedSource(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "classes")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	a := writeIncrementalSource(t, root, "A.java", "class A {}")
	b := writeIncrementalSource(t, root, "B.java", "class B {}")

	first, err := planJavac(root, output, []string{a, b}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJavacState(output, first); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	second, err := planJavac(root, output, []string{a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Full {
		t.Error("Full = false after a deleted source")
	}
}

func writeIncrementalSource(t *testing.T, root, name, content string) string {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func planSourceNames(sources []string) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, filepath.Base(source))
	}
	return names
}
