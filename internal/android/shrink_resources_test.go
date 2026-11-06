package android

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShrinkFeatureResources(t *testing.T) {
	dir := t.TempDir()
	res := filepath.Join(dir, "res")
	java := filepath.Join(dir, "java", "com", "example", "feature")
	for _, path := range []string{
		filepath.Join(res, "values"),
		filepath.Join(res, "layout"),
		filepath.Join(res, "drawable"),
		java,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(res, "values", "strings.xml"), `<resources>
    <string name="source_used">Source</string>
    <string name="manifest_used">Manifest</string>
    <string name="xml_used">XML</string>
    <string name="public_kept">Public</string>
    <string name="tools_kept">Tools</string>
    <string name="unused">Unused</string>
    <public type="string" name="public_kept" id="0x80010000"/>
</resources>`)
	writeTestFile(t, filepath.Join(res, "layout", "screen.xml"), `<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android" xmlns:tools="http://schemas.android.com/tools" tools:keep="@string/tools_kept">
    <TextView android:text="@string/xml_used" android:background="@drawable/icon"/>
</LinearLayout>`)
	writeTestFile(t, filepath.Join(res, "drawable", "icon.xml"), `<shape xmlns:android="http://schemas.android.com/apk/res/android"/>`)
	writeTestFile(t, filepath.Join(res, "drawable", "unused.xml"), `<shape xmlns:android="http://schemas.android.com/apk/res/android"/>`)
	writeTestFile(t, filepath.Join(java, "Feature.java"), `package com.example.feature;
public class Feature {
    int layout = R.layout.screen;
    String value() { return getString(R.string.source_used); }
}`)
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	writeTestFile(t, manifest, `<manifest xmlns:android="http://schemas.android.com/apk/res/android" package="com.example.feature">
    <application android:label="@string/manifest_used">
        <activity android:name=".Feature"/>
    </application>
</manifest>`)

	output := filepath.Join(dir, "out")
	module := Module{ResDirs: []string{res}, JavaDirs: []string{filepath.Dir(java)}, Manifest: manifest}
	filtered, err := shrinkFeatureResources(module, output)
	if err != nil {
		t.Fatalf("shrinkFeatureResources() error = %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered dirs = %v", filtered)
	}
	output = filtered[0]

	data, err := os.ReadFile(filepath.Join(output, "values", "strings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	stringsXML := string(data)
	for _, kept := range []string{"source_used", "manifest_used", "xml_used", "public_kept", "tools_kept"} {
		if !strings.Contains(stringsXML, kept) {
			t.Errorf("shrunk values are missing %q:\n%s", kept, stringsXML)
		}
	}
	if strings.Contains(stringsXML, "unused") {
		t.Errorf("shrunk values keep unused resources:\n%s", stringsXML)
	}
	for _, name := range []string{"drawable/icon.xml", "layout/screen.xml"} {
		if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(name))); err != nil {
			t.Errorf("kept file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "drawable", "unused.xml")); !os.IsNotExist(err) {
		t.Errorf("unused drawable was not removed: %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
