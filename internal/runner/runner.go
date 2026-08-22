// Package runner implements the `run` subcommand's launch cycle: sync,
// exec the FreeTube launch command with inherited stdio, wait for it to
// exit, sync again. Both syncs go through internal/clientsync, so
// network/server failures fail open exactly as they do for a standalone
// `sync` (CLAUDE.md invariant #6) — a down server never prevents FreeTube
// from launching or closing normally.
package runner

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"

	"freetube-sync/internal/clientsync"
)

// Deps configures one run cycle.
type Deps struct {
	// Sync is reused, unmodified, for both the pre- and post-launch sync.
	Sync clientsync.Deps

	// LaunchCommand is argv for the FreeTube process: LaunchCommand[0] is
	// resolved via exec.LookPath (like a shell would) if it isn't already
	// an absolute/relative path.
	LaunchCommand []string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Logger *slog.Logger // defaults to slog.Default()
}

func (d Deps) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// Outcome reports both syncs' results and the launched process's exit
// code.
type Outcome struct {
	PreSync  clientsync.Outcome
	PostSync clientsync.Outcome
	ExitCode int
}

// Run performs sync -> launch -> wait -> sync. An error here means either
// sync hit a genuine local error (see clientsync.Run's doc) or the launch
// command itself couldn't be started at all (e.g. binary not found) —
// never that FreeTube exited non-zero, which is reported via
// Outcome.ExitCode instead.
func Run(deps Deps) (Outcome, error) {
	if len(deps.LaunchCommand) == 0 {
		return Outcome{}, errors.New("no launch command configured")
	}

	pre, err := clientsync.Run(deps.Sync)
	if err != nil {
		return Outcome{}, fmt.Errorf("pre-launch sync: %w", err)
	}
	deps.logger().Info("run: pre-launch sync done", "skipped", pre.Skipped, "network_failed", pre.NetworkFailed)

	cmd := exec.Command(deps.LaunchCommand[0], deps.LaunchCommand[1:]...)
	cmd.Stdin = deps.Stdin
	cmd.Stdout = deps.Stdout
	cmd.Stderr = deps.Stderr

	runErr := cmd.Run()
	exitCode := 0
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		exitCode = 0
	case errors.As(runErr, &exitErr):
		// FreeTube ran and exited non-zero (or was signaled) — not a
		// freetube-sync error, just report it.
		exitCode = exitErr.ExitCode()
	default:
		// Couldn't even start it (binary missing, not executable, ...).
		// That's a genuine error, distinct from FreeTube's own exit status.
		return Outcome{PreSync: pre}, fmt.Errorf("launch %v: %w", deps.LaunchCommand, runErr)
	}

	post, err := clientsync.Run(deps.Sync)
	if err != nil {
		return Outcome{PreSync: pre, ExitCode: exitCode}, fmt.Errorf("post-launch sync: %w", err)
	}
	deps.logger().Info("run: post-launch sync done", "skipped", post.Skipped, "network_failed", post.NetworkFailed)

	return Outcome{PreSync: pre, PostSync: post, ExitCode: exitCode}, nil
}
