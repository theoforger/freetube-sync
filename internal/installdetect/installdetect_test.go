package installdetect

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// setupHome creates a temp home dir and optionally the marker dirs for a
// flatpak and/or native install.
func setupHome(t *testing.T, flatpak, native bool) string {
	t.Helper()
	home := t.TempDir()
	if flatpak {
		_, appDir := flatpakPaths(home)
		if err := os.MkdirAll(appDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if native {
		_, configDir := nativePaths(home)
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func noFlatpakInfo(string) bool { return false }

func lookPathFound(path string) (string, error) { return "/usr/bin/freetube", nil }
func lookPathMissing(path string) (string, error) {
	return "", errors.New("executable file not found in $PATH")
}

func TestDetectFlatpakOnly(t *testing.T) {
	home := setupHome(t, true, false)
	result, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo, // dir presence alone should be enough
		LookPath:    lookPathMissing,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.Kind != Flatpak {
		t.Errorf("Kind = %q, want %q", result.Kind, Flatpak)
	}
	wantDB := filepath.Join(home, ".var", "app", flatpakAppID, "config", "FreeTube", "profiles.db")
	if result.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q", result.DBPath, wantDB)
	}
	wantCmd := []string{"flatpak", "run", flatpakAppID}
	if !equalStrings(result.LaunchCommand, wantCmd) {
		t.Errorf("LaunchCommand = %v, want %v", result.LaunchCommand, wantCmd)
	}
}

func TestDetectFlatpakViaInfoCommand(t *testing.T) {
	// No directory on disk, but `flatpak info` reports it's registered.
	home := t.TempDir()
	result, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: func(appID string) bool { return appID == flatpakAppID },
		LookPath:    lookPathMissing,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.Kind != Flatpak {
		t.Errorf("Kind = %q, want %q", result.Kind, Flatpak)
	}
}

func TestDetectNativeOnly(t *testing.T) {
	home := setupHome(t, false, true)
	result, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.Kind != Native {
		t.Errorf("Kind = %q, want %q", result.Kind, Native)
	}
	wantDB := filepath.Join(home, ".config", "FreeTube", "profiles.db")
	if result.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q", result.DBPath, wantDB)
	}
	wantCmd := []string{"/usr/bin/freetube"}
	if !equalStrings(result.LaunchCommand, wantCmd) {
		t.Errorf("LaunchCommand = %v, want %v", result.LaunchCommand, wantCmd)
	}
}

func TestDetectNativeDirWithoutBinaryIsNotNative(t *testing.T) {
	// Config dir exists but `freetube` isn't resolvable: not a valid native
	// install, and nothing else is present either -> hard error.
	home := setupHome(t, false, true)
	_, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathMissing,
	})
	if err == nil {
		t.Fatal("Detect: want error, got nil")
	}
}

func TestDetectNeitherFoundIsHardError(t *testing.T) {
	home := t.TempDir()
	_, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathMissing,
	})
	if err == nil {
		t.Fatal("Detect: want error, got nil")
	}
}

func TestDetectBothPresentPromptsAndPersists(t *testing.T) {
	home := setupHome(t, true, true)
	var promptCalled bool
	var persistedKind Kind
	result, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
		Prompt: func(flatpakDB, nativeDB string) (Kind, error) {
			promptCalled = true
			if flatpakDB == "" || nativeDB == "" {
				t.Error("Prompt called with empty db path")
			}
			return Native, nil
		},
		Persist: func(k Kind) error {
			persistedKind = k
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !promptCalled {
		t.Error("Prompt was not called despite both installs present")
	}
	if result.Kind != Native {
		t.Errorf("Kind = %q, want %q", result.Kind, Native)
	}
	if persistedKind != Native {
		t.Errorf("Persist called with %q, want %q", persistedKind, Native)
	}
}

func TestDetectBothPresentWithPersistedChoiceSkipsPrompt(t *testing.T) {
	home := setupHome(t, true, true)
	result, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
		Persisted:   Flatpak,
		Prompt: func(string, string) (Kind, error) {
			t.Fatal("Prompt should not be called when a persisted choice exists")
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.Kind != Flatpak {
		t.Errorf("Kind = %q, want %q", result.Kind, Flatpak)
	}
}

func TestDetectBothPresentNoPromptAvailable(t *testing.T) {
	home := setupHome(t, true, true)
	_, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
	})
	if err == nil {
		t.Fatal("Detect: want error when both present and no Prompt configured")
	}
}

func TestDetectOverrideSkipsPromptAndDetection(t *testing.T) {
	home := setupHome(t, true, true)
	result, err := Detect(Options{
		HomeDir:     home,
		Override:    Flatpak,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
		Prompt: func(string, string) (Kind, error) {
			t.Fatal("Prompt should not be called when --install override is set")
			return "", nil
		},
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if result.Kind != Flatpak {
		t.Errorf("Kind = %q, want %q", result.Kind, Flatpak)
	}
}

func TestDetectOverrideToAbsentInstallErrors(t *testing.T) {
	home := setupHome(t, false, true) // only native present
	_, err := Detect(Options{
		HomeDir:     home,
		Override:    Flatpak,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
	})
	if err == nil {
		t.Fatal("Detect: want error when overriding to an install that isn't present")
	}
}

func TestDetectOverrideInvalidKind(t *testing.T) {
	home := t.TempDir()
	_, err := Detect(Options{
		HomeDir:  home,
		Override: Kind("bogus"),
	})
	if err == nil {
		t.Fatal("Detect: want error for invalid --install value")
	}
}

func TestDetectPromptErrorPropagates(t *testing.T) {
	home := setupHome(t, true, true)
	_, err := Detect(Options{
		HomeDir:     home,
		FlatpakInfo: noFlatpakInfo,
		LookPath:    lookPathFound,
		Prompt: func(string, string) (Kind, error) {
			return "", errors.New("user aborted")
		},
	})
	if err == nil {
		t.Fatal("Detect: want error when Prompt fails")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
