package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateServerURL parses and validates a server URL, returning a
// normalized form (trailing slash trimmed) or a descriptive error.
func ValidateServerURL(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("server URL is required (--server, FREETUBE_SYNC_SERVER, or config.json's serverUrl)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid server URL %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid server URL %q: missing host", raw)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

// minTokenLength is a floor, not a real strength check — just enough to
// catch "--token test" typos before they end up guarding a real server.
const minTokenLength = 8

// ValidateToken checks that a bearer token is present and long enough to
// plausibly be a real secret rather than a placeholder.
func ValidateToken(token string) error {
	if token == "" {
		return errors.New("token is required (--token, FREETUBE_SYNC_TOKEN, or config.json's token)")
	}
	if len(token) < minTokenLength {
		return fmt.Errorf("token is too short (%d chars, want at least %d); use a long random secret, e.g. `openssl rand -hex 32`", len(token), minTokenLength)
	}
	return nil
}

// ValidateListen checks that a listen address is present and in
// "[host]:port" form.
func ValidateListen(listen string) error {
	if listen == "" {
		return errors.New("listen address is required (--listen, FREETUBE_SYNC_LISTEN, or config.json's listen)")
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", listen, err)
	}
	return nil
}

// ValidateDataDir checks that a data directory was provided.
func ValidateDataDir(dir string) error {
	if dir == "" {
		return errors.New("data directory is required (--data, FREETUBE_SYNC_DATA, or config.json's dataDir)")
	}
	return nil
}
