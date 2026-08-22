package clientsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"freetube-sync/internal/merge"
)

type eventWire struct {
	ChannelID string `json:"channelId"`
	Action    string `json:"action"`
}

type syncResponse struct {
	Subscribed []string `json:"subscribed"`
}

// post sends events to deps.ServerURL's /sync endpoint and returns the
// authoritative subscribed channel ID list. Any error here — transport
// failure, non-200 response, malformed response body — is treated
// identically by the caller: a network/server failure that fails open.
func post(deps Deps, events []merge.Event) ([]string, error) {
	wire := make([]eventWire, len(events))
	for i, e := range events {
		wire[i] = eventWire{ChannelID: e.ChannelID, Action: string(e.Action)}
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode events: %w", err)
	}

	url := strings.TrimSuffix(deps.ServerURL, "/") + "/sync"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+deps.Token)
	if deps.DeviceID != "" {
		req.Header.Set("X-Device-Id", deps.DeviceID)
	}

	resp, err := deps.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("server returned %s: %s", resp.Status, bytes.TrimSpace(b))
	}

	var parsed syncResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return parsed.Subscribed, nil
}
