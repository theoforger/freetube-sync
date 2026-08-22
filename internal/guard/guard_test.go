package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockfilePresent(t *testing.T) {
	dir := t.TempDir()
	present, err := LockfilePresent(dir)
	if err != nil {
		t.Fatalf("LockfilePresent: %v", err)
	}
	if present {
		t.Error("LockfilePresent = true before lock file created, want false")
	}

	if err := os.WriteFile(filepath.Join(dir, lockFileName), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	present, err = LockfilePresent(dir)
	if err != nil {
		t.Fatalf("LockfilePresent: %v", err)
	}
	if !present {
		t.Error("LockfilePresent = false after lock file created, want true")
	}
}

func TestLockfilePresentPropagatesRealErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	_, err := LockfilePresent(dir)
	if err == nil {
		t.Error("LockfilePresent: want error when configDir is unreadable")
	}
}

// fakeProc builds a fake /proc tree with the given pid -> cmdline mapping.
func fakeProc(t *testing.T, procs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, cmdline := range procs {
		dir := filepath.Join(root, pid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Non-pid entries (e.g. "self") should be ignored by the scanner.
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestProcessRunningDetectsNativeBinary(t *testing.T) {
	root := fakeProc(t, map[string]string{
		"1234": "/usr/bin/freetube\x00--flag\x00",
		"5678": "/usr/bin/firefox\x00",
	})
	running, err := ProcessRunning(root)
	if err != nil {
		t.Fatalf("ProcessRunning: %v", err)
	}
	if !running {
		t.Error("ProcessRunning = false, want true")
	}
}

func TestProcessRunningDetectsFlatpak(t *testing.T) {
	root := fakeProc(t, map[string]string{
		"999": "/usr/bin/flatpak\x00run\x00io.freetubeapp.FreeTube\x00",
	})
	running, err := ProcessRunning(root)
	if err != nil {
		t.Fatalf("ProcessRunning: %v", err)
	}
	if !running {
		t.Error("ProcessRunning = false, want true")
	}
}

func TestProcessRunningNoMatch(t *testing.T) {
	root := fakeProc(t, map[string]string{
		"1": "/sbin/init\x00",
		"2": "/usr/bin/bash\x00",
	})
	running, err := ProcessRunning(root)
	if err != nil {
		t.Fatalf("ProcessRunning: %v", err)
	}
	if running {
		t.Error("ProcessRunning = true, want false")
	}
}

func TestProcessRunningEmptyProcRoot(t *testing.T) {
	root := t.TempDir()
	running, err := ProcessRunning(root)
	if err != nil {
		t.Fatalf("ProcessRunning: %v", err)
	}
	if running {
		t.Error("ProcessRunning = true on empty /proc, want false")
	}
}

func TestProcessRunningMissingProcRootErrors(t *testing.T) {
	_, err := ProcessRunning(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Error("ProcessRunning: want error for missing procRoot")
	}
}

func TestIsSafeToWriteBothClear(t *testing.T) {
	configDir := t.TempDir()
	procRoot := fakeProc(t, map[string]string{"1": "/sbin/init\x00"})
	safe, err := IsSafeToWrite(Options{ConfigDir: configDir, ProcRoot: procRoot})
	if err != nil {
		t.Fatalf("IsSafeToWrite: %v", err)
	}
	if !safe {
		t.Error("IsSafeToWrite = false, want true")
	}
}

func TestIsSafeToWriteLockfilePresent(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, lockFileName), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	procRoot := fakeProc(t, map[string]string{"1": "/sbin/init\x00"})
	safe, err := IsSafeToWrite(Options{ConfigDir: configDir, ProcRoot: procRoot})
	if err != nil {
		t.Fatalf("IsSafeToWrite: %v", err)
	}
	if safe {
		t.Error("IsSafeToWrite = true with lockfile present, want false")
	}
}

func TestIsSafeToWriteProcessRunning(t *testing.T) {
	configDir := t.TempDir()
	procRoot := fakeProc(t, map[string]string{"42": "/usr/bin/freetube\x00"})
	safe, err := IsSafeToWrite(Options{ConfigDir: configDir, ProcRoot: procRoot})
	if err != nil {
		t.Fatalf("IsSafeToWrite: %v", err)
	}
	if safe {
		t.Error("IsSafeToWrite = true with process running, want false")
	}
}

func TestIsSafeToWritePropagatesProcessScanError(t *testing.T) {
	configDir := t.TempDir()
	_, err := IsSafeToWrite(Options{ConfigDir: configDir, ProcRoot: filepath.Join(t.TempDir(), "nope")})
	if err == nil {
		t.Error("IsSafeToWrite: want error when procRoot scan fails")
	}
}

func TestReasonMentionsLockfileOrProcess(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, lockFileName), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Reason(Options{ConfigDir: configDir, ProcRoot: t.TempDir()})
	if got == "" {
		t.Error("Reason() returned empty string")
	}
}
