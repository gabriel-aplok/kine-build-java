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

func TestBuildAPKIntegration(t *testing.T) {
	sdk, err := FindSDK(Requirement{})
	if err != nil {
		t.Skipf("no Android SDK: %v", err)
	}
	jdk, err := toolchain.Find(toolchain.Requirement{})
	if err != nil {
		t.Skipf("no JDK: %v", err)
	}
	if jdk.Major < 17 {
		t.Skipf("JDK %d is older than 17", jdk.Major)
	}

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	writeAndroidProject(t, projectDir)

	provider := NewProvider(Config{
		Workspace: &core.Workspace{Root: workspace},
		Logger:    discardTestLogger(),
	})
	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"compile_sdk":  platformMajor(sdk.Platform),
			"min_sdk":      24,
			"version_code": 1,
			"version_name": "1.0",
		},
	}

	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	var buildTasks []*core.Task
	hasRun := false
	for _, task := range tasks {
		if task.ID == "run" {
			hasRun = true
			continue
		}
		if task.ID == "lint" {
			continue
		}
		buildTasks = append(buildTasks, task)
	}
	if len(buildTasks) != 4 {
		t.Fatalf("build tasks = %d, want 4", len(buildTasks))
	}
	if !hasRun {
		t.Error("run task is missing")
	}
	for _, task := range buildTasks {
		if err := task.Run(context.Background()); err != nil {
			t.Fatalf("%s: %v", task.ID, err)
		}
	}

	apk := filepath.Join(projectDir, "build", "outputs", "apk", "debug", "app-debug.apk")
	if _, err := os.Stat(apk); err != nil {
		t.Fatalf("apk: %v", err)
	}

	tmpDir := t.TempDir()
	result, err := toolchain.Run(context.Background(), toolchain.Command{
		Path: sdk.Aapt2,
		Args: []string{"dump", "badging", apk},
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		t.Fatalf("aapt2 dump badging: %v\n%s", err, result.Stderr)
	}
	badging := string(result.Stdout)
	if !strings.Contains(badging, "package: name='com.example.app'") {
		t.Errorf("badging does not report the package:\n%s", badging)
	}
	if !strings.Contains(badging, "launchable-activity: name='com.example.app.MainActivity'") {
		t.Errorf("badging does not report the launcher activity:\n%s", badging)
	}

	result, err = toolchain.Run(context.Background(), toolchain.Command{
		Path: sdk.ApkSigner,
		Args: []string{"verify", "--print-certs", apk},
		Env:  toolchain.BuildEnv(jdk, tmpDir, nil),
	})
	if err != nil {
		t.Fatalf("apksigner verify: %v\n%s", err, result.Stderr)
	}
	if !strings.Contains(string(result.Stdout), "certificate DN: CN=Android Debug") {
		t.Errorf("apksigner output does not report the debug certificate:\n%s", result.Stdout)
	}
}

func writeAndroidProject(t *testing.T, projectDir string) {
	t.Helper()

	manifest := `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app">
    <uses-sdk android:minSdkVersion="24" android:targetSdkVersion="36" />
    <application android:label="@string/app_name">
        <activity android:name=".MainActivity" android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
`
	stringsXML := `<?xml version="1.0" encoding="utf-8"?>
<resources>
    <string name="app_name">Kine Demo</string>
</resources>
`
	activity := `package com.example.app;

import android.app.Activity;
import android.os.Bundle;

public final class MainActivity extends Activity {
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
    }
}
`

	files := map[string]string{
		"src/main/AndroidManifest.xml":                    manifest,
		"src/main/res/values/strings.xml":                 stringsXML,
		"src/main/java/com/example/app/MainActivity.java": activity,
	}
	for name, content := range files {
		path := filepath.Join(projectDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
