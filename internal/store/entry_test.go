package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEntryPath(t *testing.T) {
	store := newTestStore(t)
	fingerprint := strings.Repeat("a", 64)

	got := store.entryPath(fingerprint)
	want := filepath.Join(store.storeRoot, "tasks", "aa", fingerprint+".json")
	if got != want {
		t.Errorf("entryPath() = %q, want %q", got, want)
	}
}

func TestEntryRoundTrip(t *testing.T) {
	store := newTestStore(t)
	e := testEntry(strings.Repeat("a", 64))

	if err := store.writeEntry(e); err != nil {
		t.Fatalf("writeEntry() error = %v", err)
	}
	got, err := store.readEntry(e.Fingerprint)
	if err != nil {
		t.Fatalf("readEntry() error = %v", err)
	}
	if !reflect.DeepEqual(got, &e) {
		t.Errorf("entry round trip = %+v, want %+v", got, e)
	}
}

func TestWriteEntryNoTempFiles(t *testing.T) {
	store := newTestStore(t)
	if err := store.writeEntry(testEntry(strings.Repeat("a", 64))); err != nil {
		t.Fatalf("writeEntry() error = %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(store.storeRoot, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir has %d entries, want 0", len(entries))
	}
}

func TestReadEntryMissing(t *testing.T) {
	store := newTestStore(t)

	_, err := store.readEntry(strings.Repeat("0", 64))
	if !errors.Is(err, ErrEntryMissing) {
		t.Errorf("readEntry() error = %v, want ErrEntryMissing", err)
	}
}

func TestEntryRejectsBadVersion(t *testing.T) {
	store := newTestStore(t)
	e := testEntry(strings.Repeat("a", 64))
	e.Version = 2

	if err := store.writeEntry(e); err == nil {
		t.Fatal("writeEntry() error = nil, want error")
	}
}

func TestEntryRejectsBadFingerprint(t *testing.T) {
	store := newTestStore(t)
	e := testEntry("abc")

	if err := store.writeEntry(e); err == nil {
		t.Fatal("writeEntry() error = nil, want error")
	}
}

func TestEntryRejectsBadHash(t *testing.T) {
	store := newTestStore(t)
	e := testEntry(strings.Repeat("a", 64))
	e.Outputs[0].Hash = "abc"

	if err := store.writeEntry(e); err == nil {
		t.Fatal("writeEntry() error = nil, want error")
	}
}

func TestEntryRejectsUnknownMode(t *testing.T) {
	store := newTestStore(t)
	e := testEntry(strings.Repeat("a", 64))
	e.Outputs[0].Mode = "blob"

	if err := store.writeEntry(e); err == nil {
		t.Fatal("writeEntry() error = nil, want error")
	}
}

func TestEntryRejectsMissingOutputPath(t *testing.T) {
	store := newTestStore(t)
	e := testEntry(strings.Repeat("a", 64))
	e.Outputs[0].Path = ""

	if err := store.writeEntry(e); err == nil {
		t.Fatal("writeEntry() error = nil, want error")
	}
}

func TestDecodeEntryRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeEntry([]byte("{")); err == nil {
		t.Fatal("decodeEntry() error = nil, want error")
	}
}

func TestReadEntryRejectsMismatchedFingerprint(t *testing.T) {
	store := newTestStore(t)
	expected := strings.Repeat("a", 64)
	data, err := json.Marshal(testEntry(strings.Repeat("b", 64)))
	if err != nil {
		t.Fatal(err)
	}

	path := store.entryPath(expected)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.readEntry(expected); err == nil {
		t.Fatal("readEntry() error = nil, want error")
	}
}

func testEntry(fingerprint string) entry {
	return entry{
		Version:     entryVersion,
		Fingerprint: fingerprint,
		Outputs: []entryOutput{{
			Path: "app/out.txt",
			Mode: "file",
			Hash: strings.Repeat("b", 64),
			Size: 12,
			Perm: 0o644,
		}},
		Size:     12,
		Created:  time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		Accessed: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
	}
}
