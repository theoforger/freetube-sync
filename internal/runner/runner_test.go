package runner

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"freetube-sync/internal/clientsync"
	"freetube-sync/internal/guard"
	"freetube-sync/internal/server"
)

// newFakeFreeTube writes a short-lived shell script standing in for
// FreeTube: it touches a marker file (proving it actually ran) and exits
// with exitCode.
func newFakeFreeTube(t *testing.T, exitCode int) (launchCmd []string, markerPath string) {
	t.Helper()
	dir := t.TempDir()
	markerPath = filepath.Join(dir, "ran")
	scriptPath := filepath.Join(dir, "fake-freetube.sh")
	script := fmt.Sprintf("#!/bin/sh\ntouch %q\nexit %d\n", markerPath, exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return []string{scriptPath}, markerPath
}

func newTestServerURL(t *testing.T) (url, token string) {
	t.Helper()
	dir := t.TempDir()
	store := server.NewStore(filepath.Join(dir, server.StateFileName))
	token = "test-token"
	h := &server.Handler{Store: store, Token: token}
	srv := httptest.NewServer(h.Mux())
	t.Cleanup(srv.Close)
	return srv.URL, token
}

func newSyncDeps(t *testing.T, serverURL, token string) clientsync.Deps {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "profiles.db")
	line := `{"_id":"allChannels","name":"All Channels","subscriptions":[{"id":"UC1","name":"One"}]}` + "\n"
	if err := os.WriteFile(dbPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return clientsync.Deps{
		DBPath:     dbPath,
		ShadowPath: filepath.Join(dir, "last-synced.json"),
		ServerURL:  serverURL,
		Token:      token,
		GuardOpts:  guard.Options{ConfigDir: dir, ProcRoot: t.TempDir()},
	}
}

func TestRunSyncsBeforeAndAfterLaunch(t *testing.T) {
	url, token := newTestServerURL(t)
	launchCmd, marker := newFakeFreeTube(t, 0)
	syncDeps := newSyncDeps(t, url, token)

	outcome, err := Run(Deps{Sync: syncDeps, LaunchCommand: launchCmd})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("fake FreeTube didn't run: %v", err)
	}
	if outcome.PreSync.Skipped || outcome.PreSync.NetworkFailed {
		t.Errorf("PreSync = %+v, want a clean sync", outcome.PreSync)
	}
	if outcome.PostSync.Skipped || outcome.PostSync.NetworkFailed {
		t.Errorf("PostSync = %+v, want a clean sync", outcome.PostSync)
	}
	if outcome.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", outcome.ExitCode)
	}
}

func TestRunLaunchesEvenWhenServerIsDown(t *testing.T) {
	launchCmd, marker := newFakeFreeTube(t, 0)
	syncDeps := newSyncDeps(t, "http://127.0.0.1:1", "whatever") // nothing listens here

	outcome, err := Run(Deps{Sync: syncDeps, LaunchCommand: launchCmd})
	if err != nil {
		t.Fatalf("Run: want nil error (fail open), got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("fake FreeTube didn't run despite the server being down: %v", statErr)
	}
	if !outcome.PreSync.NetworkFailed {
		t.Error("PreSync.NetworkFailed = false, want true")
	}
	if !outcome.PostSync.NetworkFailed {
		t.Error("PostSync.NetworkFailed = false, want true")
	}
}

func TestRunLaunchesEvenWhenGuardSkips(t *testing.T) {
	url, token := newTestServerURL(t)
	launchCmd, marker := newFakeFreeTube(t, 0)
	syncDeps := newSyncDeps(t, url, token)
	if err := os.WriteFile(filepath.Join(syncDeps.GuardOpts.ConfigDir, "SingletonLock"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Run(Deps{Sync: syncDeps, LaunchCommand: launchCmd})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Errorf("fake FreeTube didn't run despite guard skip: %v", statErr)
	}
	if !outcome.PreSync.Skipped || !outcome.PostSync.Skipped {
		t.Errorf("PreSync/PostSync = %+v / %+v, want both Skipped", outcome.PreSync, outcome.PostSync)
	}
}

func TestRunPropagatesNonZeroExitCode(t *testing.T) {
	url, token := newTestServerURL(t)
	launchCmd, _ := newFakeFreeTube(t, 7)
	syncDeps := newSyncDeps(t, url, token)

	outcome, err := Run(Deps{Sync: syncDeps, LaunchCommand: launchCmd})
	if err != nil {
		t.Fatalf("Run: want nil error for a nonzero FreeTube exit, got %v", err)
	}
	if outcome.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", outcome.ExitCode)
	}
	// Both syncs should still have run despite the nonzero exit.
	if outcome.PostSync.Skipped || outcome.PostSync.NetworkFailed {
		t.Errorf("PostSync = %+v, want a clean sync even after nonzero exit", outcome.PostSync)
	}
}

func TestRunNoLaunchCommandErrors(t *testing.T) {
	url, token := newTestServerURL(t)
	_, err := Run(Deps{Sync: newSyncDeps(t, url, token)})
	if err == nil {
		t.Error("Run: want error when LaunchCommand is empty")
	}
}

func TestRunLaunchCommandNotFoundErrors(t *testing.T) {
	url, token := newTestServerURL(t)
	_, err := Run(Deps{
		Sync:          newSyncDeps(t, url, token),
		LaunchCommand: []string{filepath.Join(t.TempDir(), "does-not-exist")},
	})
	if err == nil {
		t.Error("Run: want error when the launch command can't be started")
	}
}
