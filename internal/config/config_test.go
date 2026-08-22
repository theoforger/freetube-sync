package config

import (
	"os"
	"path/filepath"
	"testing"
)

// withXDGConfigHome points XDG_CONFIG_HOME at a fresh temp dir for the
// duration of the test, so Dir/Path/Load/Save are isolated from the real
// user config.
func withXDGConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"FREETUBE_SYNC_SERVER",
		"FREETUBE_SYNC_TOKEN",
		"FREETUBE_SYNC_DATA",
		"FREETUBE_SYNC_LISTEN",
		"FREETUBE_SYNC_INSTALL",
	} {
		t.Setenv(k, "")
	}
}

func TestDirHonorsXDGConfigHome(t *testing.T) {
	base := withXDGConfigHome(t)
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := filepath.Join(base, "freetube-sync")
	if dir != want {
		t.Errorf("Dir() = %q, want %q", dir, want)
	}
}

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	withXDGConfigHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != (Config{}) {
		t.Errorf("Load() = %+v, want zero value", cfg)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	withXDGConfigHome(t)
	want := Config{ServerURL: "https://host:8080", Token: "secret", Install: "flatpak"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	base := withXDGConfigHome(t)
	dir := filepath.Join(base, "freetube-sync")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("Load() with malformed JSON: want error, got nil")
	}
}

func TestResolvePrecedence(t *testing.T) {
	withXDGConfigHome(t)
	clearEnv(t)

	// File sets everything.
	if err := Save(Config{
		ServerURL: "file-server",
		Token:     "file-token",
		DataDir:   "file-data",
		Listen:    "file-listen",
		Install:   "file-install",
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("file only", func(t *testing.T) {
		got, err := Resolve(Config{})
		if err != nil {
			t.Fatal(err)
		}
		want := Config{"file-server", "file-token", "file-data", "file-listen", "file-install"}
		if got != want {
			t.Errorf("Resolve() = %+v, want %+v", got, want)
		}
	})

	t.Run("env overrides file", func(t *testing.T) {
		t.Setenv("FREETUBE_SYNC_SERVER", "env-server")
		t.Setenv("FREETUBE_SYNC_TOKEN", "env-token")
		got, err := Resolve(Config{})
		if err != nil {
			t.Fatal(err)
		}
		if got.ServerURL != "env-server" || got.Token != "env-token" {
			t.Errorf("Resolve() = %+v, want env overrides applied", got)
		}
		// Fields not set by env still come from the file.
		if got.DataDir != "file-data" {
			t.Errorf("Resolve().DataDir = %q, want %q", got.DataDir, "file-data")
		}
	})

	t.Run("flags override env and file", func(t *testing.T) {
		t.Setenv("FREETUBE_SYNC_SERVER", "env-server")
		got, err := Resolve(Config{ServerURL: "flag-server"})
		if err != nil {
			t.Fatal(err)
		}
		if got.ServerURL != "flag-server" {
			t.Errorf("Resolve().ServerURL = %q, want %q", got.ServerURL, "flag-server")
		}
	})
}
