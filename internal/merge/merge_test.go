package merge

import (
	"fmt"
	"reflect"
	"testing"
)

func TestChannelStateSubscribed(t *testing.T) {
	tests := []struct {
		name string
		s    ChannelState
		want bool
	}{
		{"added after removed", ChannelState{LastAdded: 20, LastRemoved: 10}, true},
		{"removed after added", ChannelState{LastAdded: 10, LastRemoved: 20}, false},
		{"tie favors not subscribed", ChannelState{LastAdded: 10, LastRemoved: 10}, false},
		{"zero value is not subscribed", ChannelState{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.Subscribed(); got != tt.want {
				t.Errorf("Subscribed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeTableDriven(t *testing.T) {
	tests := []struct {
		name string
		a, b State
		want State
	}{
		{
			name: "empty + empty",
			a:    State{}, b: State{},
			want: State{},
		},
		{
			name: "empty + nonempty (one-sided-empty)",
			a:    State{}, b: State{"UC1": {LastAdded: 5}},
			want: State{"UC1": {LastAdded: 5}},
		},
		{
			name: "nonempty + empty (one-sided-empty)",
			a:    State{"UC1": {LastAdded: 5}}, b: State{},
			want: State{"UC1": {LastAdded: 5}},
		},
		{
			name: "nil + nil",
			a:    nil, b: nil,
			want: State{},
		},
		{
			name: "add/add — later add wins, larger timestamp kept",
			a:    State{"UC1": {LastAdded: 100}},
			b:    State{"UC1": {LastAdded: 200}},
			want: State{"UC1": {LastAdded: 200}},
		},
		{
			name: "add-vs-remove race — add newer wins (subscribed)",
			a:    State{"UC1": {LastAdded: 200}},
			b:    State{"UC1": {LastRemoved: 100}},
			want: State{"UC1": {LastAdded: 200, LastRemoved: 100}},
		},
		{
			name: "add-vs-remove race — remove newer wins (unsubscribed)",
			a:    State{"UC1": {LastAdded: 100}},
			b:    State{"UC1": {LastRemoved: 200}},
			want: State{"UC1": {LastAdded: 100, LastRemoved: 200}},
		},
		{
			name: "remove/remove — max removed timestamp kept",
			a:    State{"UC1": {LastRemoved: 100}},
			b:    State{"UC1": {LastRemoved: 200}},
			want: State{"UC1": {LastRemoved: 200}},
		},
		{
			name: "stale/older timestamp doesn't regress state",
			a:    State{"UC1": {LastAdded: 500, LastRemoved: 100}},
			b:    State{"UC1": {LastAdded: 50}}, // an old, already-superseded add
			want: State{"UC1": {LastAdded: 500, LastRemoved: 100}},
		},
		{
			name: "disjoint channels both kept",
			a:    State{"UC1": {LastAdded: 10}},
			b:    State{"UC2": {LastAdded: 20}},
			want: State{"UC1": {LastAdded: 10}, "UC2": {LastAdded: 20}},
		},
		{
			name: "multiple channels, mixed overlap",
			a:    State{"UC1": {LastAdded: 10}, "UC2": {LastAdded: 5, LastRemoved: 50}},
			b:    State{"UC2": {LastAdded: 5, LastRemoved: 30}, "UC3": {LastRemoved: 1}},
			want: State{
				"UC1": {LastAdded: 10},
				"UC2": {LastAdded: 5, LastRemoved: 50},
				"UC3": {LastRemoved: 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Merge(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Merge(a, b) = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMergeCommutative(t *testing.T) {
	states := sampleStates()
	for i, a := range states {
		for j, b := range states {
			ab := Merge(a, b)
			ba := Merge(b, a)
			if !reflect.DeepEqual(ab, ba) {
				t.Errorf("Merge not commutative for pair (%d,%d): Merge(a,b)=%+v, Merge(b,a)=%+v", i, j, ab, ba)
			}
		}
	}
}

func TestMergeAssociative(t *testing.T) {
	states := sampleStates()
	for i, a := range states {
		for j, b := range states {
			for k, c := range states {
				left := Merge(Merge(a, b), c)
				right := Merge(a, Merge(b, c))
				if !reflect.DeepEqual(left, right) {
					t.Errorf("Merge not associative for triple (%d,%d,%d): (a∪b)∪c=%+v, a∪(b∪c)=%+v", i, j, k, left, right)
				}
			}
		}
	}
}

func TestMergeIdempotent(t *testing.T) {
	for i, a := range sampleStates() {
		if got := Merge(a, a); !reflect.DeepEqual(got, a) {
			t.Errorf("Merge(a, a) != a for state %d: got %+v, want %+v", i, got, a)
		}
		once := Merge(a, State{"UCextra": {LastAdded: 42}})
		twice := Merge(once, State{"UCextra": {LastAdded: 42}})
		if !reflect.DeepEqual(once, twice) {
			t.Errorf("merging the same update twice changed the result: once=%+v, twice=%+v", once, twice)
		}
	}
}

func sampleStates() []State {
	return []State{
		{},
		{"UC1": {LastAdded: 10}},
		{"UC1": {LastRemoved: 10}},
		{"UC1": {LastAdded: 100, LastRemoved: 50}},
		{"UC1": {LastAdded: 10}, "UC2": {LastRemoved: 5}},
		{"UC2": {LastAdded: 999}, "UC3": {LastAdded: 1, LastRemoved: 1}},
		{"UC1": {LastAdded: 10, LastRemoved: 10}, "UC4": {LastAdded: 7}},
	}
}

func TestMergeLargeSets(t *testing.T) {
	const n = 20000
	a := make(State, n)
	b := make(State, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("UC%d", i)
		a[id] = ChannelState{LastAdded: int64(i)}
		b[id] = ChannelState{LastAdded: int64(i), LastRemoved: int64(i + 1)}
	}
	got := Merge(a, b)
	if len(got) != n {
		t.Fatalf("len(Merge(a,b)) = %d, want %d", len(got), n)
	}
	// Spot-check a few entries: LastRemoved (i+1) always beats LastAdded
	// (i), so every channel should end up unsubscribed.
	for _, i := range []int{0, 1, n / 2, n - 1} {
		id := fmt.Sprintf("UC%d", i)
		if got[id].Subscribed() {
			t.Errorf("channel %s: want unsubscribed, got subscribed (%+v)", id, got[id])
		}
	}
}

func TestStamp(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		at      int64
		want    State
		wantErr bool
	}{
		{
			name:  "add",
			event: Event{ChannelID: "UC1", Action: Add},
			at:    123,
			want:  State{"UC1": {LastAdded: 123}},
		},
		{
			name:  "remove",
			event: Event{ChannelID: "UC1", Action: Remove},
			at:    456,
			want:  State{"UC1": {LastRemoved: 456}},
		},
		{
			name:    "invalid action",
			event:   Event{ChannelID: "UC1", Action: "bogus"},
			at:      1,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Stamp(tt.event, tt.at)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Stamp() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Stamp() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApplyEvents(t *testing.T) {
	t.Run("empty batch is a no-op", func(t *testing.T) {
		state := State{"UC1": {LastAdded: 5}}
		got, err := ApplyEvents(state, nil, 999)
		if err != nil {
			t.Fatalf("ApplyEvents: %v", err)
		}
		if !reflect.DeepEqual(got, state) {
			t.Errorf("ApplyEvents(state, nil, ...) = %+v, want %+v", got, state)
		}
	})

	t.Run("applies add and remove in one batch", func(t *testing.T) {
		state := State{}
		events := []Event{
			{ChannelID: "UC1", Action: Add},
			{ChannelID: "UC2", Action: Add},
			{ChannelID: "UC2", Action: Remove}, // superseded within the same batch, later in slice
		}
		got, err := ApplyEvents(state, events, 100)
		if err != nil {
			t.Fatalf("ApplyEvents: %v", err)
		}
		want := State{
			"UC1": {LastAdded: 100},
			"UC2": {LastAdded: 100, LastRemoved: 100},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ApplyEvents() = %+v, want %+v", got, want)
		}
		if got["UC2"].Subscribed() {
			t.Error("UC2: want unsubscribed after same-timestamp add+remove tie")
		}
	})

	t.Run("same-channel conflicting events in a batch are order-independent", func(t *testing.T) {
		state := State{}
		forward := []Event{{ChannelID: "UC1", Action: Add}, {ChannelID: "UC1", Action: Remove}}
		backward := []Event{{ChannelID: "UC1", Action: Remove}, {ChannelID: "UC1", Action: Add}}
		gotForward, err := ApplyEvents(state, forward, 42)
		if err != nil {
			t.Fatal(err)
		}
		gotBackward, err := ApplyEvents(state, backward, 42)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotForward, gotBackward) {
			t.Errorf("order-dependence detected: forward=%+v, backward=%+v", gotForward, gotBackward)
		}
	})

	t.Run("propagates stamp error", func(t *testing.T) {
		_, err := ApplyEvents(State{}, []Event{{ChannelID: "UC1", Action: "bogus"}}, 1)
		if err == nil {
			t.Error("ApplyEvents: want error for invalid action")
		}
	})
}

func TestSubscribedChannels(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  []string
	}{
		{"empty", State{}, []string{}},
		{"nil", nil, []string{}},
		{
			name: "mixed subscribed and unsubscribed, sorted",
			state: State{
				"UCz": {LastAdded: 10},
				"UCa": {LastAdded: 10},
				"UCm": {LastAdded: 5, LastRemoved: 10}, // unsubscribed
			},
			want: []string{"UCa", "UCz"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubscribedChannels(tt.state)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SubscribedChannels() = %v, want %v", got, tt.want)
			}
		})
	}
}
