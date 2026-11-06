package android

import (
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestBundleTask(t *testing.T) {
	workspace, projectDir := providerProject(t)
	options := Options{
		Bundle:              true,
		BundletoolVersion:   "1.18.3",
		BundletoolMain:      bundleToolMain,
		BundletoolClasspath: []string{"/tools/bundletool.jar"},
		Variant: Variant{
			Flavor:    "free",
			BuildType: "release",
			Name:      "freeRelease",
			Suffix:    "free-release",
			Sources: VariantSources{
				ResDirs:   []string{projectDir + "/src/main/res"},
				AssetDirs: []string{projectDir + "/src/main/assets"},
			},
		},
	}

	provider := &Provider{}
	task, err := provider.bundleTask(workspace, &core.Project{ID: "app", Dir: projectDir}, options, testSDK(), testJDK(), nil, false, nil, nil)
	if err != nil {
		t.Fatalf("bundleTask() error = %v", err)
	}
	if task.ID != "bundle-free-release" {
		t.Errorf("ID = %q", task.ID)
	}
	if task.Outputs[0].Path != "app/build/outputs/bundle/free/release/app-free-release-unsigned.aab" {
		t.Errorf("Outputs = %+v", task.Outputs)
	}
	for _, want := range []string{"app:android-resources-free-release", "app:dex-free-release"} {
		if !contains(task.Deps, want) {
			t.Errorf("Deps %v do not contain %q", task.Deps, want)
		}
	}
}

func TestBundleTaskUnsignedName(t *testing.T) {
	options := Options{Variant: Variant{BuildType: "release", Name: "release", Suffix: "release"}}
	if options.bundleOutputPath("/project") != "/project/build/outputs/bundle/release/app-release-unsigned.aab" {
		t.Errorf("bundle path = %q", options.bundleOutputPath("/project"))
	}
	options.ReleaseSigning = &SigningOptions{Keystore: "/key"}
	if options.bundleOutputPath("/project") != "/project/build/outputs/bundle/release/app-release.aab" {
		t.Errorf("signed bundle path = %q", options.bundleOutputPath("/project"))
	}
}
