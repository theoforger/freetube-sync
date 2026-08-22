package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"freetube-sync/internal/clientsync"
	"freetube-sync/internal/config"
	"freetube-sync/internal/guard"
	"freetube-sync/internal/shadow"
)

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	serverURL := fs.String("server", "", "server URL, e.g. https://host:8080")
	token := fs.String("token", "", "bearer token")
	install := fs.String("install", "", "override install detection: flatpak|native")
	deviceID := fs.String("device-id", "", "optional device identifier sent to the server, logged only")
	dryRun := fs.Bool("dry-run", false, "compute and print the merge result without writing anything")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Resolve(config.Config{ServerURL: *serverURL, Token: *token, Install: *install})
	if err != nil {
		return err
	}
	normalizedURL, err := config.ValidateServerURL(cfg.ServerURL)
	if err != nil {
		return err
	}
	cfg.ServerURL = normalizedURL
	if err := config.ValidateToken(cfg.Token); err != nil {
		return err
	}

	result, err := detectInstall(cfg)
	if err != nil {
		return err
	}

	shadowPath, err := shadow.Path()
	if err != nil {
		return err
	}

	outcome, err := clientsync.Run(clientsync.Deps{
		DBPath:     result.DBPath,
		ShadowPath: shadowPath,
		ServerURL:  cfg.ServerURL,
		Token:      cfg.Token,
		DeviceID:   *deviceID,
		DryRun:     *dryRun,
		GuardOpts:  guard.Options{ConfigDir: filepath.Dir(result.DBPath)},
	})
	if err != nil {
		return err
	}

	reportSyncOutcome(outcome)
	return nil
}

// reportSyncOutcome prints a one-line summary of a clientsync.Outcome.
// Shared with cmd_run.go's before/after syncs.
func reportSyncOutcome(o clientsync.Outcome) {
	switch {
	case o.Skipped:
		fmt.Printf("sync: skipped (%s)\n", o.SkipReason)
	case o.NetworkFailed:
		fmt.Printf("sync: server unreachable, left local state untouched (%s)\n", o.NetworkError)
	case o.DryRun:
		fmt.Printf("sync: dry run — would add %d, remove %d\n", len(o.Added), len(o.Removed))
	default:
		fmt.Printf("sync: ok — added %d, removed %d, %d channel(s) subscribed\n", len(o.Added), len(o.Removed), len(o.Subscribed))
	}
}
