// Package config resolves freetube-sync settings from flags, environment
// variables, and the on-disk config file, in that precedence order (flags
// win over env, env wins over the file).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds settings shared across subcommands. Zero-value fields mean
// "not set" at every layer (flags, env, file) — none of these values are
// legitimately empty strings once resolved.
type Config struct {
	ServerURL string `json:"serverUrl,omitempty"`
	Token     string `json:"token,omitempty"`
	DataDir   string `json:"dataDir,omitempty"`
	Listen    string `json:"listen,omitempty"`
	// Install is "flatpak" or "native", overriding auto-detection.
	Install string `json:"install,omitempty"`
}

// Dir returns the freetube-sync config directory, honoring XDG_CONFIG_HOME.
func Dir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "freetube-sync"), nil
}

// Path returns the full path to config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config.json. A missing file is not an error; it returns a
// zero-value Config.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return c, nil
}

// Save writes cfg to config.json, creating the directory if needed.
func Save(cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func fromEnv() Config {
	return Config{
		ServerURL: os.Getenv("FREETUBE_SYNC_SERVER"),
		Token:     os.Getenv("FREETUBE_SYNC_TOKEN"),
		DataDir:   os.Getenv("FREETUBE_SYNC_DATA"),
		Listen:    os.Getenv("FREETUBE_SYNC_LISTEN"),
		Install:   os.Getenv("FREETUBE_SYNC_INSTALL"),
	}
}

// overlay returns base with every non-empty field of over applied on top.
func overlay(base, over Config) Config {
	if over.ServerURL != "" {
		base.ServerURL = over.ServerURL
	}
	if over.Token != "" {
		base.Token = over.Token
	}
	if over.DataDir != "" {
		base.DataDir = over.DataDir
	}
	if over.Listen != "" {
		base.Listen = over.Listen
	}
	if over.Install != "" {
		base.Install = over.Install
	}
	return base
}

// Resolve merges flags -> env -> config file, in that precedence order.
// Pass the Config populated from parsed flags; empty fields are treated as
// "not set by the user" and fall through to env, then the file.
func Resolve(flags Config) (Config, error) {
	file, err := Load()
	if err != nil {
		return Config{}, err
	}
	resolved := overlay(file, fromEnv())
	resolved = overlay(resolved, flags)
	return resolved, nil
}
