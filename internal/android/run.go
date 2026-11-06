package android

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	mainAction       = "android.intent.action.MAIN"
	launcherCategory = "android.intent.category.LAUNCHER"
)

func launchInfo(path string) (string, string, error) {
	manifest, err := parseManifest(path)
	if err != nil {
		return "", "", err
	}
	return launchInfoNode(manifest)
}

func launchInfoForOptions(options Options) (string, string, error) {
	base := options.BaseManifest
	if base == "" && len(options.Manifests) > 0 {
		base = options.Manifests[0]
	}
	if base == "" {
		base = options.Manifest
	}
	values, err := manifestValues(base, options)
	if err != nil {
		return "", "", err
	}
	manifests := append(append([]string(nil), options.AARManifests...), options.Manifests...)
	if len(manifests) == 0 {
		manifests = []string{base}
	}
	merged, err := mergeManifestFiles(manifests, values)
	if err != nil {
		return "", "", err
	}
	if values["applicationId"] != values["packageName"] {
		qualifyRelativeComponentNames(merged, values["packageName"])
	}
	return launchInfoNode(merged)
}

func launchInfoNode(manifest *manifestNode) (string, string, error) {
	packageName := attributeValue(manifest, "", "package")
	if packageName == "" {
		return "", "", fmt.Errorf("manifest package is missing")
	}
	application := findManifestChild(manifest, "application")
	if application == nil {
		return packageName, "", nil
	}
	for _, activity := range application.Children {
		if activity.Name.Local != "activity" || !hasLauncher(activity) {
			continue
		}
		name := attributeValue(activity, "android", "name")
		if strings.HasPrefix(name, ".") {
			name = packageName + name
		} else if name != "" && !strings.Contains(name, ".") {
			name = packageName + "." + name
		}
		return packageName, name, nil
	}
	return packageName, "", nil
}

func hasLauncher(activity *manifestNode) bool {
	for _, filter := range activity.Children {
		if filter.Name.Local != "intent-filter" {
			continue
		}
		main := false
		launcher := false
		for _, child := range filter.Children {
			name := attributeValue(child, "android", "name")
			switch child.Name.Local {
			case "action":
				if name == mainAction {
					main = true
				}
			case "category":
				if name == launcherCategory {
					launcher = true
				}
			}
		}
		if main && launcher {
			return true
		}
	}
	return false
}

func findADB(sdk *SDK) (string, error) {
	if sdk != nil && sdk.Root != "" {
		for _, name := range []string{"adb", "adb.exe"} {
			path := filepath.Join(sdk.Root, "platform-tools", name)
			if isRegular(path) {
				return path, nil
			}
		}
	}
	path, err := exec.LookPath("adb")
	if err != nil {
		return "", fmt.Errorf("adb not found, set ANDROID_HOME or add platform-tools to PATH")
	}
	return path, nil
}

func (p *Provider) runTask(workspaceRoot string, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK) (*core.Task, error) {
	packageName, activity, err := launchInfoForOptions(options)
	if err != nil {
		return nil, err
	}
	if activity == "" {
		return nil, nil
	}
	if options.ApplicationID != "" {
		packageName = options.ApplicationID
	}

	inputPath := options.apkOutputPath(project.Dir)
	dependency := options.taskID("apk")
	if len(options.Modules) > 0 {
		inputPath = options.bundleOutputPath(project.Dir)
		dependency = options.taskID("bundle")
	}
	input, err := relativePath(workspaceRoot, inputPath)
	if err != nil {
		return nil, err
	}

	return &core.Task{
		ID:     "run",
		Deps:   []string{project.ID + ":" + dependency},
		Inputs: []core.Input{{Path: input, Mode: core.ModeFile}},
		Key: "android-run " + options.variantLabel() + " " +
			strconv.Itoa(options.effectiveVersionCode()) + " " + packageName + "/" + activity,
		Run: func(ctx context.Context) error {
			return p.runOnDevice(ctx, project, options, sdk, jdk, packageName, activity)
		},
	}, nil
}

func (p *Provider) runOnDevice(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, packageName, activity string) error {
	adb, err := findADB(sdk)
	if err != nil {
		return err
	}
	apk := options.apkOutputPath(project.Dir)

	tmpDir := filepath.Join(project.Dir, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", tmpDir, err)
	}
	env := toolchain.BuildEnv(jdk, tmpDir, nil)

	serial, err := p.resolveDevice(ctx, adb, project, env)
	if err != nil {
		return err
	}

	prefix := []string{"-s", serial}
	if len(options.Modules) > 0 {
		if err := p.installBundleSplits(ctx, project, options, sdk, jdk, adb, serial, env); err != nil {
			return err
		}
	} else {
		installArgs := append(append([]string(nil), prefix...), "install", "-r", apk)
		result, err := p.run(ctx, toolchain.Command{
			Path: adb,
			Args: installArgs,
			Dir:  project.Dir,
			Env:  env,
		})
		if err != nil {
			return toolError("adb install", result, err)
		}
	}

	startArgs := append(append([]string(nil), prefix...),
		"shell", "am", "start", "-W", "-n", packageName+"/"+activity)
	result, err := p.run(ctx, toolchain.Command{
		Path: adb,
		Args: startArgs,
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return toolError("adb start", result, err)
	}
	return nil
}

func (p *Provider) installBundleSplits(ctx context.Context, project *core.Project, options Options, sdk *SDK, jdk *toolchain.JDK, adb, serial string, env []string) error {
	if len(options.BundletoolClasspath) == 0 || options.BundletoolMain == "" {
		return fmt.Errorf("dynamic feature run requires bundletool")
	}
	signing := options.signingConfig()
	if signing != nil && signing.Unresolved {
		return fmt.Errorf("release signing environment variables are not set")
	}
	root := p.workspaceRoot(project)
	keystore, err := p.ensureKeystore(ctx, root, jdk, filepath.Join(project.Dir, ".kine", "tmp"), signing)
	if err != nil {
		return err
	}
	alias, storePassword, keyPassword := debugAlias, debugPass, debugPass
	if signing != nil {
		alias = signing.Alias
		storePassword = signing.StorePassword
		keyPassword = signing.KeyPassword
	}
	classpath := strings.Join(options.BundletoolClasspath, string(os.PathListSeparator))
	aab := options.bundleOutputPath(project.Dir)
	apks := options.androidPath(project.Dir, "run.apks")
	if err := os.Remove(apks); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clean %s: %w", apks, err)
	}
	result, err := p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: []string{
			"-cp", classpath,
			options.BundletoolMain,
			"build-apks",
			"--bundle=" + aab,
			"--output=" + apks,
			"--aapt2=" + sdk.Aapt2,
			"--local-testing",
			"--ks=" + keystore,
			"--ks-pass=pass:" + storePassword,
			"--ks-key-alias=" + alias,
			"--key-pass=pass:" + keyPassword,
		},
		Dir: project.Dir,
		Env: env,
	})
	if err != nil {
		return toolError("bundletool build-apks", result, err)
	}
	result, err = p.run(ctx, toolchain.Command{
		Path: jdk.Java,
		Args: []string{
			"-cp", classpath,
			options.BundletoolMain,
			"install-apks",
			"--apks=" + apks,
			"--adb=" + adb,
			"--device-id=" + serial,
		},
		Dir: project.Dir,
		Env: env,
	})
	if err != nil {
		return toolError("bundletool install-apks", result, err)
	}
	return nil
}

func (p *Provider) resolveDevice(ctx context.Context, adb string, project *core.Project, env []string) (string, error) {
	if p.device != "" {
		return p.device, nil
	}

	result, err := p.run(ctx, toolchain.Command{
		Path: adb,
		Args: []string{"devices"},
		Dir:  project.Dir,
		Env:  env,
	})
	if err != nil {
		return "", toolError("adb devices", result, err)
	}

	devices := parseDevices(string(result.Stdout))
	var ready []string
	var blocked []string
	for _, device := range devices {
		if device.status == "device" {
			ready = append(ready, device.serial)
		} else {
			blocked = append(blocked, device.serial+" ("+device.status+")")
		}
	}
	if len(ready) == 1 {
		return ready[0], nil
	}
	if len(ready) == 0 {
		if len(blocked) > 0 {
			return "", fmt.Errorf("no authorized Android device: %s", strings.Join(blocked, ", "))
		}
		return "", fmt.Errorf("no Android device connected")
	}
	return "", fmt.Errorf("multiple Android devices connected (%s), use --device", strings.Join(ready, ", "))
}

type adbDevice struct {
	serial string
	status string
}

func parseDevices(output string) []adbDevice {
	var devices []adbDevice
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "List" {
			continue
		}
		devices = append(devices, adbDevice{serial: fields[0], status: fields[1]})
	}
	return devices
}
