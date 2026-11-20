// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errTestWakeFailed stands in for a post-commit wake failure (a lost
// waiting-table write after the providing save committed).
var errTestWakeFailed = errors.New("test wake failed")

// #11016 hardening: post-commit generation_active hint wakes must log and
// meter instead of failing the already-committed operation, and the probe
// must cover the residual terminal transitions (other scopes' sweeps, empty
// sweeps, heartbeat supersession, Ack maintenance). These tests pin the
// wake attempt and the preserved operation outcome, not the log line.

// TestProjectorQueueFailPostCommitWakeErrorKeepsDeadLetter proves F3 on the
// Fail path: the dead-letter already committed, so a wake failure is
// metered but never returned.
func TestProjectorQueueFailPostCommitWakeErrorKeepsDeadLetter(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{wakeErr: errTestWakeFailed}
	queue := NewProjectorQueue(db, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-456"},
	}

	if err := queue.Fail(context.Background(), work, errProjectionFailed); err != nil {
		t.Fatalf("Fail() error = %v, want nil: the dead-letter committed before the wake", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_reducer_waiting_wake_total",
		map[string]string{"wake_site": "in_tx", "outcome": "error"})
}

// TestProjectorQueueClaimProbeErrorKeepsClaimedWork proves F3 on the Claim
// path: a 40P01 from the post-claim probe must not orphan the claimed work
// and must not read as a claim conflict.
func TestProjectorQueueClaimProbeErrorKeepsClaimedWork(t *testing.T) {
	t.Parallel()

	database := &claimConflictQueryer{
		probeErr: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"},
	}
	queue := newClaimConflictQueue(database)
	instruments, reader := newEnqueueInstruments(t)
	queue.Instruments = instruments

	work, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil: the claim committed before the probe", err)
	}
	if !ok || work.Scope.ScopeID != "scope-123" {
		t.Fatalf("Claim() = (%+v, %v), want claimed scope-123 kept", work, ok)
	}
	if got, want := database.calls, 1; got != want {
		t.Fatalf("claim attempts = %d, want %d: a probe failure must not retry the claim", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_queue_claim_conflict_retries_total"); got != 0 {
		t.Fatalf("claim conflict retries = %d, want 0: a probe 40P01 is not a claim conflict", got)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_reducer_waiting_wake_total",
		map[string]string{"wake_site": "in_tx", "outcome": "error"})
}

// TestProjectorQueueClaimEmptySweepProbesTerminalWaits proves F4 for empty
// sweeps: a claim that returns no work still ran the sweep, which may have
// retired other scopes' generations, so the probe still runs.
func TestProjectorQueueClaimEmptySweepProbesTerminalWaits(t *testing.T) {
	t.Parallel()

	database := &claimConflictQueryer{emptyClaim: true}
	queue := newClaimConflictQueue(database)

	_, ok, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v, want nil", err)
	}
	if ok {
		t.Fatal("Claim() ok = true, want false for an empty sweep")
	}
	if got, want := database.probeCalls, 1; got != want {
		t.Fatalf("terminal-wait probes = %d, want %d on an empty sweep", got, want)
	}
}

// TestProjectorQueueClaimProbeCoversEveryScope proves F4 for sweeps of
// other scopes: the post-claim probe takes no scope argument, so one claim
// wakes parked waits on every scope the sweep retired.
func TestProjectorQueueClaimProbeCoversEveryScope(t *testing.T) {
	t.Parallel()

	if strings.Contains(terminalGenerationActiveWaitKeysQuery, "WHERE wait.scope_id") ||
		strings.Contains(terminalGenerationActiveWaitKeysQuery, "wait.scope_id = $") {
		t.Fatalf("probe query still filters on one scope:\n%s", terminalGenerationActiveWaitKeysQuery)
	}
}

// TestProjectorQueueHeartbeatSupersedeWakesParkedWaits proves F4 for the
// heartbeat pending-to-superseded transition: superseding the running work's
// generation reopens its parked generation_active waits.
func TestProjectorQueueHeartbeatSupersedeWakesParkedWaits(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{
		queryRows: []db.Rows{&singleStringRows{value: "pending"}},
	}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-old"},
	}

	err := queue.Heartbeat(context.Background(), work)
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat() error = %v, want failure.ErrWorkSuperseded", err)
	}

	var wake *recordedExecCall
	for i := range database.queries {
		if strings.Contains(database.queries[i].query, "reducer_waiting_items") {
			wake = &database.queries[i]
		}
	}
	if wake == nil {
		t.Fatalf("Heartbeat() runs no waiting-table wake query (%d queries)", len(database.queries))
	}
	if len(wake.args) != 3 || wake.args[1] != "generation_active" {
		t.Fatalf("Heartbeat() wake args = %v, want (now, generation_active, key)", wake.args)
	}
	key, _ := wake.args[2].(string)
	if !strings.Contains(key, "scope-123") || !strings.Contains(key, "generation-old") {
		t.Fatalf("Heartbeat() wake key = %q, want the superseded scope generation", key)
	}
}

// TestProjectorQueueHeartbeatSupersedeWakeErrorKeepsSuperseded proves F3 on
// the heartbeat path: a post-commit wake failure must not mask the
// superseded stop the worker needs.
func TestProjectorQueueHeartbeatSupersedeWakeErrorKeepsSuperseded(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{
		queryRows: []db.Rows{&singleStringRows{value: "pending"}},
		wakeErr:   errTestWakeFailed,
	}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{GenerationID: "generation-old"},
	}

	err := queue.Heartbeat(context.Background(), work)
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Heartbeat() error = %v, want failure.ErrWorkSuperseded despite the wake failure", err)
	}
}

// TestProjectorQueueAckMaintenanceProbeRunsInTx proves F4 for Ack's
// maintenance supersession: the obsolete-generations maintenance retires
// older pending/failed generations while only the activated generation gets
// the activation wake, so Ack also probes the scope's terminal waits in the
// same transaction.
func TestProjectorQueueAckMaintenanceProbeRunsInTx(t *testing.T) {
	t.Parallel()

	database := &recordingExecQueryer{queryRows: []db.Rows{&recordingRows{}, &waitingWakeCountRows{}}}
	queue := NewProjectorQueue(database, "projector-1", 30*time.Second)
	queue.Now = func() time.Time {
		return time.Date(2026, time.April, 12, 14, 30, 0, 0, time.UTC)
	}
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{ScopeID: "scope-123"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-456",
		},
	}

	if err := queue.Ack(context.Background(), work, runtime.Result{}); err != nil {
		t.Fatalf("Ack() error = %v, want nil", err)
	}

	var probe, wake *recordedExecCall
	for i := range database.queries {
		if database.queries[i].query == terminalGenerationActiveWaitKeysQuery {
			probe = &database.queries[i]
		}
		if strings.Contains(database.queries[i].query, "reducer_waiting_items") &&
			strings.Contains(database.queries[i].query, "reopened") {
			wake = &database.queries[i]
		}
	}
	if probe == nil {
		t.Fatalf("Ack() runs no terminal-wait probe query (%d queries)", len(database.queries))
	}
	if wake == nil {
		t.Fatalf("Ack() runs no generation_active wake query (%d queries)", len(database.queries))
	}
	if len(probe.args) != 0 {
		t.Fatalf("Ack() probe args = %v, want none: the probe covers every scope", probe.args)
	}
}
