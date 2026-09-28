// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// assertFenceCounter checks eshu_dp_projector_delta_baseline_fence_total has
// exactly one point with the phase and outcome, or none when outcome is "".
func assertFenceCounter(t *testing.T, reader sdkmetric.Reader, phase, outcome string) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	total := counterTotal(rm, "eshu_dp_projector_delta_baseline_fence_total")
	if outcome == "" {
		if total != 0 {
			t.Fatalf("fence counter = %d, want 0", total)
		}
		return
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_projector_delta_baseline_fence_total",
		map[string]string{"phase": phase, "outcome": outcome})
	if total != 1 {
		t.Fatalf("fence counter = %d, want 1", total)
	}
}

// TestProjectorDeltaBaselinePreflightLive runs the shared preflight against
// Postgres: a refusal marks with the preflight class and writes nothing else;
// a pass writes nothing and counts nothing.
func TestProjectorDeltaBaselinePreflightLive(t *testing.T) {
	dsn := proofDSN(t)
	t.Run("refused", func(t *testing.T) {
		database := provisionFenceProof(t, dsn, "gen-b", []fenceGen{genActiveB, pendingDelta("A")}, "gen-d")
		queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
		instruments, reader := newEnqueueInstruments(t)
		err := projector.PreflightDeltaBaseline(context.Background(), queue, fenceWork("gen-d"), instruments, nil)
		if !projector.IsDeltaBaselineRefusal(err) {
			t.Fatalf("PreflightDeltaBaseline() = %v, want a delta-baseline refusal", err)
		}
		assertFenceRefused(t, readFenceState(t, database, "gen-d"), projector.DeltaBaselinePhasePreflight, "gen-b", "A", "B")
		assertFenceCounter(t, reader, projector.DeltaBaselinePhasePreflight, "refused_active_differs")
	})
	t.Run("matched", func(t *testing.T) {
		database := provisionFenceProof(t, dsn, "gen-a", []fenceGen{genActiveA, pendingDelta("A")}, "gen-d")
		queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
		instruments, reader := newEnqueueInstruments(t)
		if err := projector.PreflightDeltaBaseline(context.Background(), queue, fenceWork("gen-d"), instruments, nil); err != nil {
			t.Fatalf("PreflightDeltaBaseline() = %v, want nil", err)
		}
		if got := readFenceState(t, database, "gen-d"); got.work != "running" || got.target != "pending" || got.pointer != "gen-a" {
			t.Fatalf("state after pass = %+v, want untouched", got)
		}
		assertFenceCounter(t, reader, projector.DeltaBaselinePhasePreflight, "")
	})
}

// TestProjectorDeltaBaselineAckLeaseLostBeforeMark reclaims the work row
// between Ack's rollback and its mark. The mark's claim fences match nothing,
// Ack reports a lost claim, and the new owner's row is untouched.
func TestProjectorDeltaBaselineAckLeaseLostBeforeMark(t *testing.T) {
	database := provisionFenceProof(t, proofDSN(t), "gen-b", []fenceGen{genActiveB, pendingDelta("A")}, "gen-d")
	hook := &markHookDB{SQLDB: SQLDB{DB: database}, before: func() error {
		_, err := database.ExecContext(context.Background(),
			`UPDATE fact_work_items SET lease_owner = 'other-worker', attempt_count = attempt_count + 1 WHERE generation_id = 'gen-d'`)
		return err
	}}
	queue := NewProjectorQueue(hook, "proof-worker", time.Minute)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments
	if err := queue.Ack(context.Background(), fenceWork("gen-d"), runtime.Result{}); !errors.Is(err, ErrProjectorClaimRejected) {
		t.Fatalf("Ack() = %v, want ErrProjectorClaimRejected", err)
	}
	var owner string
	if err := database.QueryRowContext(context.Background(),
		"SELECT lease_owner FROM fact_work_items WHERE generation_id = 'gen-d'").Scan(&owner); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	got := readFenceState(t, database, "gen-d")
	if got.work != "running" || owner != "other-worker" || got.target != "pending" || got.pointer != "gen-b" {
		t.Fatalf("state = %+v owner %s; want the new owner's running row, gen-d pending, gen-b published", got, owner)
	}
	assertFenceCounter(t, reader, projector.DeltaBaselinePhaseAck, "")
}

// TestProjectorDeltaBaselineMarkFailureConverges fails Ack's mark once. The
// Ack rolled back, so nothing moved; the next attempt's preflight refuses and
// marks the delta.
func TestProjectorDeltaBaselineMarkFailureConverges(t *testing.T) {
	database := provisionFenceProof(t, proofDSN(t), "gen-b", []fenceGen{genActiveB, pendingDelta("A")}, "gen-d")
	hook := &markHookDB{SQLDB: SQLDB{DB: database}, before: func() error { return errors.New("injected mark failure") }}
	err := NewProjectorQueue(hook, "proof-worker", time.Minute).Ack(context.Background(), fenceWork("gen-d"), runtime.Result{})
	if err == nil || errors.Is(err, failure.ErrWorkSuperseded) || errors.Is(err, ErrProjectorClaimRejected) {
		t.Fatalf("Ack() with failed mark = %v, want a plain error", err)
	}
	if got := readFenceState(t, database, "gen-d"); got.work != "running" || got.target != "pending" || got.pointer != "gen-b" {
		t.Fatalf("state after failed mark = %+v, want the Ack rolled back", got)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	if err := projector.PreflightDeltaBaseline(context.Background(), queue, fenceWork("gen-d"), nil, nil); !projector.IsDeltaBaselineRefusal(err) {
		t.Fatalf("next attempt preflight = %v, want a refusal", err)
	}
	assertFenceRefused(t, readFenceState(t, database, "gen-d"), projector.DeltaBaselinePhasePreflight, "gen-b", "A", "B")
}

// TestProjectorDeltaBaselineReadAndMarkNeverWaitOnScopeRow holds the scope row
// the way an ingestion commit does. The preflight read and the refusal mark
// must finish without waiting for it.
func TestProjectorDeltaBaselineReadAndMarkNeverWaitOnScopeRow(t *testing.T) {
	dsn := proofDSN(t)
	database := provisionFenceProof(t, dsn, "gen-b", []fenceGen{genActiveB, pendingDelta("A")}, "gen-d")
	holder := searchPathPeer(t, dsn, database)
	holdTx, err := holder.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holdTx.Rollback() }()
	if _, err := holdTx.ExecContext(context.Background(),
		"SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE", fenceProofScope); err != nil {
		t.Fatalf("hold scope row: %v", err)
	}
	if _, err := database.ExecContext(context.Background(), "SET lock_timeout = '500ms'"); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	start := time.Now()
	err = projector.PreflightDeltaBaseline(context.Background(), queue, fenceWork("gen-d"), nil, nil)
	if !projector.IsDeltaBaselineRefusal(err) {
		t.Fatalf("preflight under a held scope row = %v, want a refusal (lock_timeout means it waited)", err)
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("preflight took %s under a held scope row, want no wait", elapsed)
	}
}

// searchPathPeer opens a second connection on the proof schema of database.
func searchPathPeer(t *testing.T, dsn string, database *sql.DB) *sql.DB {
	t.Helper()
	var searchPath string
	if err := database.QueryRowContext(context.Background(), "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read search_path: %v", err)
	}
	peer := openLivenessProofDB(t, dsn)
	if _, err := peer.ExecContext(context.Background(), "SET search_path TO "+searchPath); err != nil {
		t.Fatalf("set peer search_path: %v", err)
	}
	return peer
}
