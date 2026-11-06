package compat

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseMaven(t *testing.T) {
	dir := t.TempDir()
	pom := `<project>
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>parent</artifactId>
    <version>1.0.0</version>
  </parent>
  <artifactId>app</artifactId>
  <properties>
    <maven.compiler.release>21</maven.compiler.release>
    <lib.version>2.0</lib.version>
  </properties>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>lib</artifactId>
      <version>${lib.version}</version>
      <exclusions><exclusion><groupId>com.example</groupId><artifactId>skip</artifactId></exclusion></exclusions>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>runtime</artifactId>
      <version>${project.version}</version>
      <scope>runtime</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>tests</artifactId>
      <version>1</version>
      <scope>test</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>provided</artifactId>
      <version>1</version>
      <scope>provided</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>system</artifactId>
      <version>1</version>
      <scope>system</scope>
    </dependency>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>bad</artifactId>
      <version>${missing}</version>
    </dependency>
  </dependencies>
  <build>
    <sourceDirectory>src/main/custom</sourceDirectory>
    <plugins>
      <plugin>
        <artifactId>maven-jar-plugin</artifactId>
        <configuration>
          <archive><manifest><mainClass>demo.App</mainClass></manifest></archive>
        </configuration>
      </plugin>
    </plugins>
  </build>
</project>`
	writeFile(t, filepath.Join(dir, "pom.xml"), pom)

	build, err := parseMaven(dir)
	if err != nil {
		t.Fatalf("parseMaven() error = %v", err)
	}
	if build.Java != 21 {
		t.Errorf("Java = %d, want 21", build.Java)
	}
	if build.MainClass != "demo.App" {
		t.Errorf("MainClass = %q", build.MainClass)
	}
	if !reflect.DeepEqual(build.Compile, []string{"com.example:lib:2.0"}) {
		t.Errorf("Compile = %v", build.Compile)
	}
	if !reflect.DeepEqual(build.Runtime, []string{"com.example:runtime:1.0.0"}) {
		t.Errorf("Runtime = %v", build.Runtime)
	}
	if !reflect.DeepEqual(build.Test, []string{"com.example:tests:1"}) {
		t.Errorf("Test = %v", build.Test)
	}
	if !reflect.DeepEqual(build.Provided, []string{"com.example:provided:1"}) {
		t.Errorf("Provided = %v", build.Provided)
	}
	if !reflect.DeepEqual(build.JavaDirs, []string{"src/main/custom"}) {
		t.Errorf("JavaDirs = %v", build.JavaDirs)
	}
	if !reflect.DeepEqual(build.TestJavaDirs, []string{"src/test/java"}) {
		t.Errorf("TestJavaDirs = %v", build.TestJavaDirs)
	}

	warnings := strings.Join(build.Warnings, "\n")
	for _, want := range []string{"system", "missing", "exclusions"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings do not contain %q:\n%s", want, warnings)
		}
	}
}

func TestParseMavenClassifier(t *testing.T) {
	dir := t.TempDir()
	pom := `<project>
  <artifactId>app</artifactId>
  <dependencies>
    <dependency>
      <groupId>org.lwjgl</groupId>
      <artifactId>lwjgl</artifactId>
      <version>3.3.1</version>
      <classifier>natives-linux</classifier>
    </dependency>
  </dependencies>
</project>`
	writeFile(t, filepath.Join(dir, "pom.xml"), pom)

	build, err := parseMaven(dir)
	if err != nil {
		t.Fatalf("parseMaven() error = %v", err)
	}
	if !reflect.DeepEqual(build.Compile, []string{"org.lwjgl:lwjgl:3.3.1:natives-linux"}) {
		t.Errorf("Compile = %v", build.Compile)
	}
}

func TestParseMavenPackaging(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), `<project><artifactId>app</artifactId><packaging>war</packaging></project>`)

	_, err := parseMaven(dir)
	if err == nil {
		t.Fatal("parseMaven() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "packaging") {
		t.Errorf("error %q does not mention packaging", err)
	}
}

func TestParseMavenBadXML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), "<project>")

	if _, err := parseMaven(dir); err == nil {
		t.Fatal("parseMaven() error = nil, want error")
	}
}

func TestParseMavenMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pom.xml"), "<project></project>")

	_, err := parseMaven(dir)
	if err == nil {
		t.Fatal("parseMaven() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "artifactId") {
		t.Errorf("error %q does not mention artifactId", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
