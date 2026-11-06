package deps

import "testing"

func TestParsePOM(t *testing.T) {
	data := []byte(`<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>app</artifactId>
  <version>1.0</version>
  <properties>
    <lib.version>2.0</lib.version>
  </properties>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.example</groupId>
        <artifactId>lib</artifactId>
        <version>${lib.version}</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>lib</artifactId>
      <scope>runtime</scope>
      <optional>true</optional>
      <exclusions>
        <exclusion>
          <groupId>com.example</groupId>
          <artifactId>util</artifactId>
        </exclusion>
      </exclusions>
    </dependency>
  </dependencies>
</project>`)

	project, err := parsePOM(data)
	if err != nil {
		t.Fatalf("parsePOM() error = %v", err)
	}
	if project.GroupID != "com.example" {
		t.Errorf("GroupID = %q, want com.example", project.GroupID)
	}
	if project.Properties["lib.version"] != "2.0" {
		t.Errorf("property = %q, want 2.0", project.Properties["lib.version"])
	}
	if len(project.DependencyManagement.Items) != 1 {
		t.Fatalf("managed dependencies = %d, want 1", len(project.DependencyManagement.Items))
	}
	if got := project.DependencyManagement.Items[0].Version; got != "${lib.version}" {
		t.Errorf("managed version = %q, want ${lib.version}", got)
	}
	if len(project.Dependencies.Items) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(project.Dependencies.Items))
	}
	dependency := project.Dependencies.Items[0]
	if dependency.Scope != "runtime" {
		t.Errorf("scope = %q, want runtime", dependency.Scope)
	}
	if dependency.Optional != "true" {
		t.Errorf("optional = %q, want true", dependency.Optional)
	}
	if len(dependency.Exclusions) != 1 {
		t.Fatalf("exclusions = %d, want 1", len(dependency.Exclusions))
	}
	if dependency.Exclusions[0].ArtifactID != "util" {
		t.Errorf("exclusion artifact = %q, want util", dependency.Exclusions[0].ArtifactID)
	}
}

func TestParsePOMLatin1(t *testing.T) {
	data := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n<project><groupId>com.example</groupId><artifactId>caf\xe9</artifactId><version>1.0</version></project>")

	project, err := parsePOM(data)
	if err != nil {
		t.Fatalf("parsePOM() error = %v", err)
	}
	if project.ArtifactID != "café" {
		t.Errorf("ArtifactID = %q, want café", project.ArtifactID)
	}
}

func TestInterpolate(t *testing.T) {
	properties := map[string]string{
		"a": "1",
		"b": "${a}.2",
	}

	if got, want := interpolate("v${b}", properties), "v1.2"; got != want {
		t.Errorf("interpolate() = %q, want %q", got, want)
	}
	if got, want := interpolate("${missing}", properties), "${missing}"; got != want {
		t.Errorf("interpolate() = %q, want %q", got, want)
	}
}

func TestMergeDependencies(t *testing.T) {
	parent := []pomDependency{
		{GroupID: "g", ArtifactID: "a", Version: "1"},
		{GroupID: "g", ArtifactID: "b", Version: "1"},
	}
	child := []pomDependency{
		{GroupID: "g", ArtifactID: "b", Version: "2"},
		{GroupID: "g", ArtifactID: "c", Version: "3"},
	}

	merged := mergeDependencies(parent, child)
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	if merged[1].ArtifactID != "b" || merged[1].Version != "2" {
		t.Errorf("merged[1] = %+v, want b 2", merged[1])
	}
}
