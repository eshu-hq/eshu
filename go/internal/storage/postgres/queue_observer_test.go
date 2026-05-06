package postgres

import (
	"context"
	"errors"
	"testing"
)

func TestQueueObserverStoreQueueDepths(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", "pending", int64(5)},
					{"projector", "claimed", int64(2)},
					{"projector", "running", int64(1)},
					{"projector", "retrying", int64(3)},
					{"reducer", "pending", int64(10)},
					{"reducer", "claimed", int64(4)},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.QueueDepths(context.Background())
	if err != nil {
		t.Fatalf("QueueDepths() error = %v", err)
	}

	// projector: pending=5, in_flight=3 (claimed+running merged), retrying=3
	if depths["projector"]["pending"] != 5 {
		t.Fatalf("projector pending = %d, want 5", depths["projector"]["pending"])
	}
	if depths["projector"]["in_flight"] != 3 {
		t.Fatalf("projector in_flight = %d, want 3 (claimed 2 + running 1)", depths["projector"]["in_flight"])
	}
	if depths["projector"]["retrying"] != 3 {
		t.Fatalf("projector retrying = %d, want 3", depths["projector"]["retrying"])
	}

	// reducer: pending=10, in_flight=4
	if depths["reducer"]["pending"] != 10 {
		t.Fatalf("reducer pending = %d, want 10", depths["reducer"]["pending"])
	}
	if depths["reducer"]["in_flight"] != 4 {
		t.Fatalf("reducer in_flight = %d, want 4", depths["reducer"]["in_flight"])
	}
}

func TestQueueObserverStoreQueueDepthsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.QueueDepths(context.Background())
	if err != nil {
		t.Fatalf("QueueDepths() error = %v", err)
	}
	if len(depths) != 0 {
		t.Fatalf("QueueDepths() = %v, want empty map", depths)
	}
}

func TestQueueObserverStoreQueueDepthsMergesClaimedAndRunning(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", "claimed", int64(7)},
					{"projector", "running", int64(3)},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	depths, err := observer.QueueDepths(context.Background())
	if err != nil {
		t.Fatalf("QueueDepths() error = %v", err)
	}

	if depths["projector"]["in_flight"] != 10 {
		t.Fatalf("in_flight = %d, want 10 (claimed 7 + running 3)", depths["projector"]["in_flight"])
	}
	if _, has := depths["projector"]["claimed"]; has {
		t.Fatal("claimed status should be merged into in_flight, not present separately")
	}
	if _, has := depths["projector"]["running"]; has {
		t.Fatal("running status should be merged into in_flight, not present separately")
	}
}

func TestQueueObserverStoreQueueOldestAge(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{
				rows: [][]any{
					{"projector", 120.5},
					{"reducer", 45.0},
				},
			},
		},
	}

	observer := NewQueueObserverStore(queryer)
	ages, err := observer.QueueOldestAge(context.Background())
	if err != nil {
		t.Fatalf("QueueOldestAge() error = %v", err)
	}

	if ages["projector"] != 120.5 {
		t.Fatalf("projector age = %f, want 120.5", ages["projector"])
	}
	if ages["reducer"] != 45.0 {
		t.Fatalf("reducer age = %f, want 45.0", ages["reducer"])
	}
}

func TestQueueObserverStoreQueueOldestAgeEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{rows: [][]any{}},
		},
	}

	observer := NewQueueObserverStore(queryer)
	ages, err := observer.QueueOldestAge(context.Background())
	if err != nil {
		t.Fatalf("QueueOldestAge() error = %v", err)
	}
	if len(ages) != 0 {
		t.Fatalf("QueueOldestAge() = %v, want empty map", ages)
	}
}

func TestQueueObserverStoreNilQueryer(t *testing.T) {
	t.Parallel()

	observer := &QueueObserverStore{}

	_, err := observer.QueueDepths(context.Background())
	if err == nil {
		t.Fatal("QueueDepths() error = nil, want non-nil for nil queryer")
	}

	_, err = observer.QueueOldestAge(context.Background())
	if err == nil {
		t.Fatal("QueueOldestAge() error = nil, want non-nil for nil queryer")
	}
}

func TestQueueObserverStoreQueueDepthsQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{err: errors.New("connection lost")},
		},
	}

	observer := NewQueueObserverStore(queryer)
	_, err := observer.QueueDepths(context.Background())
	if err == nil {
		t.Fatal("QueueDepths() error = nil, want non-nil")
	}
}

func TestQueueObserverStoreQueueOldestAgeQueryError(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{
		responses: []fakeRows{
			{err: errors.New("connection lost")},
		},
	}

	observer := NewQueueObserverStore(queryer)
	_, err := observer.QueueOldestAge(context.Background())
	if err == nil {
		t.Fatal("QueueOldestAge() error = nil, want non-nil")
	}
}
