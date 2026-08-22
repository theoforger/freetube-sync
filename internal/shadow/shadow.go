// Package shadow manages the client's local shadow snapshot — the record
// of what was subscribed as of the last successful sync, used to diff
// against current profiles.db state (CLAUDE.md invariant #5: clients
// never send timestamps, only plain add/remove events computed from this
// diff).
package shadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"freetube-sync/internal/config"
	"freetube-sync/internal/nedb"
)

// FileName is the shadow snapshot's file name within the freetube-sync
// config dir.
const FileName = "last-synced.json"

// Snapshot is the last successfully-synced subscription state. It also
// doubles as this device's channel-metadata cache: when the server hands
// back a channel ID added by another device, this device has no
// name/thumbnail for it until the metadata shows up here (see
// internal/clientsync for how it's populated).
type Snapshot struct {
	Subscriptions []nedb.Subscription `json:"subscriptions"`
}

// Path returns the shadow snapshot's path, honoring XDG_CONFIG_HOME (via
// internal/config, so it lives alongside config.json).
func Path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Load reads the shadow snapshot at path. A missing file is not an error
// — it's the first-sync case — and returns a zero-value Snapshot.
func Load(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("read shadow snapshot: %w", err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("parse shadow snapshot %s: %w", path, err)
	}
	return s, nil
}

// Save writes the shadow snapshot to path, creating its directory if
// needed, via temp file + fsync + rename.
func Save(path string, s Snapshot) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create shadow snapshot dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode shadow snapshot: %w", err)
	}

	tmp, err := os.CreateTemp(dir, FileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	return nil
}
