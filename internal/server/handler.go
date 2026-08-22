package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"freetube-sync/internal/merge"
)

// eventRequest is the wire format for one client-reported event. Only
// ChannelID and Action are ever decoded from the request body — if a
// client includes anything else (in particular a timestamp field), it's
// silently dropped by json.Decode. Only the server's own clock ever
// produces timestamps (CLAUDE.md invariant #5).
type eventRequest struct {
	ChannelID string `json:"channelId"`
	Action    string `json:"action"`
}

// syncResponse is the /sync response body: the full current authoritative
// set of subscribed channel IDs, no timestamps.
type syncResponse struct {
	Subscribed []string `json:"subscribed"`
}

// Handler serves the /sync endpoint behind bearer-token auth.
type Handler struct {
	Store *Store
	Token string

	// Logger defaults to slog.Default() when nil.
	Logger *slog.Logger
	// Now defaults to time.Now when nil; overridable for deterministic
	// tests.
	Now func() time.Time
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// Mux returns the routed handler, ready to pass to http.ListenAndServe.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sync", h.withAuth(h.handleSync))
	return mux
}

func (h *Handler) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		presented := strings.TrimPrefix(auth, prefix)
		if !strings.HasPrefix(auth, prefix) ||
			subtle.ConstantTimeCompare([]byte(presented), []byte(h.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-Device-Id")

	var reqs []eventRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	events := make([]merge.Event, 0, len(reqs))
	for _, e := range reqs {
		if e.ChannelID == "" {
			http.Error(w, "channelId is required", http.StatusBadRequest)
			return
		}
		action := merge.Action(e.Action)
		if action != merge.Add && action != merge.Remove {
			http.Error(w, fmt.Sprintf("invalid action %q for channel %q", e.Action, e.ChannelID), http.StatusBadRequest)
			return
		}
		events = append(events, merge.Event{ChannelID: e.ChannelID, Action: action})
	}

	at := h.now().UnixMilli()
	state, err := h.Store.Apply(events, at)
	if err != nil {
		h.logger().Error("sync failed", "device_id", deviceID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	subscribed := merge.SubscribedChannels(state)
	h.logger().Info("sync",
		"device_id", deviceID,
		"events", len(events),
		"subscribed_count", len(subscribed),
	)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(syncResponse{Subscribed: subscribed}); err != nil {
		h.logger().Error("encode response failed", "error", err)
	}
}
