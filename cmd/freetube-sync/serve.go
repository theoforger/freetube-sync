package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"freetube-sync/internal/config"
	"freetube-sync/internal/server"
)

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", "", "address to listen on, e.g. :8080")
	dataDir := fs.String("data", "", "directory for the canonical state file")
	token := fs.String("token", "", "bearer token clients must present")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Resolve(config.Config{Listen: *listen, DataDir: *dataDir, Token: *token})
	if err != nil {
		return err
	}
	if err := config.ValidateListen(cfg.Listen); err != nil {
		return err
	}
	if err := config.ValidateDataDir(cfg.DataDir); err != nil {
		return err
	}
	if err := config.ValidateToken(cfg.Token); err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}

	store := server.NewStore(filepath.Join(cfg.DataDir, server.StateFileName))
	handler := &server.Handler{Store: store, Token: cfg.Token}

	slog.Info("freetube-sync serve starting", "listen", cfg.Listen, "data", cfg.DataDir)
	if err := http.ListenAndServe(cfg.Listen, handler.Mux()); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
