package deps

import "testing"

func TestScoreVariantWithNumericAttributes(t *testing.T) {
	variant := &moduleVariant{
		Name: "runtime",
		Attributes: map[string]any{
			"org.gradle.usage":                               "java-runtime",
			"org.gradle.jvm.version":                         8,
			"org.jetbrains.kotlin.platform.type":             "jvm",
			"com.android.build.api.attributes.BuildTypeAttr": "release",
		},
		Files: []moduleFile{{Name: "lib-1.0.jar", URL: "lib-1.0.jar"}},
	}
	score, ok := scoreVariant(variant, map[string]string{
		"org.gradle.usage": "java-runtime",
		"com.android.build.api.attributes.BuildTypeAttr": "release",
	})
	if !ok {
		t.Fatal("scoreVariant() rejected a runtime variant")
	}
	if score <= 0 {
		t.Errorf("score = %d, want positive", score)
	}
}

func TestSelectModuleVariantRejectsMetadata(t *testing.T) {
	metadata := &moduleMetadata{Variants: []moduleVariant{
		{Name: "metadata", Attributes: map[string]any{"org.gradle.usage": "kotlin-metadata"}},
		{Name: "debug", Attributes: map[string]any{"org.gradle.usage": "java-runtime", "com.android.build.api.attributes.BuildTypeAttr": "debug"}},
		{Name: "release", Attributes: map[string]any{"org.gradle.usage": "java-runtime", "com.android.build.api.attributes.BuildTypeAttr": "release"}},
	}}
	r := &resolver{attributes: map[string]string{
		"org.gradle.usage": "java-runtime",
		"com.android.build.api.attributes.BuildTypeAttr": "release",
	}}
	selected := r.selectModuleVariant(metadata)
	if selected == nil || selected.Name != "release" {
		t.Errorf("selected = %+v, want release", selected)
	}
}
