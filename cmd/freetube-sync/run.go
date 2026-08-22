package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"freetube-sync/internal/clientsync"
	"freetube-sync/internal/config"
	"freetube-sync/internal/guard"
	"freetube-sync/internal/runner"
	"freetube-sync/internal/shadow"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	serverURL := fs.String("server", "", "server URL, e.g. https://host:8080")
	token := fs.String("token", "", "bearer token")
	install := fs.String("install", "", "override install detection: flatpak|native")
	deviceID := fs.String("device-id", "", "optional device identifier sent to the server, logged only")
	dryRun := fs.Bool("dry-run", false, "compute and print each sync's merge result without writing anything")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Anything after a bare "--" overrides the detected launch command,
	// e.g.: freetube-sync run --server ... --token ... -- /custom/freetube --flag
	explicitLaunch := fs.Args()

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

	launchCmd := result.LaunchCommand
	if len(explicitLaunch) > 0 {
		launchCmd = explicitLaunch
	}

	shadowPath, err := shadow.Path()
	if err != nil {
		return err
	}

	syncDeps := clientsync.Deps{
		DBPath:     result.DBPath,
		ShadowPath: shadowPath,
		ServerURL:  cfg.ServerURL,
		Token:      cfg.Token,
		DeviceID:   *deviceID,
		DryRun:     *dryRun,
		GuardOpts:  guard.Options{ConfigDir: filepath.Dir(result.DBPath)},
	}

	outcome, err := runner.Run(runner.Deps{
		Sync:          syncDeps,
		LaunchCommand: launchCmd,
		Stdin:         os.Stdin,
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	})
	if err != nil {
		return err
	}

	fmt.Print("before launch: ")
	reportSyncOutcome(outcome.PreSync)
	fmt.Print("after exit:    ")
	reportSyncOutcome(outcome.PostSync)
	if outcome.ExitCode != 0 {
		os.Exit(outcome.ExitCode)
	}
	return nil
}
