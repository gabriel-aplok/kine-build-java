package minecraft

import (
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	data := `{
  "id": "1.20.1",
  "mainClass": "net.minecraft.client.main.Main",
  "libraries": [
    {"name": "com.mojang:brigadier:1.0.18"},
    {"name": "org.lwjgl:lwjgl:3.3.1:natives-linux"}
  ],
  "downloads": {
    "client": {"url": "https://example/client.jar", "sha1": "abc", "size": 1234}
  },
  "assetIndex": {"id": "5"}
}`

	version, err := ParseVersion([]byte(data))
	if err != nil {
		t.Fatalf("ParseVersion() error = %v", err)
	}
	if version.ID != "1.20.1" {
		t.Errorf("ID = %q", version.ID)
	}
	if len(version.Libraries) != 2 {
		t.Fatalf("Libraries = %d, want 2", len(version.Libraries))
	}
	if version.Libraries[0].Group != "com.mojang" || version.Libraries[0].Artifact != "brigadier" {
		t.Errorf("library 0 = %+v", version.Libraries[0])
	}
	if version.Libraries[1].Classifier != "natives-linux" {
		t.Errorf("library 1 = %+v", version.Libraries[1])
	}
	if version.Client.URL != "https://example/client.jar" || version.Client.SHA1 != "abc" || version.Client.Size != 1234 {
		t.Errorf("Client = %+v", version.Client)
	}
	if version.AssetIndex != "5" {
		t.Errorf("AssetIndex = %q", version.AssetIndex)
	}
}

func TestParseVersionMissingID(t *testing.T) {
	_, err := ParseVersion([]byte(`{"libraries": []}`))
	if err == nil {
		t.Fatal("ParseVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Errorf("error %q does not mention the id", err)
	}
}

func TestParseVersionBadLibrary(t *testing.T) {
	_, err := ParseVersion([]byte(`{"id": "1", "libraries": [{"name": "nope"}]}`))
	if err == nil {
		t.Fatal("ParseVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "library 0") {
		t.Errorf("error %q does not name the library", err)
	}
}

func TestParseVersionEmptyLibrary(t *testing.T) {
	_, err := ParseVersion([]byte(`{"id": "1", "libraries": [{"name": ""}]}`))
	if err == nil {
		t.Fatal("ParseVersion() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "library 0") {
		t.Errorf("error %q does not name the library", err)
	}
}

func TestParseVersionInvalidJSON(t *testing.T) {
	if _, err := ParseVersion([]byte("{")); err == nil {
		t.Fatal("ParseVersion() error = nil, want error")
	}
}
