// Package server implements the freetube-sync server role: the /sync HTTP
// endpoint and its canonical, single-tenant state file.
package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"freetube-sync/internal/merge"
)

// StateFileName is the canonical state file's name within --data.
const StateFileName = "state.json"

// Store manages the server's canonical subscription state file. Every
// read-modify-write cycle is mutex-guarded (so concurrent requests can't
// race on the file) and every write is atomic — temp file, fsync, rename —
// mirroring internal/nedb's write pattern (CLAUDE.md invariant #2).
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore returns a Store backed by the state file at path. The parent
// directory must already exist.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// load reads and parses the state file. A missing file is not an error —
// it's the first-run case, and returns an empty State.
func (st *Store) load() (merge.State, error) {
	data, err := os.ReadFile(st.path)
	if err != nil {
		if os.IsNotExist(err) {
			return merge.State{}, nil
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}
	var state merge.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state file %s: %w", st.path, err)
	}
	if state == nil {
		state = merge.State{}
	}
	return state, nil
}

// write atomically overwrites the state file with state's contents.
func (st *Store) write(state merge.State) error {
	dir := filepath.Dir(st.path)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(st.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // best-effort cleanup if we bail before rename

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
	if err := os.Rename(tmpPath, st.path); err != nil {
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	return nil
}

// Apply merges events into the canonical state — stamping all of them
// with the arrival timestamp `at` (the server's own clock; a client
// timestamp is never trusted, per invariant #5) — persists the result
// atomically, and returns the resulting state. The whole
// read-modify-write cycle holds the store's mutex.
func (st *Store) Apply(events []merge.Event, at int64) (merge.State, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	current, err := st.load()
	if err != nil {
		return nil, err
	}
	next, err := merge.ApplyEvents(current, events, at)
	if err != nil {
		return nil, err
	}
	if err := st.write(next); err != nil {
		return nil, err
	}
	return next, nil
}

// State returns the current canonical state without modifying it.
func (st *Store) State() (merge.State, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.load()
}
