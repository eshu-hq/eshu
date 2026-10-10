// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// TestMarkProjectionWriteStartedFenceSync is the deterministic half of the
// #7819 exclusion proof. The write-start marker locks the scope's
// projector_scope_claim_fences row SKIP LOCKED and bumps the fence when the
// marker sets, so the marker commit and a claim's fence lock serialize: a
// claim whose snapshot predates the marker drops its candidate through the
// #7115 fence recheck instead of claiming a newer row while the sweep spares
// the marked retry (the skip-blind split). A busy fence defers the marker
// like a lock timeout; the caller re-runs it.
func TestMarkProjectionWriteStartedFenceSync(t *testing.T) {
	dsn := supersessionProofDSN(t)
	work := heartbeatProofWork("gen-il")

	// A fence row held by an in-flight claim defers the marker without
	// waiting; the re-run after release marks and bumps the fence by one.
	t.Run("defers_on_busy_fence", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		before, ok := fenceRow(t, control, "scope-hb")
		if !ok {
			t.Fatal("scope-hb has no claim fence row")
		}
		holderDB := openSessionOnProofSchema(ctx, t, control, dsn)
		holder, err := holderDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin holder: %v", err)
		}
		defer func() { _ = holder.Rollback() }()
		if _, err := holder.ExecContext(ctx,
			`SELECT fence FROM projector_scope_claim_fences WHERE scope_id = 'scope-hb' FOR NO KEY UPDATE`); err != nil {
			t.Fatalf("hold the fence row: %v", err)
		}
		queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
		err = queue.MarkProjectionWriteStarted(ctx, work)
		if !errors.Is(err, failure.ErrWorkWriteMarkerDeferred) {
			t.Fatalf("MarkProjectionWriteStarted on a held fence row = %v, want ErrWorkWriteMarkerDeferred", err)
		}
		if !errors.Is(err, failure.ErrWorkWriteMarkerFenceBusy) {
			t.Fatalf("MarkProjectionWriteStarted on a held fence row = %v, want the fence-busy cause", err)
		}
		if err := holder.Rollback(); err != nil {
			t.Fatalf("release holder: %v", err)
		}
		if err := queue.MarkProjectionWriteStarted(ctx, work); err != nil {
			t.Fatalf("re-run after the fence freed = %v, want nil", err)
		}
		after, ok := fenceRow(t, control, "scope-hb")
		if !ok {
			t.Fatal("scope-hb lost its claim fence row")
		}
		if after != before+1 {
			t.Fatalf("fence = %d, want exactly one bump from %d", after, before)
		}
		if got := readWriteMarkerState(ctx, t, control); got != "pending,true,running" {
			t.Fatalf("state = %s, want pending,true,running", got)
		}
	})

	// A refused marker (lost claim here) bumps nothing: nothing was written
	// for a concurrent claim to re-read.
	t.Run("no_bump_when_refused", func(t *testing.T) {
		control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
		ctx := context.Background()
		before, ok := fenceRow(t, control, "scope-hb")
		if !ok {
			t.Fatal("scope-hb has no claim fence row")
		}
		queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
		stale := work
		stale.AttemptCount = 2
		if err := queue.MarkProjectionWriteStarted(ctx, stale); !errors.Is(err, failure.ErrWorkClaimLost) {
			t.Fatalf("MarkProjectionWriteStarted for a stale attempt = %v, want ErrWorkClaimLost", err)
		}
		after, ok := fenceRow(t, control, "scope-hb")
		if !ok {
			t.Fatal("scope-hb lost its claim fence row")
		}
		if after != before {
			t.Fatalf("fence = %d, want unchanged at %d", after, before)
		}
	})
}

// TestMarkProjectionWriteStartedMissingFenceRefuses is the #7907 missing-fence
// proof: a scope whose fence row is gone is itself gone (the fence trigger
// creates the row with the scope and the scope delete cascades to it), so the
// marker refuses immediately through the refusal classifier instead of
// spinning the deferral bound like a busy fence.
func TestMarkProjectionWriteStartedMissingFenceRefuses(t *testing.T) {
	control := heartbeatProofDB(t, writeMarkerInterleaveSeed)
	work := heartbeatProofWork("gen-il")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := control.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = 'scope-hb'`); err != nil {
		t.Fatalf("delete scope-hb: %v", err)
	}
	if _, ok := fenceRow(t, control, "scope-hb"); ok {
		t.Fatal("scope-hb still has a claim fence row after the scope delete")
	}
	queue := NewProjectorQueue(SQLDB{DB: control}, "proof-worker", time.Minute)
	start := time.Now()
	err := queue.MarkProjectionWriteStarted(ctx, work)
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("MarkProjectionWriteStarted on a gone scope = %v, want ErrWorkSuperseded without deferring", err)
	}
	if got := time.Since(start); got > 10*time.Second {
		t.Fatalf("missing-fence refusal took %v, want one immediate attempt", got)
	}
}
