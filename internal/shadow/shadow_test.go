package shadow

import (
	"os"
	"path/filepath"
	"testing"

	"freetube-sync/internal/nedb"
)

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-synced.json")
	snap, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snap.Subscriptions) != 0 {
		t.Errorf("Subscriptions = %v, want empty", snap.Subscriptions)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "last-synced.json")
	want := Snapshot{Subscriptions: []nedb.Subscription{
		{ID: "UC1", Name: "One", Thumbnail: "https://example.com/1.jpg"},
		{ID: "UC2", Name: "Two"},
	}}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Subscriptions) != 2 || got.Subscriptions[0] != want.Subscriptions[0] || got.Subscriptions[1] != want.Subscriptions[1] {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestSaveOverwritesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-synced.json")
	if err := Save(path, Snapshot{Subscriptions: []nedb.Subscription{{ID: "UC1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Snapshot{Subscriptions: []nedb.Subscription{{ID: "UC2"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Subscriptions) != 1 || got.Subscriptions[0].ID != "UC2" {
		t.Errorf("Load() = %+v, want single UC2", got)
	}
	// No stray temp files left in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "last-synced.json" {
		t.Errorf("unexpected directory contents: %v", entries)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-synced.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load: want error for malformed JSON")
	}
}

func TestPathHonorsXDGConfigHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(base, "freetube-sync", FileName)
	if path != want {
		t.Errorf("Path() = %q, want %q", path, want)
	}
}
