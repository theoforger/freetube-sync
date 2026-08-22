// Package merge implements the pure LWW-element-set merge logic that
// underlies the sync protocol. It has no I/O and no notion of "now" except
// what's handed to it — every timestamp comes from the caller (in
// practice, the server's clock at arrival time; see CLAUDE.md invariant
// #5, clients never send timestamps).
package merge

import (
	"fmt"
	"sort"
)

// ChannelState is the canonical per-channel state: the most recent
// "subscribed" and "unsubscribed" timestamps ever observed for a channel.
type ChannelState struct {
	LastAdded   int64 `json:"lastAdded"`
	LastRemoved int64 `json:"lastRemoved"`
}

// Subscribed reports whether s represents a currently-subscribed channel.
// Ties favor "not subscribed" — an add and a remove stamped with the same
// arrival time cancel out to unsubscribed, which is also what keeps Merge
// order-independent for same-timestamp conflicting events (see
// ApplyEvents).
func (s ChannelState) Subscribed() bool {
	return s.LastAdded > s.LastRemoved
}

func mergeChannelState(a, b ChannelState) ChannelState {
	return ChannelState{
		LastAdded:   max64(a.LastAdded, b.LastAdded),
		LastRemoved: max64(a.LastRemoved, b.LastRemoved),
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// State is the full canonical subscription state: one ChannelState per
// channel ID.
type State map[string]ChannelState

// Merge combines two States into one, taking the per-field maximum of
// LastAdded/LastRemoved for every channel present in either input.
//
// This is commutative (Merge(a, b) == Merge(b, a)), associative
// (Merge(Merge(a, b), c) == Merge(a, Merge(b, c))), and idempotent
// (Merge(a, a) == a, and merging the same result in again is a no-op) —
// see merge_test.go. That's what makes it safe to apply regardless of
// sync order or retries: a client re-sending the same events, or two
// servers merging state in either order, always converge to the same
// result.
func Merge(a, b State) State {
	out := make(State, len(a)+len(b))
	for id, s := range a {
		out[id] = s
	}
	for id, s := range b {
		if existing, ok := out[id]; ok {
			out[id] = mergeChannelState(existing, s)
		} else {
			out[id] = s
		}
	}
	return out
}

// Action identifies a client-reported subscription change.
type Action string

const (
	Add    Action = "add"
	Remove Action = "remove"
)

// Event is a client-reported add/remove — deliberately timestamp-free.
// Clients diff their local profiles.db against a shadow snapshot and send
// plain events; only the server ever assigns timestamps, on arrival
// (invariant #5 — this is what prevents client clock skew from corrupting
// merge results).
type Event struct {
	ChannelID string
	Action    Action
}

// Stamp converts a client Event into a singleton State using an
// externally-supplied arrival timestamp `at`. Merging the result into the
// canonical state (via Merge) applies the event without ever trusting a
// client-supplied timestamp.
func Stamp(e Event, at int64) (State, error) {
	var s ChannelState
	switch e.Action {
	case Add:
		s = ChannelState{LastAdded: at}
	case Remove:
		s = ChannelState{LastRemoved: at}
	default:
		return nil, fmt.Errorf("invalid action %q for channel %q", e.Action, e.ChannelID)
	}
	return State{e.ChannelID: s}, nil
}

// ApplyEvents stamps and merges a batch of events into state, all using
// the same arrival timestamp `at`. A diff-based client emits at most one
// event per channel per sync, so this is normally unambiguous; if a batch
// ever did contain more than one event for the same channel, the result is
// still well-defined and order-independent, since Merge folds by taking a
// per-field maximum — an add and a remove stamped at the same `at` cancel
// out to "not subscribed" (see ChannelState.Subscribed) regardless of
// which was applied first.
func ApplyEvents(state State, events []Event, at int64) (State, error) {
	out := state
	for _, e := range events {
		s, err := Stamp(e, at)
		if err != nil {
			return nil, err
		}
		out = Merge(out, s)
	}
	return out, nil
}

// SubscribedChannels returns the sorted list of channel IDs currently
// subscribed in state.
func SubscribedChannels(state State) []string {
	ids := make([]string, 0, len(state))
	for id, s := range state {
		if s.Subscribed() {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
