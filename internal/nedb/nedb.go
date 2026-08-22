// Package nedb reads and writes FreeTube's profiles.db, a line-delimited
// JSON store (one document per line, NeDB's on-disk format).
//
// This is the highest-risk part of freetube-sync: profiles.db holds real
// user data (all profiles, settings embedded per-profile, etc.), most of
// which is unrelated to subscriptions. Per CLAUDE.md invariant #4, every
// line and field not related to the "All Channels" profile's subscriptions
// must round-trip byte-for-byte. To guarantee that, this package never
// re-marshals a document wholesale: it locates the exact byte span of the
// "subscriptions" value within a line and only ever replaces that span,
// leaving field order, spacing, and every other field untouched — even
// within the modified line itself.
//
// Callers that mutate the database MUST check internal/guard.IsSafeToWrite
// first (invariant #1); this package has no opinion on whether FreeTube is
// running and does not import internal/guard, to keep it a pure,
// independently-testable I/O layer.
package nedb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// allChannelsProfileID is the one profile this package ever mutates,
// identified by its stable "_id" — not its "name". FreeTube stores the
// default profile's "name" as an untranslated i18n key
// ("Profile.All Channels"), rendered into the display string by whatever
// locale is active; on a non-English install (or any FreeTube version that
// hasn't resolved it yet) it is never the literal string "All Channels".
// "_id" is always "allChannels" regardless of locale, so it's the only
// value that can be matched reliably. Sub-profiles, watch history, and
// playlists are out of scope (see CLAUDE.md "Assumptions & non-goals").
const allChannelsProfileID = "allChannels"

// BackupSuffix is appended to the database path to name the pre-overwrite
// backup file (invariant #3).
const BackupSuffix = ".freetube-sync-bak"

// Subscription mirrors FreeTube's per-profile subscription entry.
type Subscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// Doc is one parsed line (one NeDB document). Raw always holds the exact
// original bytes for anything not explicitly replaced via WithField.
type Doc struct {
	Raw []byte
}

// stringField decodes the string value of key, if present. Returns ok=false
// if the key is absent or its value isn't a string.
func (d Doc) stringField(key string) (value string, ok bool, err error) {
	raw, found, err := d.rawField(key)
	if err != nil || !found {
		return "", false, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		// Not a string value (e.g. a number/object) — not an error for our
		// purposes, just "not a matching string field".
		return "", false, nil
	}
	return value, true, nil
}

// rawField scans the top-level object in d.Raw and returns the exact raw
// bytes of key's value along with its byte offsets within d.Raw, without
// re-encoding anything else. Returns found=false if d.Raw isn't a JSON
// object or doesn't contain key at the top level.
func (d Doc) rawField(key string) (raw json.RawMessage, found bool, err error) {
	_, _, raw, found, err = d.rawFieldSpan(key)
	return raw, found, err
}

// rawFieldSpan is like rawField but also returns the [start, end) byte
// offsets of the value within d.Raw, so callers can splice in a
// replacement without touching anything else in the line.
func (d Doc) rawFieldSpan(key string) (start, end int, raw json.RawMessage, found bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(d.Raw))
	tok, err := dec.Token()
	if err != nil {
		return 0, 0, nil, false, fmt.Errorf("decode leading token: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return 0, 0, nil, false, fmt.Errorf("line is not a JSON object (starts with %v)", tok)
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return 0, 0, nil, false, fmt.Errorf("decode key token: %w", err)
		}
		keyStr, ok := keyTok.(string)
		if !ok {
			return 0, 0, nil, false, fmt.Errorf("unexpected non-string key token %v", keyTok)
		}
		if keyStr != key {
			// Skip the value without caring what shape it is.
			var discard json.RawMessage
			if err := dec.Decode(&discard); err != nil {
				return 0, 0, nil, false, fmt.Errorf("skip value for key %q: %w", keyStr, err)
			}
			continue
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return 0, 0, nil, false, fmt.Errorf("decode value for key %q: %w", keyStr, err)
		}
		valueEnd := int(dec.InputOffset())
		valueStart := valueEnd - len(value)
		return valueStart, valueEnd, value, true, nil
	}
	return 0, 0, nil, false, nil
}

// Name returns the document's "name" field, if present.
func (d Doc) Name() (string, error) {
	name, _, err := d.stringField("name")
	return name, err
}

// ID returns the document's "_id" field, if present.
func (d Doc) ID() (string, error) {
	id, _, err := d.stringField("_id")
	return id, err
}

// IsAllChannelsProfile reports whether this document is the "All Channels"
// profile (invariant #4: the only profile freetube-sync ever mutates), by
// its stable "_id" rather than its locale-dependent "name" — see
// allChannelsProfileID.
func (d Doc) IsAllChannelsProfile() bool {
	id, err := d.ID()
	return err == nil && id == allChannelsProfileID
}

// Subscriptions decodes the document's "subscriptions" field. A document
// with no such field returns a nil slice, not an error.
func (d Doc) Subscriptions() ([]Subscription, error) {
	raw, found, err := d.rawField("subscriptions")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	var subs []Subscription
	if err := json.Unmarshal(raw, &subs); err != nil {
		return nil, fmt.Errorf("decode subscriptions: %w", err)
	}
	return subs, nil
}

// WithSubscriptions returns a copy of d with the "subscriptions" field's
// raw bytes replaced by the JSON encoding of subs. Every other byte of the
// line — field order, spacing, all other values — is preserved exactly.
func (d Doc) WithSubscriptions(subs []Subscription) (Doc, error) {
	start, end, _, found, err := d.rawFieldSpan("subscriptions")
	if err != nil {
		return Doc{}, err
	}
	if !found {
		return Doc{}, fmt.Errorf("document has no \"subscriptions\" field")
	}
	if subs == nil {
		subs = []Subscription{}
	}
	encoded, err := json.Marshal(subs)
	if err != nil {
		return Doc{}, fmt.Errorf("encode subscriptions: %w", err)
	}
	newRaw := make([]byte, 0, len(d.Raw)-(end-start)+len(encoded))
	newRaw = append(newRaw, d.Raw[:start]...)
	newRaw = append(newRaw, encoded...)
	newRaw = append(newRaw, d.Raw[end:]...)
	return Doc{Raw: newRaw}, nil
}

// DB is a parsed profiles.db: one Doc per non-empty line, in file order.
type DB struct {
	Docs []Doc
	// TrailingNewline records whether the source file ended with a
	// newline, so Bytes() reproduces it exactly.
	TrailingNewline bool
}

// Parse splits data into lines and wraps each as a Doc. Blank lines are
// preserved verbatim (NeDB compaction can leave them); they're simply
// never matched as the "All Channels" profile.
func Parse(data []byte) DB {
	trailing := len(data) == 0 || data[len(data)-1] == '\n'
	trimmed := data
	if trailing && len(trimmed) > 0 {
		trimmed = trimmed[:len(trimmed)-1]
	}
	var lines [][]byte
	if len(trimmed) > 0 {
		lines = bytes.Split(trimmed, []byte("\n"))
	}
	docs := make([]Doc, len(lines))
	for i, line := range lines {
		docs[i] = Doc{Raw: line}
	}
	return DB{Docs: docs, TrailingNewline: trailing}
}

// Bytes reassembles the database back into its on-disk line-delimited
// form.
func (db DB) Bytes() []byte {
	var buf bytes.Buffer
	for i, doc := range db.Docs {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.Write(doc.Raw)
	}
	if db.TrailingNewline && len(db.Docs) > 0 {
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// allChannelsIndex locates the single "All Channels" profile document.
func (db DB) allChannelsIndex() (int, error) {
	found := -1
	for i, doc := range db.Docs {
		if doc.IsAllChannelsProfile() {
			if found != -1 {
				return -1, fmt.Errorf("multiple %q (_id %q) profiles found in database", "All Channels", allChannelsProfileID)
			}
			found = i
		}
	}
	if found == -1 {
		return -1, fmt.Errorf("no %q (_id %q) profile found in database", "All Channels", allChannelsProfileID)
	}
	return found, nil
}

// GetSubscriptions returns the "All Channels" profile's current
// subscriptions.
func (db DB) GetSubscriptions() ([]Subscription, error) {
	idx, err := db.allChannelsIndex()
	if err != nil {
		return nil, err
	}
	return db.Docs[idx].Subscriptions()
}

// SetSubscriptions replaces the "All Channels" profile's subscriptions in
// place (within db.Docs), leaving every other document and every other
// field of that document untouched.
func (db *DB) SetSubscriptions(subs []Subscription) error {
	idx, err := db.allChannelsIndex()
	if err != nil {
		return err
	}
	newDoc, err := db.Docs[idx].WithSubscriptions(subs)
	if err != nil {
		return err
	}
	db.Docs[idx] = newDoc
	return nil
}

// ReadFile reads and parses profiles.db from path.
func ReadFile(path string) (DB, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DB{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data), nil
}

// WriteFile writes db to path, following invariants #2–#3: back up any
// existing file, then write via temp-file + fsync + atomic rename. Callers
// must have already confirmed it's safe to write (invariant #1).
func (db DB) WriteFile(path string) error {
	dir := filepath.Dir(path)

	if _, err := os.Stat(path); err == nil {
		if err := backupFile(path, path+BackupSuffix); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup if we bail before the rename.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(db.Bytes()); err != nil {
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
	syncDir(dir)
	return nil
}

// backupFile copies src to dst, fsync-ing the destination before returning
// so the backup itself can't be left half-written.
func backupFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := out.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	syncDir(filepath.Dir(dst))
	return nil
}

// syncDir fsyncs a directory so a preceding rename is durable. Best-effort:
// some filesystems don't support it, so errors are ignored.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
