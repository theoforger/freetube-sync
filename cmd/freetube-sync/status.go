package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"freetube-sync/internal/config"
	"freetube-sync/internal/guard"
)

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	install := fs.String("install", "", "override install detection: flatpak|native")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Resolve(config.Config{Install: *install})
	if err != nil {
		return err
	}

	fmt.Printf("freetube-sync %s\n", version)
	fmt.Printf("resolved config:\n")
	fmt.Printf("  server:  %s\n", nonEmpty(cfg.ServerURL))
	fmt.Printf("  data:    %s\n", nonEmpty(cfg.DataDir))
	fmt.Printf("  listen:  %s\n", nonEmpty(cfg.Listen))
	fmt.Printf("  install: %s\n", nonEmpty(cfg.Install))

	// cfg.Install already folds in --install (flag) > env > the persisted
	// choice from config.json, in that order (config.Resolve). Passing it
	// as Override means: use it if set, otherwise detect fresh and (if
	// both installs are present) prompt once and persist the answer.
	result, err := detectInstall(cfg)
	if err != nil {
		fmt.Printf("install: detection failed: %v\n", err)
		return nil
	}
	fmt.Printf("install:  %s\n", result.Kind)
	fmt.Printf("db path:  %s\n", result.DBPath)
	fmt.Printf("launch:   %s\n", strings.Join(result.LaunchCommand, " "))

	guardOpts := guard.Options{ConfigDir: filepath.Dir(result.DBPath)}
	safe, err := guard.IsSafeToWrite(guardOpts)
	if err != nil {
		fmt.Printf("guard:    check failed: %v\n", err)
		return nil
	}
	if safe {
		fmt.Println("guard:    safe to sync")
	} else {
		fmt.Printf("guard:    FreeTube is running, skipping (%s)\n", guard.Reason(guardOpts))
	}
	return nil
}

func nonEmpty(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}
