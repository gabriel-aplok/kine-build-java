package android

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const launcherManifest = `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.app">
    <uses-sdk android:minSdkVersion="24" android:targetSdkVersion="36" />
    <application android:label="App">
        <activity android:name=".MainActivity" android:exported="true">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>
    </application>
</manifest>
`

func TestLaunchInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AndroidManifest.xml")
	if err := os.WriteFile(path, []byte(launcherManifest), 0o600); err != nil {
		t.Fatal(err)
	}

	packageName, activity, err := launchInfo(path)
	if err != nil {
		t.Fatalf("launchInfo() error = %v", err)
	}
	if packageName != "com.example.app" {
		t.Errorf("package = %q", packageName)
	}
	if activity != "com.example.app.MainActivity" {
		t.Errorf("activity = %q", activity)
	}
}

func TestLaunchInfoNoLauncher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AndroidManifest.xml")
	manifest := `<manifest package="com.example.app"><application><activity android:name=".Other" /></application></manifest>`
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	packageName, activity, err := launchInfo(path)
	if err != nil {
		t.Fatalf("launchInfo() error = %v", err)
	}
	if packageName != "com.example.app" || activity != "" {
		t.Errorf("package/activity = %q/%q", packageName, activity)
	}
}

func TestLaunchInfoMissingPackage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AndroidManifest.xml")
	if err := os.WriteFile(path, []byte("<manifest></manifest>"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, err := launchInfo(path); err == nil {
		t.Fatal("launchInfo() error = nil, want error")
	}
}

func TestRunTask(t *testing.T) {
	fixture := newRunFixture(t)
	var commands []toolchain.Command
	provider := fixture.provider("List of devices attached\nSERIAL1\tdevice\n", &commands)

	task, err := provider.runTask(fixture.workspace, fixture.project, fixture.options, fixture.sdk, &toolchain.JDK{Home: "/jdk", Version: "21.0.1", Major: 21})
	if err != nil {
		t.Fatalf("runTask() error = %v", err)
	}
	if task.ID != "run" {
		t.Errorf("ID = %q", task.ID)
	}
	if len(task.Deps) != 1 || task.Deps[0] != "app:apk" {
		t.Errorf("Deps = %v", task.Deps)
	}
	if len(task.Inputs) != 1 || task.Inputs[0].Path != "app/build/outputs/apk/debug/app-debug.apk" {
		t.Errorf("Inputs = %+v", task.Inputs)
	}
	if task.Key != "android-run debug 0 com.example.app/com.example.app.MainActivity" {
		t.Errorf("Key = %q", task.Key)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(commands))
	}
	if commands[0].Path != fixture.adb || !reflect.DeepEqual(commands[0].Args, []string{"devices"}) {
		t.Errorf("devices = %q %v", commands[0].Path, commands[0].Args)
	}
	wantInstall := []string{"-s", "SERIAL1", "install", "-r", fixture.apk}
	if commands[1].Path != fixture.adb || !reflect.DeepEqual(commands[1].Args, wantInstall) {
		t.Errorf("install = %q %v", commands[1].Path, commands[1].Args)
	}
	wantStart := []string{"-s", "SERIAL1", "shell", "am", "start", "-W", "-n", "com.example.app/com.example.app.MainActivity"}
	if commands[2].Path != fixture.adb || !reflect.DeepEqual(commands[2].Args, wantStart) {
		t.Errorf("start = %q %v", commands[2].Path, commands[2].Args)
	}
}

func TestRunTaskConfiguredDevice(t *testing.T) {
	fixture := newRunFixture(t)
	var commands []toolchain.Command
	provider := fixture.provider("", &commands)
	provider.device = "SERIAL9"

	task, err := provider.runTask(fixture.workspace, fixture.project, fixture.options, fixture.sdk, &toolchain.JDK{})
	if err != nil {
		t.Fatalf("runTask() error = %v", err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %d, want 2", len(commands))
	}
	if commands[0].Args[0] != "-s" || commands[0].Args[1] != "SERIAL9" {
		t.Errorf("install = %v", commands[0].Args)
	}
	if commands[1].Args[0] != "-s" || commands[1].Args[1] != "SERIAL9" {
		t.Errorf("start = %v", commands[1].Args)
	}
}

func TestResolveDeviceSingle(t *testing.T) {
	fixture := newRunFixture(t)
	provider := fixture.provider("List of devices attached\nSERIAL1\tdevice\nSERIAL2\toffline\n", nil)

	serial, err := provider.resolveDevice(context.Background(), fixture.adb, fixture.project, nil)
	if err != nil {
		t.Fatalf("resolveDevice() error = %v", err)
	}
	if serial != "SERIAL1" {
		t.Errorf("serial = %q", serial)
	}
}

func TestResolveDeviceMultiple(t *testing.T) {
	fixture := newRunFixture(t)
	provider := fixture.provider("List of devices attached\nSERIAL1\tdevice\nSERIAL2\tdevice\n", nil)

	_, err := provider.resolveDevice(context.Background(), fixture.adb, fixture.project, nil)
	if err == nil {
		t.Fatal("resolveDevice() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--device") {
		t.Errorf("error %q does not mention --device", err)
	}
}

func TestResolveDeviceNone(t *testing.T) {
	fixture := newRunFixture(t)
	provider := fixture.provider("List of devices attached\n", nil)

	_, err := provider.resolveDevice(context.Background(), fixture.adb, fixture.project, nil)
	if err == nil {
		t.Fatal("resolveDevice() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "no Android device") {
		t.Errorf("error = %q", err)
	}
}

func TestResolveDeviceUnauthorized(t *testing.T) {
	fixture := newRunFixture(t)
	provider := fixture.provider("List of devices attached\nSERIAL1\tunauthorized\n", nil)

	_, err := provider.resolveDevice(context.Background(), fixture.adb, fixture.project, nil)
	if err == nil {
		t.Fatal("resolveDevice() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("error = %q", err)
	}
}

func TestParseDevices(t *testing.T) {
	devices := parseDevices("List of devices attached\nSERIAL1\tdevice\nSERIAL2\toffline\n\n")
	want := []adbDevice{{serial: "SERIAL1", status: "device"}, {serial: "SERIAL2", status: "offline"}}
	if !reflect.DeepEqual(devices, want) {
		t.Errorf("devices = %+v, want %+v", devices, want)
	}
}

type runFixture struct {
	workspace string
	project   *core.Project
	options   Options
	sdk       *SDK
	adb       string
	apk       string
}

func newRunFixture(t *testing.T) runFixture {
	t.Helper()

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "app")
	manifest := filepath.Join(projectDir, "AndroidManifest.xml")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(launcherManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	apkPath := filepath.Join(projectDir, "build", "outputs", "apk", "debug", "app-debug.apk")
	if err := os.MkdirAll(filepath.Dir(apkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apkPath, []byte("apk"), 0o600); err != nil {
		t.Fatal(err)
	}

	sdkRoot := t.TempDir()
	adb := filepath.Join(sdkRoot, "platform-tools", "adb")
	if err := os.MkdirAll(filepath.Dir(adb), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adb, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	return runFixture{
		workspace: workspace,
		project:   &core.Project{ID: "app", Dir: projectDir},
		options:   Options{Manifest: manifest},
		sdk:       &SDK{Root: sdkRoot},
		adb:       adb,
		apk:       apkPath,
	}
}

func (f runFixture) provider(devices string, commands *[]toolchain.Command) *Provider {
	return &Provider{run: func(ctx context.Context, command toolchain.Command) (toolchain.Result, error) {
		if commands != nil {
			*commands = append(*commands, command)
		}
		if len(command.Args) > 0 && command.Args[0] == "devices" {
			return toolchain.Result{Stdout: []byte(devices)}, nil
		}
		return toolchain.Result{}, nil
	}}
}

func TestRunTaskNoLauncher(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(`<manifest package="com.example.app"><application/></manifest>`), 0o600); err != nil {
		t.Fatal(err)
	}

	task, err := (&Provider{}).runTask(dir, &core.Project{ID: "app", Dir: dir}, Options{Manifest: manifest}, &SDK{}, &toolchain.JDK{})
	if err != nil {
		t.Fatalf("runTask() error = %v", err)
	}
	if task != nil {
		t.Errorf("task = %+v, want nil", task)
	}
}

func TestFindADBMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := findADB(&SDK{Root: t.TempDir()})
	if err == nil {
		t.Fatal("findADB() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "adb") {
		t.Errorf("error %q does not mention adb", err)
	}
}

func TestParseDevicesCRLF(t *testing.T) {
	devices := parseDevices("List of devices attached\r\nSERIAL1\tdevice\r\n")
	want := []adbDevice{{serial: "SERIAL1", status: "device"}}
	if !reflect.DeepEqual(devices, want) {
		t.Errorf("devices = %+v, want %+v", devices, want)
	}
}
