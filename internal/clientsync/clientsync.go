// Package clientsync implements the client-side sync cycle: guard check,
// diff current profiles.db state against the local shadow snapshot, POST
// the diff to the server, overwrite profiles.db and the shadow snapshot
// with the authoritative response.
//
// Per CLAUDE.md invariant #6, network/server failures fail open: they are
// reported via Outcome, not returned as an error, and leave local state
// completely untouched.
package clientsync

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"freetube-sync/internal/guard"
	"freetube-sync/internal/merge"
	"freetube-sync/internal/nedb"
	"freetube-sync/internal/shadow"
)

// Deps configures one sync run. Only DBPath, ShadowPath, ServerURL, and
// Token are required; everything else has a sane default and exists
// mainly for testability.
type Deps struct {
	DBPath     string
	ShadowPath string
	ServerURL  string
	Token      string
	DeviceID   string // optional, sent as X-Device-Id, logged only
	DryRun     bool

	HTTPClient *http.Client  // defaults to a client with a 10s timeout
	Logger     *slog.Logger  // defaults to slog.Default()
	GuardOpts  guard.Options // ConfigDir should be filepath.Dir(DBPath)
}

func (d Deps) httpClient() *http.Client {
	if d.HTTPClient != nil {
		return d.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (d Deps) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// Outcome describes what Run did, for callers (cmd_sync.go, cmd_run.go)
// to report to the user and logs.
type Outcome struct {
	// Skipped is true when the guard check found it unsafe to touch
	// profiles.db; nothing else in Outcome is populated.
	Skipped    bool
	SkipReason string

	// NetworkFailed is true when the server couldn't be reached or
	// returned an error; per invariant #6 this is not a Run error. Local
	// state (profiles.db, shadow snapshot) is left untouched.
	NetworkFailed bool
	NetworkError  string

	// DryRun is true when Deps.DryRun was set: Added/Removed reflect the
	// local diff that would have been sent, but nothing was written or
	// sent anywhere.
	DryRun bool

	Added      []string
	Removed    []string
	Subscribed []string // authoritative post-sync set; only set on success
}

// Run performs one guard-check -> diff -> POST -> overwrite cycle.
//
// Local I/O errors (a corrupt profiles.db, a shadow/config dir that can't
// be created) are returned as errors — those indicate something is
// actually wrong locally and should not be silently swallowed. Only
// network/server failures fail open (see Outcome.NetworkFailed).
func Run(deps Deps) (Outcome, error) {
	safe, err := guard.IsSafeToWrite(deps.GuardOpts)
	if err != nil {
		return Outcome{}, fmt.Errorf("guard check: %w", err)
	}
	if !safe {
		return Outcome{Skipped: true, SkipReason: guard.Reason(deps.GuardOpts)}, nil
	}

	db, err := nedb.ReadFile(deps.DBPath)
	if err != nil {
		return Outcome{}, err
	}
	currentSubs, err := db.GetSubscriptions()
	if err != nil {
		return Outcome{}, fmt.Errorf("read subscriptions: %w", err)
	}

	snap, err := shadow.Load(deps.ShadowPath)
	if err != nil {
		return Outcome{}, err
	}

	added, removed, metadata := diff(currentSubs, snap.Subscriptions)
	events := buildEvents(added, removed)

	deps.logger().Info("sync: local diff", "device_id", deps.DeviceID, "added", len(added), "removed", len(removed))

	if deps.DryRun {
		return Outcome{Added: added, Removed: removed, DryRun: true}, nil
	}

	subscribedIDs, err := post(deps, events)
	if err != nil {
		deps.logger().Warn("sync: server unreachable, leaving local state untouched", "error", err)
		return Outcome{NetworkFailed: true, NetworkError: err.Error()}, nil
	}

	newSubs := reconcile(subscribedIDs, metadata)

	if err := db.SetSubscriptions(newSubs); err != nil {
		return Outcome{}, err
	}
	if err := db.WriteFile(deps.DBPath); err != nil {
		return Outcome{}, err
	}
	if err := shadow.Save(deps.ShadowPath, shadow.Snapshot{Subscriptions: newSubs}); err != nil {
		return Outcome{}, err
	}

	return Outcome{Added: added, Removed: removed, Subscribed: subscribedIDs}, nil
}

// diff compares current profiles.db subscriptions against the previous
// shadow snapshot, returning sorted added/removed channel IDs and a
// channelID -> Subscription metadata cache seeded from both (current
// profiles.db metadata wins over stale shadow metadata for any ID present
// in both).
func diff(current, previous []nedb.Subscription) (added, removed []string, metadata map[string]nedb.Subscription) {
	metadata = make(map[string]nedb.Subscription, len(current)+len(previous))
	prevSet := make(map[string]bool, len(previous))
	for _, s := range previous {
		prevSet[s.ID] = true
		metadata[s.ID] = s
	}
	currSet := make(map[string]bool, len(current))
	for _, s := range current {
		currSet[s.ID] = true
		metadata[s.ID] = s // freshest — overwrites any shadow copy
	}

	for id := range currSet {
		if !prevSet[id] {
			added = append(added, id)
		}
	}
	for id := range prevSet {
		if !currSet[id] {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed, metadata
}

func buildEvents(added, removed []string) []merge.Event {
	events := make([]merge.Event, 0, len(added)+len(removed))
	for _, id := range added {
		events = append(events, merge.Event{ChannelID: id, Action: merge.Add})
	}
	for _, id := range removed {
		events = append(events, merge.Event{ChannelID: id, Action: merge.Remove})
	}
	return events
}

// reconcile builds the final subscriptions list to write to profiles.db
// from the server's authoritative channel ID list, filling in metadata
// (name/thumbnail) from the local cache. IDs the server returned that this
// device has never seen metadata for (added by another device) get a
// bare, name-less entry — FreeTube backfills that lazily on its own.
func reconcile(ids []string, metadata map[string]nedb.Subscription) []nedb.Subscription {
	subs := make([]nedb.Subscription, 0, len(ids))
	for _, id := range ids {
		if s, ok := metadata[id]; ok {
			subs = append(subs, s)
		} else {
			subs = append(subs, nedb.Subscription{ID: id})
		}
	}
	return subs
}
