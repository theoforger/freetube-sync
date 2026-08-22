// Package installdetect resolves whether FreeTube is installed as a
// Flatpak or natively, and derives the profiles.db path and launch command
// for whichever is in play.
package installdetect

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Kind identifies which FreeTube installation to target.
type Kind string

const (
	Flatpak Kind = "flatpak"
	Native  Kind = "native"

	flatpakAppID = "io.freetubeapp.FreeTube"
)

// Result is the outcome of detection: which kind was chosen, and the
// concrete paths/commands derived from it.
type Result struct {
	Kind          Kind
	DBPath        string
	LaunchCommand []string
}

// Prompter asks the user to choose between the two candidate installations
// when both are present, returning their choice.
type Prompter func(flatpakDBPath, nativeDBPath string) (Kind, error)

// Options controls detection behavior. Zero-value fields fall back to real
// OS interaction; tests should override HomeDir, LookPath, FlatpakInfo, and
// Prompt to avoid touching the real filesystem/environment.
type Options struct {
	// HomeDir overrides os.UserHomeDir(). Empty means use the real home dir.
	HomeDir string
	// Override, if non-empty, forces this Kind and skips detection/prompt
	// entirely. Corresponds to the --install flag.
	Override Kind
	// Persisted is a previously-saved choice (from config.json) used to
	// break the "both present" tie without re-prompting. Empty means none.
	Persisted Kind
	// LookPath overrides exec.LookPath, used to resolve the native
	// `freetube` binary.
	LookPath func(file string) (string, error)
	// FlatpakInfo overrides the `flatpak info <appID>` presence check. It
	// should return true iff the Flatpak app is registered.
	FlatpakInfo func(appID string) bool
	// Prompt is invoked only when both installs are present and there is
	// no Persisted choice.
	Prompt Prompter
	// Persist is called with the user's choice after a prompt, so callers
	// can save it (e.g. to config.json). Nil means don't persist.
	Persist func(Kind) error
}

func (o Options) homeDir() (string, error) {
	if o.HomeDir != "" {
		return o.HomeDir, nil
	}
	return os.UserHomeDir()
}

func (o Options) lookPath() func(string) (string, error) {
	if o.LookPath != nil {
		return o.LookPath
	}
	return exec.LookPath
}

func (o Options) flatpakInfo() func(string) bool {
	if o.FlatpakInfo != nil {
		return o.FlatpakInfo
	}
	return func(appID string) bool {
		return exec.Command("flatpak", "info", appID).Run() == nil
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func flatpakPaths(home string) (dbPath string, appDir string) {
	appDir = filepath.Join(home, ".var", "app", flatpakAppID)
	dbPath = filepath.Join(appDir, "config", "FreeTube", "profiles.db")
	return dbPath, appDir
}

func nativePaths(home string) (dbPath string, configDir string) {
	configDir = filepath.Join(home, ".config", "FreeTube")
	dbPath = filepath.Join(configDir, "profiles.db")
	return dbPath, configDir
}

// flatpakPresent reports whether a Flatpak install of FreeTube is detected.
func flatpakPresent(home string, opts Options) bool {
	_, appDir := flatpakPaths(home)
	return opts.flatpakInfo()(flatpakAppID) || dirExists(appDir)
}

// nativePresent reports whether a native install of FreeTube is detected,
// returning the resolved binary path when it is.
func nativePresent(home string, opts Options) (binPath string, ok bool) {
	_, configDir := nativePaths(home)
	if !dirExists(configDir) {
		return "", false
	}
	resolved, err := opts.lookPath()("freetube")
	if err != nil {
		return "", false
	}
	return resolved, true
}

func flatpakResult(home string) Result {
	dbPath, _ := flatpakPaths(home)
	return Result{
		Kind:          Flatpak,
		DBPath:        dbPath,
		LaunchCommand: []string{"flatpak", "run", flatpakAppID},
	}
}

func nativeResult(home, binPath string) Result {
	dbPath, _ := nativePaths(home)
	return Result{
		Kind:          Native,
		DBPath:        dbPath,
		LaunchCommand: []string{binPath},
	}
}

// Detect resolves the FreeTube installation to target, per the precedence
// described in CLAUDE.md: explicit --install override, then persisted
// choice (only relevant when both are present), then auto-detection, then
// (if both present with no prior choice) an interactive prompt. Returns a
// hard error if neither installation is found.
func Detect(opts Options) (Result, error) {
	home, err := opts.homeDir()
	if err != nil {
		return Result{}, fmt.Errorf("resolve home directory: %w", err)
	}

	if opts.Override != "" {
		return resolveExplicit(home, opts, opts.Override, "--install")
	}

	flatpakOK := flatpakPresent(home, opts)
	nativeBin, nativeOK := nativePresent(home, opts)

	switch {
	case flatpakOK && nativeOK:
		if opts.Persisted != "" {
			return resolveExplicit(home, opts, opts.Persisted, "persisted config")
		}
		if opts.Prompt == nil {
			return Result{}, errors.New("both Flatpak and native FreeTube installs found; no prompt available to choose (pass --install)")
		}
		flatpakDB, _ := flatpakPaths(home)
		nativeDB, _ := nativePaths(home)
		choice, err := opts.Prompt(flatpakDB, nativeDB)
		if err != nil {
			return Result{}, fmt.Errorf("prompt for install choice: %w", err)
		}
		result, err := resolveExplicit(home, opts, choice, "prompt")
		if err != nil {
			return Result{}, err
		}
		if opts.Persist != nil {
			if err := opts.Persist(choice); err != nil {
				return Result{}, fmt.Errorf("persist install choice: %w", err)
			}
		}
		return result, nil
	case flatpakOK:
		return flatpakResult(home), nil
	case nativeOK:
		return nativeResult(home, nativeBin), nil
	default:
		return Result{}, errors.New("no FreeTube installation found (checked Flatpak and native paths)")
	}
}

// resolveExplicit builds a Result for an explicitly-chosen kind (from a
// flag, persisted config, or prompt answer), validating that the
// corresponding installation actually exists.
func resolveExplicit(home string, opts Options, kind Kind, source string) (Result, error) {
	switch kind {
	case Flatpak:
		if !flatpakPresent(home, opts) {
			return Result{}, fmt.Errorf("install=%s from %s, but no Flatpak FreeTube install was found", kind, source)
		}
		return flatpakResult(home), nil
	case Native:
		binPath, ok := nativePresent(home, opts)
		if !ok {
			return Result{}, fmt.Errorf("install=%s from %s, but no native FreeTube install was found", kind, source)
		}
		return nativeResult(home, binPath), nil
	default:
		return Result{}, fmt.Errorf("invalid install kind %q from %s (want %q or %q)", kind, source, Flatpak, Native)
	}
}
