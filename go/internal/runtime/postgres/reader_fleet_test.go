// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/x509"
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFleetSharedFailuresDoNotRetryAnotherMember(t *testing.T) {
	for _, err := range []error{
		&pgconn.PgError{Code: "28P01"},
		x509.HostnameError{Host: "wrong-reader"},
		x509.UnknownAuthorityError{},
		ErrWrongTopology,
	} {
		if !sharedFleetFailure(context.Background(), err) {
			t.Fatalf("shared error %T was retryable", err)
		}
	}
	if sharedFleetFailure(context.Background(), memberLocalTopology{}) {
		t.Fatal("member-local topology mismatch was treated as global")
	}
}

func TestFleetWholeReadRetryMayRevisitMember(t *testing.T) {
	access := &Access{
		readerMembers: []physicalReaderMember{{ordinal: 0, maxOpen: 4}, {ordinal: 1, maxOpen: 4}},
		allocator:     newReaderAllocator([]int{4, 4}, 8),
		replayTimeout: time.Second,
		lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{
		owner: access, lsn: "0/1", systemID: "1", database: "postgres", incarnation: "1",
	})
	selectMember := func(t *testing.T, failFirst bool) int {
		t.Helper()
		failed := false
		member, err := runFleet(access, ctx, 1, func(_ context.Context, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
			if failFirst && !failed {
				failed = true
				return 0, syscall.ECONNREFUSED
			}
			reservation.Release()
			return reservation.member, nil
		})
		if err != nil {
			t.Fatalf("fleet member selection: %v", err)
		}
		return member
	}

	t.Run("intervening selection", func(t *testing.T) {
		access.nextReader.Store(0)
		first := selectMember(t, false)
		intervening := selectMember(t, false)
		retry := selectMember(t, false)
		if first != 0 || intervening != 1 || retry != first {
			t.Fatalf("member choices first=%d intervening=%d retry=%d, want 0,1,0", first, intervening, retry)
		}
	})
	t.Run("setup fallback", func(t *testing.T) {
		access.nextReader.Store(0)
		first := selectMember(t, true)
		retry := selectMember(t, false)
		if first != 1 || retry != first {
			t.Fatalf("member choices after setup fallback first=%d retry=%d, want 1,1", first, retry)
		}
	})
}

func TestFleetHealthySetupGetsFairShareOfReplayWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps int
		delay time.Duration
	}{
		{name: "one slow stage", steps: 1, delay: 150 * time.Millisecond},
		{name: "four sequential stages", steps: 4, delay: 40 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := &Access{
				readerMembers: []physicalReaderMember{{ordinal: 0, maxOpen: 4}, {ordinal: 1, maxOpen: 4}},
				allocator:     newReaderAllocator([]int{4, 4}, 8),
				replayTimeout: 450 * time.Millisecond,
				lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
			}
			ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{
				owner: access, lsn: "0/1", systemID: "1", database: "postgres", incarnation: "1",
			})
			var attempts []int
			member, err := runFleet(access, ctx, 4, func(tryCtx, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
				attempts = append(attempts, reservation.member)
				if reservation.member == 0 {
					for range tc.steps {
						select {
						case <-time.After(tc.delay):
						case <-tryCtx.Done():
							return 0, tryCtx.Err()
						}
					}
				}
				reservation.Release()
				return reservation.member, nil
			})
			if err != nil || member != 0 || len(attempts) != 1 {
				t.Fatalf("healthy first member result=%d err=%v attempts=%v; want member 0 once", member, err, attempts)
			}
			reserved, waiting := access.allocator.pressure()
			for index := range reserved {
				if reserved[index] != 0 || waiting[index] != 0 {
					t.Fatalf("member %d reserved=%d waiting=%d after setup", index, reserved[index], waiting[index])
				}
			}
		})
	}
}

func TestFleetAttemptDeadlineSharesRemainingWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		window     time.Duration
		members    int
		preReserve time.Duration
	}{
		{name: "two members", window: 450 * time.Millisecond, members: 2},
		{name: "three members", window: 900 * time.Millisecond, members: 3},
		{name: "last member", window: 450 * time.Millisecond, members: 1},
		{name: "after allocator wait", window: 450 * time.Millisecond, members: 2, preReserve: 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, stopParent := context.WithTimeout(t.Context(), tc.window)
			defer stopParent()
			if tc.preReserve > 0 {
				time.Sleep(tc.preReserve)
			}
			started := time.Now()
			parentDeadline, ok := parent.Deadline()
			if !ok {
				t.Fatal("parent deadline is absent")
			}
			attempt, stopAttempt := fleetAttemptContext(parent, tc.members)
			defer stopAttempt()
			attemptDeadline, ok := attempt.Deadline()
			if !ok {
				t.Fatal("attempt deadline is absent")
			}
			want := parentDeadline.Sub(started) / time.Duration(tc.members)
			got := attemptDeadline.Sub(started)
			if got < want-30*time.Millisecond || got > want+30*time.Millisecond || attemptDeadline.After(parentDeadline) {
				t.Fatalf("attempt budget=%s parent remaining=%s members=%d; want near %s within parent", got, parentDeadline.Sub(started), tc.members, want)
			}
		})
	}

	caller, stopCaller := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer stopCaller()
	shared, stopShared := context.WithTimeout(caller, 2*time.Second)
	defer stopShared()
	attempt, stopAttempt := fleetAttemptContext(shared, 2)
	defer stopAttempt()
	deadline, ok := attempt.Deadline()
	if !ok || time.Until(deadline) > 200*time.Millisecond {
		t.Fatalf("short caller deadline was not shared: deadline=%v present=%t", deadline, ok)
	}
}

func TestFleetPingBoundsWriterSaturation(t *testing.T) {
	access := openFleetRegressionAccess(t, 400*time.Millisecond)
	access.pingTimeout = 120 * time.Millisecond
	access.writer.SetMaxOpenConns(1)
	held, err := access.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = access.Ping(ctx)
	elapsed := time.Since(started)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("writer saturation Ping error=%v, want bounded deadline", err)
	}
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("writer saturation Ping took %s, want within PingTimeout", elapsed)
	}
}

func TestFleetSkipsLaggedReaderWithinOneReplayBudget(t *testing.T) {
	access := openFleetRegressionAccess(t, 450*time.Millisecond)
	ctx := t.Context()
	if _, err := access.readerMembers[0].pool.ExecContext(ctx, "SELECT pg_wal_replay_pause()"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := access.readerMembers[0].pool.ExecContext(context.Background(), "SELECT pg_wal_replay_resume()"); err != nil {
			t.Error(err)
		}
	})
	if _, err := access.writer.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS eshu_fleet_replay_proof (value integer)"); err != nil {
		t.Fatal(err)
	}
	if _, err := access.writer.ExecContext(ctx, "INSERT INTO eshu_fleet_replay_proof VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	checkpointCtx, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	access.nextReader.Store(0)
	started := time.Now()
	set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(checkpointCtx, 4)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if elapsed >= 400*time.Millisecond {
		t.Fatalf("lagged A delayed healthy B for %s", elapsed)
	}
	if got := access.readerMembers[1].pool.Stats().InUse; got != 4 {
		t.Fatalf("healthy B holds %d snapshot connections, want 4", got)
	}
}
