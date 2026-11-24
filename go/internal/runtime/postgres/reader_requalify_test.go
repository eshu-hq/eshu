// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// requalifyTestAccess builds a single-member fleet whose frozen identity is
// stale: the standby restarted (incarnation "fresh") after the process froze
// "stale" at bootstrap. The writer lineage still names the same cluster.
func requalifyTestAccess(holder *atomic.Pointer[memberIdentity]) *Access {
	return &Access{
		readerMembers: []physicalReaderMember{{ordinal: 0, id: "read-0", maxOpen: 4, identity: holder}},
		allocator:     newReaderAllocator([]int{4}, 4),
		replayTimeout: time.Second,
		lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
		logger:        slog.Default(),
	}
}

func requalifyTestContext(access *Access) context.Context {
	return context.WithValue(context.Background(), checkpointKey{}, checkpoint{
		owner: access, lsn: "0/1", systemID: "1", database: "postgres", incarnation: "1",
	})
}

func staleTestHolder() *atomic.Pointer[memberIdentity] {
	holder := &atomic.Pointer[memberIdentity]{}
	holder.Store(&memberIdentity{incarnation: "stale", addresses: []net.IP{net.ParseIP("10.0.0.1")}})
	return holder
}

// fleetIdentityForTest builds a frozen member pinning for tests that borrow
// through fenceReaderIdentity with a stub pool.
func fleetIdentityForTest(incarnation, address string) *atomic.Pointer[memberIdentity] {
	holder := &atomic.Pointer[memberIdentity]{}
	holder.Store(&memberIdentity{incarnation: incarnation, addresses: []net.IP{net.ParseIP(address)}})
	return holder
}

// TestFleetRequalifiesRestartedMember pins the standby-restart recovery: a
// restart invalidates the frozen member pinning, and the next borrow must
// transparently reverify and refresh it instead of excluding the member
// until the process restarts.
func TestFleetRequalifiesRestartedMember(t *testing.T) {
	t.Parallel()

	holder := staleTestHolder()
	access := requalifyTestAccess(holder)
	access.nextReader.Store(0)
	var calls int
	access.readMemberIdentity = func(context.Context, *physicalReaderMember) (physicalIdentity, string, []net.IP, error) {
		calls++
		return physicalIdentity{
			recovery: "true", readOnly: "on", defaultReadOnly: "on",
			systemID: "1", database: "postgres", incarnation: "fresh",
		}, "10.0.0.2", []net.IP{net.ParseIP("10.0.0.2")}, nil
	}

	failed := false
	member, err := runFleet(access, requalifyTestContext(access), 1,
		func(_ context.Context, _ context.Context, reservation *readerReservation, _ checkpoint) (int, error) {
			if !failed {
				failed = true
				return 0, memberLocalTopology{}
			}
			reservation.Release()
			return reservation.member, nil
		})
	if err != nil {
		t.Fatalf("fleet borrow across a standby restart: %v, want success after requalification", err)
	}
	if member != 0 {
		t.Fatalf("fleet borrow member = %d, want 0", member)
	}
	if calls != 1 {
		t.Fatalf("requalification reads = %d, want exactly 1", calls)
	}
	if got := holder.Load().incarnation; got != "fresh" {
		t.Fatalf("frozen incarnation = %q, want %q", got, "fresh")
	}
}

// TestFleetRequalifyRefusedStaysUnavailable pins the safety side: a member
// that no longer serves the lined-up cluster must stay excluded, failing
// closed with the reader-unavailable sentinel instead of serving foreign data.
func TestFleetRequalifyRefusedStaysUnavailable(t *testing.T) {
	t.Parallel()

	holder := staleTestHolder()
	access := requalifyTestAccess(holder)
	access.nextReader.Store(0)
	access.readMemberIdentity = func(context.Context, *physicalReaderMember) (physicalIdentity, string, []net.IP, error) {
		return physicalIdentity{}, "", nil, errors.New("reader replaced by a foreign cluster")
	}

	_, err := runFleet(access, requalifyTestContext(access), 1,
		func(_ context.Context, _ context.Context, _ *readerReservation, _ checkpoint) (int, error) {
			return 0, memberLocalTopology{}
		})
	// The exclusion keeps the topology marker; borrowFleet masks it as the
	// public "reader connection unavailable" text without changing identity.
	var local memberLocalTopology
	if !errors.As(err, &local) {
		t.Fatalf("refused requalification error = %v, want the member-local topology marker", err)
	}
	if got := holder.Load().incarnation; got != "stale" {
		t.Fatalf("frozen incarnation = %q, want it unchanged at %q", got, "stale")
	}
}

// TestFleetRequalifyRunsOncePerMember bounds the recovery: a member that
// reports a local mismatch again after a refresh is excluded for the rest of
// the call instead of requalifying in a loop until the replay window expires.
func TestFleetRequalifyRunsOncePerMember(t *testing.T) {
	t.Parallel()

	holder := staleTestHolder()
	access := requalifyTestAccess(holder)
	access.nextReader.Store(0)
	var calls int
	access.readMemberIdentity = func(context.Context, *physicalReaderMember) (physicalIdentity, string, []net.IP, error) {
		calls++
		return physicalIdentity{
			recovery: "true", readOnly: "on", defaultReadOnly: "on",
			systemID: "1", database: "postgres", incarnation: "fresh",
		}, "10.0.0.2", []net.IP{net.ParseIP("10.0.0.2")}, nil
	}

	_, err := runFleet(access, requalifyTestContext(access), 1,
		func(_ context.Context, _ context.Context, _ *readerReservation, _ checkpoint) (int, error) {
			return 0, memberLocalTopology{}
		})
	var local memberLocalTopology
	if !errors.As(err, &local) {
		t.Fatalf("persistently stale member error = %v, want the member-local topology marker", err)
	}
	if calls != 1 {
		t.Fatalf("requalification reads = %d, want exactly 1", calls)
	}
}

// TestPublishRequalifiedIdentityVerifiesCluster pins the publish gate
// without network: only a standby proving the lined-up system and database
// refreshes the frozen pinning, and an unchanged read is a no-op.
func TestPublishRequalifiedIdentityVerifiesCluster(t *testing.T) {
	t.Parallel()

	holder := staleTestHolder()
	access := requalifyTestAccess(holder)

	foreign := physicalIdentity{
		recovery: "true", readOnly: "on", defaultReadOnly: "on",
		systemID: "other", database: "postgres", incarnation: "fresh",
	}
	if access.publishRequalifiedIdentity(0, foreign, "10.0.0.2", []net.IP{net.ParseIP("10.0.0.2")}) {
		t.Fatal("published a member serving a foreign cluster, want refusal")
	}
	if got := holder.Load().incarnation; got != "stale" {
		t.Fatalf("frozen incarnation = %q after refused publish, want %q", got, "stale")
	}

	same := physicalIdentity{
		recovery: "true", readOnly: "on", defaultReadOnly: "on",
		systemID: "1", database: "postgres", incarnation: "stale",
	}
	if access.publishRequalifiedIdentity(0, same, "10.0.0.1", []net.IP{net.ParseIP("10.0.0.1")}) {
		t.Fatal("unchanged read refreshed the pinning, want a no-op refusal")
	}

	restarted := physicalIdentity{
		recovery: "true", readOnly: "on", defaultReadOnly: "on",
		systemID: "1", database: "postgres", incarnation: "fresh",
	}
	if !access.publishRequalifiedIdentity(0, restarted, "10.0.0.2", []net.IP{net.ParseIP("10.0.0.2")}) {
		t.Fatal("restarted same-cluster member was not published, want a refresh")
	}
	if got := holder.Load().incarnation; got != "fresh" {
		t.Fatalf("frozen incarnation = %q, want %q", got, "fresh")
	}
	if access.publishRequalifiedIdentity(9, restarted, "10.0.0.2", []net.IP{net.ParseIP("10.0.0.2")}) {
		t.Fatal("out-of-range member publish succeeded, want refusal")
	}
}

// TestTryRequalifyMemberBounds pins the orchestration guardrails: an
// out-of-range member and a member with no dial and no stub refuse without
// touching the network.
func TestTryRequalifyMemberBounds(t *testing.T) {
	t.Parallel()

	holder := staleTestHolder()
	access := requalifyTestAccess(holder)
	if access.tryRequalifyMember(context.Background(), 9) {
		t.Fatal("out-of-range member requalified, want refusal")
	}
	bare := &Access{
		readerMembers: []physicalReaderMember{{ordinal: 0, id: "read-0", identity: staleTestHolder()}},
		allocator:     newReaderAllocator([]int{4}, 4),
		replayTimeout: time.Second,
		lineage:       newWriterLineage(physicalIdentity{systemID: "1", database: "postgres", incarnation: "1"}, lineageObservation{}, nil),
		logger:        slog.Default(),
	}
	if bare.tryRequalifyMember(context.Background(), 0) {
		t.Fatal("member without dial or stub requalified, want refusal")
	}
	if got := holder.Load().incarnation; got != "stale" {
		t.Fatalf("frozen incarnation = %q, want it unchanged at %q", got, "stale")
	}
}
