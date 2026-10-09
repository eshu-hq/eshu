// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestFleetReservationTimeoutClassifiesCapacityWithLiveCaller(t *testing.T) {
	access := &Access{
		readerMembers: []physicalReaderMember{{ordinal: 0, maxOpen: 4}},
		allocator:     newReaderAllocator([]int{4}, 4),
		replayTimeout: 25 * time.Millisecond,
		lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{
		owner: access, lsn: "0/1", systemID: "1", database: "postgres", incarnation: "1",
	})
	held, err := access.allocator.reserve(ctx, []int{0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	_, err = runFleet(access, ctx, 4, func(context.Context, context.Context, *readerReservation, checkpoint) (int, error) {
		t.Fatal("four-slot setup ran without a four-slot reservation")
		return 0, nil
	})
	if !errors.Is(err, db.ErrSnapshotReservationCapacity) || ctx.Err() != nil {
		t.Fatalf("live-caller capacity timeout = %v, caller error = %v", err, ctx.Err())
	}
	reserved, waiters := access.allocator.pressure()
	if reserved[0] != 1 || waiters[0] != 0 {
		t.Fatalf("capacity timeout leaked reservation or waiter: reserved=%v waiters=%v", reserved, waiters)
	}
	probe, err := access.allocator.reserve(ctx, []int{0}, 1)
	if err != nil {
		t.Fatalf("single-reader fallback slot unavailable: %v", err)
	}
	probe.Release()
}

func TestFleetSnapshotSetPreservesReservationCapacityThroughPrivateFailure(t *testing.T) {
	access := reservationCapacityAccess(25 * time.Millisecond)
	// MaxReadConnections needs a reader handle, but the held allocator slot
	// prevents snapshot setup from borrowing a database connection.
	access.reader = &sql.DB{}
	access.reader.SetMaxOpenConns(4)
	ctx := reservationCheckpointContext(t.Context(), access)
	held, err := access.allocator.reserve(ctx, []int{0}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 4)
	if set != nil || !errors.Is(err, db.ErrSnapshotReservationCapacity) || ctx.Err() != nil {
		t.Fatalf("snapshot set = %v, capacity error = %v, caller error = %v", set, err, ctx.Err())
	}
	reserved, waiters := access.allocator.pressure()
	if reserved[0] != 1 || waiters[0] != 0 {
		t.Fatalf("snapshot timeout leaked reservation or waiter: reserved=%v waiters=%v", reserved, waiters)
	}
	held.Release()
	probe, err := access.allocator.reserve(ctx, []int{0}, 4)
	if err != nil {
		t.Fatalf("subsequent snapshot reservation failed: %v", err)
	}
	probe.Release()
}

func TestFleetReservationCapacityExcludesCallerCancellationAndSingleReads(t *testing.T) {
	access := reservationCapacityAccess(25 * time.Millisecond)
	ctx := reservationCheckpointContext(t.Context(), access)
	held, err := access.allocator.reserve(ctx, []int{0}, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	for _, test := range []struct {
		name  string
		count int
	}{
		{name: "caller deadline", count: 4},
		{name: "single read", count: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := ctx
			if test.name == "caller deadline" {
				var cancel context.CancelFunc
				request, cancel = context.WithTimeout(ctx, 5*time.Millisecond)
				defer cancel()
			}
			_, runErr := runFleet(access, request, test.count, func(context.Context, context.Context, *readerReservation, checkpoint) (int, error) {
				t.Fatal("setup ran despite all slots being held")
				return 0, nil
			})
			if !errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, db.ErrSnapshotReservationCapacity) {
				t.Fatalf("non-capacity timeout = %v", runErr)
			}
			reserved, waiters := access.allocator.pressure()
			if reserved[0] != 4 || waiters[0] != 0 {
				t.Fatalf("timeout leaked reservation or waiter: reserved=%v waiters=%v", reserved, waiters)
			}
		})
	}
}

func TestFleetReservationCapacityExcludesSetupFailure(t *testing.T) {
	access := reservationCapacityAccess(25 * time.Millisecond)
	ctx := reservationCheckpointContext(t.Context(), access)
	setupErr := errors.New("seeded snapshot setup failure")
	_, err := runFleet(access, ctx, 4, func(context.Context, context.Context, *readerReservation, checkpoint) (int, error) {
		return 0, setupErr
	})
	if !errors.Is(err, setupErr) || errors.Is(err, db.ErrSnapshotReservationCapacity) {
		t.Fatalf("setup failure classification = %v", err)
	}
	reserved, waiters := access.allocator.pressure()
	if reserved[0] != 0 || waiters[0] != 0 {
		t.Fatalf("setup failure leaked reservation or waiter: reserved=%v waiters=%v", reserved, waiters)
	}
	probe, err := access.allocator.reserve(ctx, []int{0}, 4)
	if err != nil {
		t.Fatalf("subsequent snapshot reservation failed: %v", err)
	}
	probe.Release()
}

func TestFleetReservationCapacityExcludesPriorTransientSetupFailure(t *testing.T) {
	access := &Access{
		readerMembers: []physicalReaderMember{{ordinal: 0, maxOpen: 4}, {ordinal: 1, maxOpen: 4}},
		allocator:     newReaderAllocator([]int{4, 4}, 8),
		replayTimeout: 50 * time.Millisecond,
		lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
	}
	ctx := reservationCheckpointContext(t.Context(), access)
	held, err := access.allocator.reserve(ctx, []int{1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	attempts := 0
	_, err = runFleet(access, ctx, 4, func(_ context.Context, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
		attempts++
		if reservation.member != 0 {
			t.Fatalf("setup member = %d, want first member 0", reservation.member)
		}
		return 0, syscall.ECONNREFUSED
	})
	if attempts != 1 || !errors.Is(err, syscall.ECONNREFUSED) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, db.ErrSnapshotReservationCapacity) || ctx.Err() != nil {
		t.Fatalf("transient setup then capacity wait: attempts=%d err=%v caller=%v", attempts, err, ctx.Err())
	}
	reserved, waiters := access.allocator.pressure()
	if reserved[0] != 0 || reserved[1] != 1 || waiters[0] != 0 || waiters[1] != 0 {
		t.Fatalf("setup failure and timeout leaked reservation or waiter: reserved=%v waiters=%v", reserved, waiters)
	}
}

func TestFleetReservationCancelReleaseRaceDoesNotLeak(t *testing.T) {
	for range 50 {
		access := reservationCapacityAccess(time.Second)
		ctx := reservationCheckpointContext(t.Context(), access)
		held, err := access.allocator.reserve(ctx, []int{0}, 4)
		if err != nil {
			t.Fatal(err)
		}
		request, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, runErr := runFleet(access, request, 4, func(_ context.Context, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
				reservation.Release()
				return 0, nil
			})
			done <- runErr
		}()
		waitAllocatorQueue(t, access.allocator, 0, 1)
		released := make(chan struct{})
		go func() {
			held.Release()
			close(released)
		}()
		cancel()
		if runErr := <-done; runErr != nil && !errors.Is(runErr, context.Canceled) {
			t.Fatalf("cancel/release result = %v", runErr)
		}
		<-released
		reserved, waiters := access.allocator.pressure()
		if reserved[0] != 0 || waiters[0] != 0 {
			t.Fatalf("cancel/release leaked reservation or waiter: reserved=%v waiters=%v", reserved, waiters)
		}
	}
}

func reservationCapacityAccess(timeout time.Duration) *Access {
	return &Access{
		readerMembers: []physicalReaderMember{{ordinal: 0, maxOpen: 4}},
		allocator:     newReaderAllocator([]int{4}, 4),
		replayTimeout: timeout,
		lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
	}
}

func reservationCheckpointContext(ctx context.Context, access *Access) context.Context {
	return context.WithValue(ctx, checkpointKey{}, checkpoint{
		owner: access, lsn: "0/1", systemID: "1", database: "postgres", incarnation: "1",
	})
}

func BenchmarkFleetReservationClassification(b *testing.B) {
	b.Run("available", func(b *testing.B) {
		access := reservationCapacityAccess(time.Second)
		ctx := reservationCheckpointContext(b.Context(), access)
		b.ResetTimer()
		for range b.N {
			_, err := runFleet(access, ctx, 4, func(_ context.Context, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
				reservation.Release()
				return 0, nil
			})
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("contended", func(b *testing.B) {
		access := reservationCapacityAccess(100 * time.Microsecond)
		ctx := reservationCheckpointContext(b.Context(), access)
		held, err := access.allocator.reserve(ctx, []int{0}, 1)
		if err != nil {
			b.Fatal(err)
		}
		defer held.Release()
		b.ResetTimer()
		for range b.N {
			_, runErr := runFleet(access, ctx, 4, func(context.Context, context.Context, *readerReservation, checkpoint) (int, error) {
				b.Fatal("snapshot setup ran without a full reservation")
				return 0, nil
			})
			if !errors.Is(runErr, context.DeadlineExceeded) {
				b.Fatalf("contended wait did not expire: %v", runErr)
			}
		}
	})
}
