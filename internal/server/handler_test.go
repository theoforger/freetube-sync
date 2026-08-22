package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "s3cr3t"

func newTestHandler(t *testing.T) (*Handler, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, StateFileName))
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	const baseMillis = 1_700_000_000_000
	var tick atomic.Int64
	h := &Handler{
		Store:  store,
		Token:  testToken,
		Logger: logger,
		Now: func() time.Time {
			return time.UnixMilli(baseMillis + tick.Add(1))
		},
	}
	return h, &logBuf
}

func postSync(t *testing.T, srv *httptest.Server, token string, deviceID string, events []eventRequest) (*http.Response, syncResponse) {
	t.Helper()
	body, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/sync", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if deviceID != "" {
		req.Header.Set("X-Device-Id", deviceID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var parsed syncResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp, parsed
}

func TestSyncRejectsMissingOrBadToken(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	resp, _ := postSync(t, srv, "", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	resp, _ = postSync(t, srv, "wrong-token", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestSyncAddThenList(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	resp, got := postSync(t, srv, testToken, "device-a", []eventRequest{
		{ChannelID: "UC1", Action: "add"},
		{ChannelID: "UC2", Action: "add"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(got.Subscribed) != 2 || got.Subscribed[0] != "UC1" || got.Subscribed[1] != "UC2" {
		t.Errorf("Subscribed = %v, want [UC1 UC2]", got.Subscribed)
	}
}

func TestSyncMergesAcrossDevices(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	// Device A adds UC1 and UC2.
	_, got := postSync(t, srv, testToken, "device-a", []eventRequest{
		{ChannelID: "UC1", Action: "add"},
		{ChannelID: "UC2", Action: "add"},
	})
	if len(got.Subscribed) != 2 {
		t.Fatalf("after device-a: Subscribed = %v", got.Subscribed)
	}

	// Device B, syncing independently, adds UC3 and removes UC2.
	_, got = postSync(t, srv, testToken, "device-b", []eventRequest{
		{ChannelID: "UC3", Action: "add"},
		{ChannelID: "UC2", Action: "remove"},
	})
	want := []string{"UC1", "UC3"}
	if len(got.Subscribed) != len(want) || got.Subscribed[0] != want[0] || got.Subscribed[1] != want[1] {
		t.Errorf("after device-b: Subscribed = %v, want %v", got.Subscribed, want)
	}
}

func TestSyncPersistsAcrossHandlerRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFileName)

	h1 := &Handler{Store: NewStore(path), Token: testToken}
	srv1 := httptest.NewServer(h1.Mux())
	_, got := postSync(t, srv1, testToken, "", []eventRequest{{ChannelID: "UC1", Action: "add"}})
	srv1.Close()
	if len(got.Subscribed) != 1 {
		t.Fatalf("Subscribed = %v", got.Subscribed)
	}

	// Simulate a restart: new Handler/Store pointed at the same file.
	h2 := &Handler{Store: NewStore(path), Token: testToken}
	srv2 := httptest.NewServer(h2.Mux())
	defer srv2.Close()
	_, got = postSync(t, srv2, testToken, "", nil) // no new events, just list current state
	if len(got.Subscribed) != 1 || got.Subscribed[0] != "UC1" {
		t.Errorf("after restart: Subscribed = %v, want [UC1]", got.Subscribed)
	}
}

func TestSyncRejectsMalformedBody(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/sync", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestSyncRejectsMissingChannelID(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	resp, _ := postSync(t, srv, testToken, "", []eventRequest{{ChannelID: "", Action: "add"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestSyncRejectsInvalidAction(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	resp, _ := postSync(t, srv, testToken, "", []eventRequest{{ChannelID: "UC1", Action: "bogus"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestSyncIgnoresClientSuppliedTimestamp(t *testing.T) {
	// A client (buggy or hostile) that includes extra fields, including
	// something timestamp-shaped, must not influence the result: only
	// ChannelID/Action are ever decoded.
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	body := `[{"channelId":"UC1","action":"add","lastAdded":1}]`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/sync", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got syncResponse
	json.NewDecoder(resp.Body).Decode(&got)
	if len(got.Subscribed) != 1 || got.Subscribed[0] != "UC1" {
		t.Errorf("Subscribed = %v, want [UC1]", got.Subscribed)
	}
}

func TestSyncLogsDeviceIDAndCounts(t *testing.T) {
	h, logBuf := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	postSync(t, srv, testToken, "my-laptop", []eventRequest{{ChannelID: "UC1", Action: "add"}})

	logged := logBuf.String()
	for _, want := range []string{"my-laptop", "events=1", "subscribed_count=1"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log output missing %q; got: %s", want, logged)
		}
	}
}

func TestSyncConcurrentRequestsDontCorruptState(t *testing.T) {
	h, _ := newTestHandler(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			postSync(t, srv, testToken, "", []eventRequest{
				{ChannelID: chanID(i), Action: "add"},
			})
		}(i)
	}
	wg.Wait()

	state, err := h.Store.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if len(state) != n {
		t.Errorf("len(state) = %d, want %d", len(state), n)
	}
}

func chanID(i int) string {
	return "UC" + string(rune('A'+i%26)) + string(rune('0'+i/26))
}
