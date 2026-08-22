package nedb

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "profiles.db"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestParseThenBytesRoundTripsUnmodified(t *testing.T) {
	original := readFixture(t)
	db := Parse(original)
	got := db.Bytes()
	if !bytes.Equal(got, original) {
		t.Errorf("round trip mismatch:\n got:  %q\n want: %q", got, original)
	}
}

func TestGetSubscriptionsFromFixture(t *testing.T) {
	db := Parse(readFixture(t))
	subs, err := db.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("len(subs) = %d, want 3", len(subs))
	}
	want := Subscription{ID: "UCXuqSBlHAE6Xw-yeJA0Tunw", Name: "Linus Tech Tips", Thumbnail: "https://yt3.googleusercontent.com/lkH37D712tiyphnu0Id0D5MwwQ7IRuwgQLVD05iMXlDWO-kDHut3CQwbwSFj3nyzLIDaqB0S=s176-c-k-c0x00ffffff-no-rj"}
	if subs[0] != want {
		t.Errorf("subs[0] = %+v, want %+v", subs[0], want)
	}
}

func TestSetSubscriptionsOnlyTouchesAllChannelsSubscriptionsField(t *testing.T) {
	original := readFixture(t)
	db := Parse(original)

	newSubs := []Subscription{
		{ID: "UCnew00000000000000001", Name: "New Channel One", Thumbnail: "https://example.com/1.jpg"},
		{ID: "UCnew00000000000000002", Name: "New Channel Two"},
	}
	if err := db.SetSubscriptions(newSubs); err != nil {
		t.Fatalf("SetSubscriptions: %v", err)
	}

	// Re-read to confirm the new value stuck.
	got, err := db.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions after set: %v", err)
	}
	if len(got) != 2 || got[0] != newSubs[0] || got[1] != newSubs[1] {
		t.Errorf("GetSubscriptions() = %+v, want %+v", got, newSubs)
	}

	origDB := Parse(original)
	origLines := bytes.Split(bytes.TrimSuffix(original, []byte("\n")), []byte("\n"))
	newLines := bytes.Split(bytes.TrimSuffix(db.Bytes(), []byte("\n")), []byte("\n"))
	if len(origLines) != len(newLines) {
		t.Fatalf("line count changed: %d -> %d", len(origLines), len(newLines))
	}

	for i := range origLines {
		if origDB.Docs[i].IsAllChannelsProfile() {
			if bytes.Equal(origLines[i], newLines[i]) {
				t.Errorf("line %d (All Channels): expected subscriptions to change, but line is identical", i)
			}
			// Every field except "subscriptions" must be byte-identical.
			assertOnlySubscriptionsFieldChanged(t, origLines[i], newLines[i])
			continue
		}
		if !bytes.Equal(origLines[i], newLines[i]) {
			t.Errorf("line %d changed but shouldn't have:\n got:  %q\n want: %q", i, newLines[i], origLines[i])
		}
	}
}

// assertOnlySubscriptionsFieldChanged decodes both lines as generic JSON
// objects and asserts every key except "subscriptions" is byte-identical
// in its raw form, and that key order is unchanged.
func assertOnlySubscriptionsFieldChanged(t *testing.T, oldLine, newLine []byte) {
	t.Helper()
	oldKeys := rawKeysInOrder(t, oldLine)
	newKeys := rawKeysInOrder(t, newLine)
	if len(oldKeys) != len(newKeys) {
		t.Fatalf("field count changed: %d -> %d", len(oldKeys), len(newKeys))
	}
	for i, k := range oldKeys {
		if newKeys[i] != k {
			t.Errorf("field order changed at position %d: %q -> %q", i, k, newKeys[i])
		}
	}
	oldRaw := rawFieldsOf(t, oldLine)
	newRaw := rawFieldsOf(t, newLine)
	for key, oldVal := range oldRaw {
		if key == "subscriptions" {
			continue
		}
		newVal, ok := newRaw[key]
		if !ok {
			t.Errorf("field %q missing after SetSubscriptions", key)
			continue
		}
		if !bytes.Equal(oldVal, newVal) {
			t.Errorf("field %q changed: %q -> %q", key, oldVal, newVal)
		}
	}
}

func rawKeysInOrder(t *testing.T, line []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(line))
	if _, err := dec.Token(); err != nil {
		t.Fatalf("decode leading token: %v", err)
	}
	var keys []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("decode key: %v", err)
		}
		keys = append(keys, keyTok.(string))
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			t.Fatalf("skip value: %v", err)
		}
	}
	return keys
}

func rawFieldsOf(t *testing.T, line []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	return m
}

func TestSetSubscriptionsOnMissingProfileErrors(t *testing.T) {
	db := Parse([]byte(`{"_id":"x","name":"Not All Channels","subscriptions":[]}` + "\n"))
	if err := db.SetSubscriptions(nil); err == nil {
		t.Error("SetSubscriptions: want error when no All Channels profile exists")
	}
}

func TestSetSubscriptionsWithNilWritesEmptyArray(t *testing.T) {
	db := Parse([]byte(`{"name":"All Channels","subscriptions":[{"id":"UCx","name":"X"}]}` + "\n"))
	if err := db.SetSubscriptions(nil); err != nil {
		t.Fatalf("SetSubscriptions: %v", err)
	}
	subs, err := db.GetSubscriptions()
	if err != nil {
		t.Fatalf("GetSubscriptions: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("len(subs) = %d, want 0", len(subs))
	}
	if !bytes.Contains(db.Bytes(), []byte(`"subscriptions":[]`)) {
		t.Errorf("expected empty array literal, got %q", db.Bytes())
	}
}

func TestParsePreservesNoTrailingNewline(t *testing.T) {
	original := []byte(`{"name":"All Channels","subscriptions":[]}`)
	db := Parse(original)
	if db.TrailingNewline {
		t.Error("TrailingNewline = true, want false")
	}
	if !bytes.Equal(db.Bytes(), original) {
		t.Errorf("Bytes() = %q, want %q", db.Bytes(), original)
	}
}

func TestParseEmptyInput(t *testing.T) {
	db := Parse(nil)
	if len(db.Docs) != 0 {
		t.Errorf("len(Docs) = %d, want 0", len(db.Docs))
	}
	if !bytes.Equal(db.Bytes(), []byte{}) {
		t.Errorf("Bytes() = %q, want empty", db.Bytes())
	}
}

func TestWriteFileAtomicWithBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.db")
	original := readFixture(t)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	db := Parse(original)
	newSubs := []Subscription{{ID: "UCnew", Name: "Brand New"}}
	if err := db.SetSubscriptions(newSubs); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Backup holds the pre-write content, byte-for-byte.
	backup, err := os.ReadFile(path + BackupSuffix)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(backup, original) {
		t.Errorf("backup content mismatch")
	}

	// Main file holds the new content.
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if !bytes.Equal(final, db.Bytes()) {
		t.Errorf("final file mismatch")
	}

	// No stray temp files left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != "profiles.db" && name != "profiles.db"+BackupSuffix {
			t.Errorf("unexpected leftover file: %s", name)
		}
	}
}

func TestWriteFileNoBackupWhenNoExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.db")
	db := Parse([]byte(`{"name":"All Channels","subscriptions":[]}` + "\n"))
	if err := db.WriteFile(path); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := os.Stat(path + BackupSuffix); !os.IsNotExist(err) {
		t.Errorf("backup file should not exist when there was nothing to back up (err=%v)", err)
	}
}

func TestWriteFileFailureLeavesOriginalIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based failure injection doesn't apply")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.db")
	original := []byte(`{"name":"All Channels","subscriptions":[]}` + "\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	db := Parse(original)
	if err := db.WriteFile(path); err == nil {
		t.Fatal("WriteFile: want error when directory isn't writable")
	}

	os.Chmod(dir, 0o755)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("original file was modified despite write failure:\n got:  %q\n want: %q", got, original)
	}
}

func TestReadFileFixture(t *testing.T) {
	db, err := ReadFile(filepath.Join("..", "..", "testdata", "profiles.db"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(db.Docs) != 3 {
		t.Fatalf("len(Docs) = %d, want 3", len(db.Docs))
	}
}
