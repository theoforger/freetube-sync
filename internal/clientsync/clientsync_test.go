package clientsync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"freetube-sync/internal/guard"
	"freetube-sync/internal/nedb"
	"freetube-sync/internal/server"
	"freetube-sync/internal/shadow"
)

// newTestServer spins up a real internal/server.Handler over httptest, so
// these tests exercise the actual wire protocol end to end (per TASKS.md's
// Stage 5 deliverable: two local "devices" synced against one running
// serve instance).
func newTestServer(t *testing.T) (url, token string) {
	t.Helper()
	dir := t.TempDir()
	store := server.NewStore(filepath.Join(dir, server.StateFileName))
	token = "test-token"
	h := &server.Handler{Store: store, Token: token}
	srv := httptest.NewServer(h.Mux())
	t.Cleanup(srv.Close)
	return srv.URL, token
}

// device bundles one simulated client's on-disk state: its own
// profiles.db and shadow snapshot, both under a private temp dir so two
// devices never share a guard/config directory.
type device struct {
	t          *testing.T
	dir        string
	dbPath     string
	shadowPath string
}

func newDevice(t *testing.T, subs []nedb.Subscription) *device {
	t.Helper()
	dir := t.TempDir()
	d := &device{
		t:          t,
		dir:        dir,
		dbPath:     filepath.Join(dir, "profiles.db"),
		shadowPath: filepath.Join(dir, "last-synced.json"),
	}
	d.writeDB(subs)
	return d
}

func (d *device) writeDB(subs []nedb.Subscription) {
	d.t.Helper()
	subsJSON, err := json.Marshal(subs)
	if err != nil {
		d.t.Fatal(err)
	}
	line := fmt.Sprintf(`{"_id":"allChannels","name":"All Channels","bgColor":"#000","textColor":"#fff","subscriptions":%s}`, subsJSON)
	if err := os.WriteFile(d.dbPath, []byte(line+"\n"), 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) subscriptions() []nedb.Subscription {
	d.t.Helper()
	db, err := nedb.ReadFile(d.dbPath)
	if err != nil {
		d.t.Fatalf("ReadFile: %v", err)
	}
	subs, err := db.GetSubscriptions()
	if err != nil {
		d.t.Fatalf("GetSubscriptions: %v", err)
	}
	return subs
}

func (d *device) run(serverURL, token string) Outcome {
	d.t.Helper()
	outcome, err := Run(Deps{
		DBPath:     d.dbPath,
		ShadowPath: d.shadowPath,
		ServerURL:  serverURL,
		Token:      token,
		GuardOpts:  guard.Options{ConfigDir: d.dir, ProcRoot: d.t.TempDir()},
	})
	if err != nil {
		d.t.Fatalf("Run: %v", err)
	}
	return outcome
}

func ids(subs []nedb.Subscription) []string {
	out := make([]string, len(subs))
	for i, s := range subs {
		out[i] = s.ID
	}
	return out
}

func containsAll(t *testing.T, got, want []string) {
	t.Helper()
	set := make(map[string]bool, len(got))
	for _, id := range got {
		set[id] = true
	}
	for _, id := range want {
		if !set[id] {
			t.Errorf("got %v, missing %q", got, id)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %v (len %d), want len %d", got, len(got), len(want))
	}
}

func TestRunFirstSyncNoShadow(t *testing.T) {
	url, token := newTestServer(t)
	dev := newDevice(t, []nedb.Subscription{
		{ID: "UC1", Name: "One"},
		{ID: "UC2", Name: "Two"},
	})

	outcome := dev.run(url, token)
	if outcome.Skipped || outcome.NetworkFailed || outcome.DryRun {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	containsAll(t, outcome.Added, []string{"UC1", "UC2"})
	if len(outcome.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", outcome.Removed)
	}
	containsAll(t, outcome.Subscribed, []string{"UC1", "UC2"})

	// profiles.db round-trips its own metadata.
	subs := dev.subscriptions()
	containsAll(t, ids(subs), []string{"UC1", "UC2"})

	// Shadow snapshot was created.
	snap, err := shadow.Load(dev.shadowPath)
	if err != nil {
		t.Fatalf("shadow.Load: %v", err)
	}
	containsAll(t, ids(snap.Subscriptions), []string{"UC1", "UC2"})
}

func TestRunNoOpSync(t *testing.T) {
	url, token := newTestServer(t)
	dev := newDevice(t, []nedb.Subscription{{ID: "UC1", Name: "One"}})
	dev.run(url, token) // establish baseline

	outcome := dev.run(url, token)
	if len(outcome.Added) != 0 || len(outcome.Removed) != 0 {
		t.Errorf("no-op sync: Added=%v Removed=%v, want both empty", outcome.Added, outcome.Removed)
	}
	containsAll(t, outcome.Subscribed, []string{"UC1"})
}

func TestRunLocalOnlyChange(t *testing.T) {
	url, token := newTestServer(t)
	dev := newDevice(t, []nedb.Subscription{{ID: "UC1", Name: "One"}})
	dev.run(url, token)

	// Simulate the user subscribing to a new channel in FreeTube directly.
	dev.writeDB([]nedb.Subscription{{ID: "UC1", Name: "One"}, {ID: "UC2", Name: "Two"}})

	outcome := dev.run(url, token)
	containsAll(t, outcome.Added, []string{"UC2"})
	if len(outcome.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", outcome.Removed)
	}
	containsAll(t, outcome.Subscribed, []string{"UC1", "UC2"})
}

func TestRunRemoteOnlyChange(t *testing.T) {
	url, token := newTestServer(t)

	devA := newDevice(t, []nedb.Subscription{{ID: "UC1", Name: "One"}})
	devA.run(url, token)

	devB := newDevice(t, []nedb.Subscription{{ID: "UC1", Name: "One"}})
	devB.run(url, token) // baseline, matches server already

	// Device A subscribes to a new channel and syncs it up.
	devA.writeDB([]nedb.Subscription{{ID: "UC1", Name: "One"}, {ID: "UC2", Name: "Two"}})
	devA.run(url, token)

	// Device B, with no local changes, should pick up UC2 from the server.
	outcome := devB.run(url, token)
	if len(outcome.Added) != 0 || len(outcome.Removed) != 0 {
		t.Errorf("device B local diff should be empty: Added=%v Removed=%v", outcome.Added, outcome.Removed)
	}
	containsAll(t, outcome.Subscribed, []string{"UC1", "UC2"})

	// UC2 wasn't known locally on device B, so it gets a bare entry.
	subs := devB.subscriptions()
	found := false
	for _, s := range subs {
		if s.ID == "UC2" {
			found = true
			if s.Name != "" {
				t.Errorf("UC2 metadata unexpectedly known on device B: %+v", s)
			}
		}
	}
	if !found {
		t.Errorf("device B profiles.db missing UC2: %v", subs)
	}
}

func TestRunBothChanged(t *testing.T) {
	url, token := newTestServer(t)

	devA := newDevice(t, []nedb.Subscription{{ID: "UC1"}})
	devA.run(url, token)
	devB := newDevice(t, []nedb.Subscription{{ID: "UC1"}})
	devB.run(url, token)

	// Device A adds UC2 and syncs.
	devA.writeDB([]nedb.Subscription{{ID: "UC1"}, {ID: "UC2", Name: "Two"}})
	devA.run(url, token)

	// Device B independently adds UC3 (having not yet seen UC2) and syncs.
	devB.writeDB([]nedb.Subscription{{ID: "UC1"}, {ID: "UC3", Name: "Three"}})
	outcome := devB.run(url, token)

	containsAll(t, outcome.Added, []string{"UC3"})
	containsAll(t, outcome.Subscribed, []string{"UC1", "UC2", "UC3"})
}

func TestRunRemovalPropagates(t *testing.T) {
	url, token := newTestServer(t)
	devA := newDevice(t, []nedb.Subscription{{ID: "UC1"}, {ID: "UC2"}})
	devA.run(url, token)
	devB := newDevice(t, []nedb.Subscription{{ID: "UC1"}, {ID: "UC2"}})
	devB.run(url, token)

	devA.writeDB([]nedb.Subscription{{ID: "UC1"}}) // unsubscribed from UC2
	outcome := devA.run(url, token)
	containsAll(t, outcome.Removed, []string{"UC2"})

	outcomeB := devB.run(url, token)
	containsAll(t, outcomeB.Subscribed, []string{"UC1"})
}

func TestRunServerUnreachableFailsOpen(t *testing.T) {
	dev := newDevice(t, []nedb.Subscription{{ID: "UC1"}})
	before, err := os.ReadFile(dev.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := Run(Deps{
		DBPath:     dev.dbPath,
		ShadowPath: dev.shadowPath,
		ServerURL:  "http://127.0.0.1:1", // nothing listens here
		Token:      "whatever",
		GuardOpts:  guard.Options{ConfigDir: dev.dir, ProcRoot: t.TempDir()},
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatalf("Run: want nil error (fail open), got %v", err)
	}
	if !outcome.NetworkFailed {
		t.Errorf("outcome.NetworkFailed = false, want true")
	}
	if outcome.NetworkError == "" {
		t.Error("outcome.NetworkError is empty")
	}

	after, err := os.ReadFile(dev.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("profiles.db was modified despite a network failure")
	}
	if _, err := os.Stat(dev.shadowPath); !os.IsNotExist(err) {
		t.Error("shadow snapshot was created despite a network failure")
	}
}

func TestRunGuardSkipLeavesStateUntouched(t *testing.T) {
	url, token := newTestServer(t)
	dev := newDevice(t, []nedb.Subscription{{ID: "UC1"}})
	before, err := os.ReadFile(dev.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate FreeTube running: drop a SingletonLock in the guard's
	// config dir.
	if err := os.WriteFile(filepath.Join(dev.dir, "SingletonLock"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Run(Deps{
		DBPath:     dev.dbPath,
		ShadowPath: dev.shadowPath,
		ServerURL:  url,
		Token:      token,
		GuardOpts:  guard.Options{ConfigDir: dev.dir, ProcRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Skipped {
		t.Errorf("outcome.Skipped = false, want true")
	}
	if outcome.SkipReason == "" {
		t.Error("outcome.SkipReason is empty")
	}

	after, err := os.ReadFile(dev.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("profiles.db was modified despite guard skip")
	}
	if _, err := os.Stat(dev.shadowPath); !os.IsNotExist(err) {
		t.Error("shadow snapshot was created despite guard skip")
	}
}

func TestRunDryRunLeavesStateUntouched(t *testing.T) {
	url, token := newTestServer(t)
	dev := newDevice(t, []nedb.Subscription{{ID: "UC1"}})
	before, err := os.ReadFile(dev.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := Run(Deps{
		DBPath:     dev.dbPath,
		ShadowPath: dev.shadowPath,
		ServerURL:  url,
		Token:      token,
		DryRun:     true,
		GuardOpts:  guard.Options{ConfigDir: dev.dir, ProcRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.DryRun {
		t.Error("outcome.DryRun = false, want true")
	}
	containsAll(t, outcome.Added, []string{"UC1"})

	after, err := os.ReadFile(dev.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("profiles.db was modified during a dry run")
	}
	if _, err := os.Stat(dev.shadowPath); !os.IsNotExist(err) {
		t.Error("shadow snapshot was created during a dry run")
	}
}

func TestRunRejectsBadToken(t *testing.T) {
	url, _ := newTestServer(t)
	dev := newDevice(t, []nedb.Subscription{{ID: "UC1"}})
	outcome, err := Run(Deps{
		DBPath:     dev.dbPath,
		ShadowPath: dev.shadowPath,
		ServerURL:  url,
		Token:      "wrong-token",
		GuardOpts:  guard.Options{ConfigDir: dev.dir, ProcRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("Run: want nil error (fail open), got %v", err)
	}
	if !outcome.NetworkFailed {
		t.Error("outcome.NetworkFailed = false, want true for a rejected token")
	}
}
