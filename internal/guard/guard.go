// Package guard implements the "is FreeTube running" safety check that
// every profiles.db write must pass first (CLAUDE.md invariant #1). It
// uses two independent signals: the Chromium/Electron-style SingletonLock
// file FreeTube holds in its config dir while running, and a /proc scan
// for a live FreeTube process (covering both native and Flatpak).
package guard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// lockFileName is the Chromium/Electron single-instance lock FreeTube
// holds open for as long as it's running.
const lockFileName = "SingletonLock"

// nativeBinaryName and flatpakAppIDLower identify a FreeTube process by
// whole argv token, not substring — see processMatches.
const (
	nativeBinaryName  = "freetube"
	flatpakAppIDLower = "io.freetubeapp.freetube"
)

// Options configures a guard check.
type Options struct {
	// ConfigDir is the directory containing profiles.db (and, while
	// FreeTube runs, SingletonLock) — the same dir installdetect.Result's
	// DBPath lives in.
	ConfigDir string
	// ProcRoot overrides "/proc", for testing. Empty means "/proc".
	ProcRoot string
}

func (o Options) procRoot() string {
	if o.ProcRoot != "" {
		return o.ProcRoot
	}
	return "/proc"
}

// LockfilePresent reports whether FreeTube's SingletonLock file exists in
// configDir.
func LockfilePresent(configDir string) (bool, error) {
	_, err := os.Lstat(filepath.Join(configDir, lockFileName))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ProcessRunning scans procRoot (normally /proc) for a running FreeTube
// process, matching either the native binary name or the Flatpak app ID
// against each process's cmdline. The caller's own PID is excluded: a
// `run` invoked with an explicit launch-command override (e.g. the desktop
// launcher's `freetube-sync run -- flatpak run io.freetubeapp.FreeTube`)
// carries the Flatpak app ID directly in its own argv, so without this
// exclusion every guard check made from inside that process — both the
// pre-launch and post-exit sync — would match itself and report FreeTube
// as running unconditionally, regardless of whether it actually is.
func ProcessRunning(procRoot string) (bool, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false, err
	}
	selfPID := os.Getpid()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a /proc/<pid> entry
		}
		if pid == selfPID {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "cmdline"))
		if err != nil {
			// Process may have exited between ReadDir and here, or we
			// lack permission to read it — neither is fatal to the scan.
			continue
		}
		if processMatches(cmdline) {
			return true, nil
		}
	}
	return false, nil
}

// processMatches inspects a /proc/<pid>/cmdline blob — NUL-separated
// argv, per the kernel's format — for FreeTube. It compares whole argv
// tokens (by basename), not a substring search over the raw blob: a
// substring match on the full cmdline would false-positive on, say,
// `vim notes-about-freetube.md` or a download landing in
// ~/Downloads/freetube-linux.tar.gz, both of which would otherwise look
// like "FreeTube is running" and block every sync indefinitely.
func processMatches(cmdline []byte) bool {
	for _, arg := range bytes.Split(cmdline, []byte{0}) {
		if len(arg) == 0 {
			continue
		}
		token := strings.ToLower(filepath.Base(string(arg)))
		if token == nativeBinaryName || token == flatpakAppIDLower {
			return true
		}
	}
	return false
}

// IsSafeToWrite reports whether it's safe to write profiles.db right now.
// Both signals must come back clear; either one alone is enough to say no.
func IsSafeToWrite(opts Options) (bool, error) {
	locked, err := LockfilePresent(opts.ConfigDir)
	if err != nil {
		return false, fmt.Errorf("check lockfile: %w", err)
	}
	if locked {
		return false, nil
	}
	running, err := ProcessRunning(opts.procRoot())
	if err != nil {
		return false, fmt.Errorf("scan processes: %w", err)
	}
	return !running, nil
}

// Reason returns a short human-readable explanation for why writing is
// currently unsafe. It re-checks both signals, so call it only after
// IsSafeToWrite has returned (false, nil); on error it returns a generic
// message rather than propagating the error.
func Reason(opts Options) string {
	locked, err := LockfilePresent(opts.ConfigDir)
	if err == nil && locked {
		return "FreeTube's lock file is present"
	}
	running, err := ProcessRunning(opts.procRoot())
	if err == nil && running {
		return "a FreeTube process is running"
	}
	return "FreeTube appears to be running"
}
