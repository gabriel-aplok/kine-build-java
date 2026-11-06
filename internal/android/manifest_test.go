package android

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestMergeManifests(t *testing.T) {
	dir := t.TempDir()
	mainManifest := writeTestManifest(t, dir, "main.xml", `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:tools="http://schemas.android.com/tools"
    package="com.example.app"
    android:versionName="${versionName}">
    <uses-permission android:name="android.permission.INTERNET"/>
    <uses-permission android:name="android.permission.CAMERA"/>
    <application android:label="App" android:debuggable="false">
        <activity android:name=".MainActivity" android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN"/>
                <category android:name="android.intent.category.LAUNCHER"/>
            </intent-filter>
        </activity>
        <meta-data android:name="main" android:value="main"/>
    </application>
</manifest>
`)
	buildTypeManifest := writeTestManifest(t, dir, "debug.xml", `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:tools="http://schemas.android.com/tools">
    <uses-permission android:name="android.permission.CAMERA" tools:node="remove"/>
    <uses-permission android:name="android.permission.VIBRATE"/>
    <application android:debuggable="true">
        <activity android:name=".DebugActivity" android:exported="false"/>
        <meta-data android:name="debug" android:value="true"/>
    </application>
</manifest>
`)
	flavorManifest := writeTestManifest(t, dir, "free.xml", `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:tools="http://schemas.android.com/tools">
    <application android:label="${applicationId}">
        <activity android:name=".MainActivity" android:exported="false" tools:replace="android:exported"/>
        <activity android:name=".FreeActivity" android:exported="false"/>
    </application>
</manifest>
`)

	merged, err := mergeManifestFiles([]string{mainManifest, buildTypeManifest, flavorManifest}, map[string]string{
		"applicationId": "com.example.app.free",
		"versionName":   "1.0-free",
	})
	if err != nil {
		t.Fatalf("mergeManifestFiles() error = %v", err)
	}

	if packageName := attributeValue(merged, "", "package"); packageName != "com.example.app" {
		t.Errorf("package = %q", packageName)
	}
	if versionName := attributeValue(merged, "android", "versionName"); versionName != "1.0-free" {
		t.Errorf("versionName = %q", versionName)
	}
	if hasManifestChild(merged, "uses-permission:android.permission.CAMERA") {
		t.Error("CAMERA permission was not removed")
	}
	if !hasManifestChild(merged, "uses-permission:android.permission.INTERNET") ||
		!hasManifestChild(merged, "uses-permission:android.permission.VIBRATE") {
		t.Error("permissions were not merged")
	}

	application := findManifestChild(merged, "application")
	if application == nil {
		t.Fatal("application is missing")
	}
	if label := attributeValue(application, "android", "label"); label != "com.example.app.free" {
		t.Errorf("label = %q", label)
	}
	if debuggable := attributeValue(application, "android", "debuggable"); debuggable != "true" {
		t.Errorf("debuggable = %q", debuggable)
	}
	mainActivity := findManifestChild(application, "activity:.MainActivity")
	if mainActivity == nil {
		t.Fatal("MainActivity is missing")
	}
	if exported := attributeValue(mainActivity, "android", "exported"); exported != "false" {
		t.Errorf("MainActivity exported = %q", exported)
	}
	if findManifestChild(application, "activity:.DebugActivity") == nil ||
		findManifestChild(application, "activity:.FreeActivity") == nil {
		t.Error("activities were not merged")
	}
	if findManifestChild(application, "meta-data:debug") == nil ||
		findManifestChild(application, "meta-data:main") == nil {
		t.Error("meta-data was not merged")
	}
	if attributeValue(mainActivity, "tools", "replace") != "" {
		t.Error("tools attributes were not stripped")
	}

	output := filepath.Join(dir, "merged.xml")
	if err := writeManifestFile(output, merged); err != nil {
		t.Fatalf("writeManifestFile() error = %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `xmlns:android="http://schemas.android.com/apk/res/android"`) {
		t.Errorf("merged manifest lost the android namespace:\n%s", data)
	}
	reparsed, err := parseManifest(output)
	if err != nil {
		t.Fatalf("parseManifest() error = %v\n%s", err, data)
	}
	if attributeValue(reparsed, "", "package") != "com.example.app" {
		t.Errorf("reparsed package = %q", attributeValue(reparsed, "", "package"))
	}
}

func TestMergeManifestsToolsNodeReplace(t *testing.T) {
	dir := t.TempDir()
	mainManifest := writeTestManifest(t, dir, "main.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app">
    <application>
        <activity android:name=".MainActivity" android:exported="true"/>
    </application>
</manifest>
`)
	flavorManifest := writeTestManifest(t, dir, "free.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools">
    <application>
        <activity android:name=".MainActivity" android:exported="false" tools:node="replace"/>
    </application>
</manifest>
`)

	merged, err := mergeManifestFiles([]string{mainManifest, flavorManifest}, nil)
	if err != nil {
		t.Fatalf("mergeManifestFiles() error = %v", err)
	}
	application := findManifestChild(merged, "application")
	activity := findManifestChild(application, "activity:.MainActivity")
	if activity == nil {
		t.Fatal("MainActivity is missing")
	}
	if exported := attributeValue(activity, "android", "exported"); exported != "false" {
		t.Errorf("exported = %q", exported)
	}
}

func TestMergeManifestsUnsupportedRule(t *testing.T) {
	dir := t.TempDir()
	mainManifest := writeTestManifest(t, dir, "main.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application/></manifest>`)
	badManifest := writeTestManifest(t, dir, "bad.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools"><application><activity android:name=".A" tools:node="bogus"/></application></manifest>`)

	_, err := mergeManifestFiles([]string{mainManifest, badManifest}, nil)
	if err == nil {
		t.Fatal("mergeManifestFiles() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "tools:node") {
		t.Errorf("error %q does not mention tools:node", err)
	}
}

func TestMergeManifestsUnknownPlaceholder(t *testing.T) {
	dir := t.TempDir()
	manifest := writeTestManifest(t, dir, "main.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app" android:label="${missing}"><application/></manifest>`)

	_, err := mergeManifestFiles([]string{manifest}, nil)
	if err == nil {
		t.Fatal("mergeManifestFiles() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not name the placeholder", err)
	}
}

func TestRunAndroidManifestMerger(t *testing.T) {
	dir := t.TempDir()
	mainManifest := writeTestManifest(t, dir, "main.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><application android:label="${applicationId}"/></manifest>`)
	overlay := writeTestManifest(t, dir, "debug.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android"><uses-permission android:name="android.permission.INTERNET"/></manifest>`)
	library := writeTestManifest(t, dir, "lib.xml", `<manifest xmlns:android="http://schemas.android.com/apk/res/android"><application><activity android:name="com.example.lib.LibActivity"/></application></manifest>`)
	buildDir := filepath.Join(dir, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var commands []toolchain.Command
	provider := &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		commands = append(commands, command)
		target := ""
		for i, arg := range command.Args {
			if arg == "--out" && i+1 < len(command.Args) {
				target = command.Args[i+1]
				break
			}
		}
		if target == "" {
			return toolchain.Result{}, nil
		}
		merged := `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app"><uses-permission android:name="android.permission.INTERNET"/><application android:label="com.example.app.free" android:debuggable="true"><activity android:name="com.example.lib.LibActivity"/></application></manifest>`
		return toolchain.Result{}, os.WriteFile(target, []byte(merged), 0o600)
	}}

	options := Options{
		BaseManifest: mainManifest,
		Manifests:    []string{mainManifest, overlay},
		AARManifests: []string{library},
		MinSDK:       24,
		TargetSDK:    36,
		VersionCode:  1,
		VersionName:  "1.0",
		Variant: Variant{
			Name:             "freeDebug",
			FlavorOptions:    FlavorOptions{ApplicationIDSuffix: ".free"},
			BuildTypeOptions: BuildTypeOptions{Debug: true},
		},
	}
	tool := &manifestMergerTool{Main: androidManifestMergerMain, Classpath: []string{"/tool/manifest-merger.jar"}}
	target, err := provider.runAndroidManifestMerger(context.Background(), &core.Project{ID: "app", Dir: dir}, options, buildDir, testJDK(), tool)
	if err != nil {
		t.Fatalf("runAndroidManifestMerger() error = %v", err)
	}
	if target != filepath.Join(buildDir, "AndroidManifest.xml") {
		t.Errorf("target = %q", target)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(commands))
	}
	for _, want := range []string{
		"--main", mainManifest,
		"--libs", library,
		"--overlays", overlay,
		"--remove-tools-declarations",
		"--placeholder", "applicationId=com.example.app.free",
		"--property", "MIN_SDK_VERSION=24",
	} {
		if !contains(commands[0].Args, want) {
			t.Errorf("args %v do not contain %q", commands[0].Args, want)
		}
	}
}

func writeTestManifest(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
