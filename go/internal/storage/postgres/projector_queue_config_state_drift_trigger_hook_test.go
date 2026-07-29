// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// configStateDriftTriggerHookFake implements ExecQueryer + Beginner +
// Transaction (every surface ProjectorQueue.Ack needs) plus
// ConfigStateDriftTrigger, recording every ExecContext call, the Commit call,
// and every TriggerConfigStateDrift call (with its scope/generation
// arguments) into one shared, ordered log.
type configStateDriftTriggerHookFake struct {
	log         *[]string
	triggerArgs *[][2]string
	triggerErr  error
}

func (f configStateDriftTriggerHookFake) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	*f.log = append(*f.log, "exec")
	return driverResult{}, nil
}

func (f configStateDriftTriggerHookFake) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("query not expected in this test")
}

func (f configStateDriftTriggerHookFake) Begin(context.Context) (Transaction, error) {
	return f, nil
}

func (f configStateDriftTriggerHookFake) Commit() error {
	*f.log = append(*f.log, "commit")
	return nil
}

func (f configStateDriftTriggerHookFake) Rollback() error {
	*f.log = append(*f.log, "rollback")
	return nil
}

func (f configStateDriftTriggerHookFake) TriggerConfigStateDrift(_ context.Context, scopeID, generationID string) error {
	*f.log = append(*f.log, "hook-trigger")
	*f.triggerArgs = append(*f.triggerArgs, [2]string{scopeID, generationID})
	return f.triggerErr
}

// TestProjectorQueueAckInvokesConfigStateDriftTriggerAfterCommitForStateSnapshotScope
// proves the runtime delta-trigger required by issue #5593: Ack on a
// state_snapshot:* scope calls the wired ConfigStateDriftTrigger with the
// exact scope/generation that just activated, strictly AFTER Ack's own
// transaction commits (mirroring the CrossplaneRedrive hook's ordering
// contract), and a trigger error is swallowed (logged, not returned) so it
// can never fail Ack -- the generation is already correctly activated by the
// time the hook runs.
func TestProjectorQueueAckInvokesConfigStateDriftTriggerAfterCommitForStateSnapshotScope(t *testing.T) {
	var log []string
	var triggerArgs [][2]string
	fake := configStateDriftTriggerHookFake{log: &log, triggerArgs: &triggerArgs, triggerErr: errors.New("injected trigger failure")}

	queue := ProjectorQueue{
		db:                      fake,
		LeaseOwner:              "test-owner",
		LeaseDuration:           time.Minute,
		ConfigStateDriftTrigger: fake,
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "state_snapshot:s3:hash-1"},
		Generation: scope.ScopeGeneration{GenerationID: "terraform_state:state_snapshot:s3:hash-1:lineage-1:serial:2"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("expected Ack to swallow the trigger's error, got: %v", err)
	}

	if len(log) == 0 || log[len(log)-1] != "hook-trigger" {
		t.Fatalf("expected the hook's TriggerConfigStateDrift call to be the LAST event in the log, got %v", log)
	}
	commitIndex, triggerIndex := -1, -1
	for i, event := range log {
		if event == "commit" {
			commitIndex = i
		}
		if event == "hook-trigger" {
			triggerIndex = i
		}
	}
	if commitIndex == -1 || triggerIndex == -1 {
		t.Fatalf("expected both a commit and a hook-trigger event in the log, got %v", log)
	}
	if triggerIndex < commitIndex {
		t.Fatalf("expected hook-trigger (index %d) to occur AFTER commit (index %d), got %v", triggerIndex, commitIndex, log)
	}

	if len(triggerArgs) != 1 {
		t.Fatalf("expected exactly one TriggerConfigStateDrift call, got %d: %v", len(triggerArgs), triggerArgs)
	}
	if got, want := triggerArgs[0][0], work.Scope.ScopeID; got != want {
		t.Fatalf("trigger scope_id = %q, want %q", got, want)
	}
	if got, want := triggerArgs[0][1], work.Generation.GenerationID; got != want {
		t.Fatalf("trigger generation_id = %q, want %q", got, want)
	}
}

// TestProjectorQueueAckRecordsConfigStateDriftRuntimeTriggerFailureCounter
// proves the issue #5593 P1-2 fix: a TriggerConfigStateDrift error advances
// ConfigStateDriftRuntimeTriggerFailures{outcome="trigger_error"}, mirroring
// runCrossplaneRedriveHook's CrossplaneRedriveSweeps{outcome="sweep_error"}
// precedent, so a systematically failing trigger is visible on a dashboard
// instead of only in logs.
func TestProjectorQueueAckRecordsConfigStateDriftRuntimeTriggerFailureCounter(t *testing.T) {
	var log []string
	var triggerArgs [][2]string
	fake := configStateDriftTriggerHookFake{log: &log, triggerArgs: &triggerArgs, triggerErr: errors.New("injected trigger failure")}
	inst, reader := newEnqueueInstruments(t)

	queue := ProjectorQueue{
		db:                      fake,
		LeaseOwner:              "test-owner",
		LeaseDuration:           time.Minute,
		ConfigStateDriftTrigger: fake,
		Instruments:             inst,
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "state_snapshot:s3:hash-1"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-state-1"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("expected Ack to swallow the trigger's error, got: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	metricName := "eshu_dp_config_state_drift_runtime_trigger_failures_total"
	if got, want := counterTotal(rm, metricName), int64(1); got != want {
		t.Fatalf("%s = %d, want %d", metricName, got, want)
	}
	assertCounterPresentWithLabels(t, rm, metricName, map[string]string{"outcome": "trigger_error"})
}

// TestProjectorQueueAckDoesNotRecordFailureCounterOnSuccessfulTrigger proves
// the failure counter stays at zero when TriggerConfigStateDrift succeeds --
// it is an error-outcome-only signal, not a general activity counter (that
// role belongs to CorrelationDriftIntentsEnqueued).
func TestProjectorQueueAckDoesNotRecordFailureCounterOnSuccessfulTrigger(t *testing.T) {
	var log []string
	var triggerArgs [][2]string
	fake := configStateDriftTriggerHookFake{log: &log, triggerArgs: &triggerArgs}
	inst, reader := newEnqueueInstruments(t)

	queue := ProjectorQueue{
		db:                      fake,
		LeaseOwner:              "test-owner",
		LeaseDuration:           time.Minute,
		ConfigStateDriftTrigger: fake,
		Instruments:             inst,
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "state_snapshot:s3:hash-1"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-state-1"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got, want := counterTotal(rm, "eshu_dp_config_state_drift_runtime_trigger_failures_total"), int64(0); got != want {
		t.Fatalf("failure counter = %d, want %d on a successful trigger", got, want)
	}
}

// TestProjectorQueueAckSkipsConfigStateDriftTriggerForNonStateSnapshotScope
// proves the hook only fires for state_snapshot:* scopes -- every other scope
// kind (code repos, cloud inventory, etc.) activates through the exact same
// Ack path and must not pay for or trigger drift evaluation.
func TestProjectorQueueAckSkipsConfigStateDriftTriggerForNonStateSnapshotScope(t *testing.T) {
	var log []string
	var triggerArgs [][2]string
	fake := configStateDriftTriggerHookFake{log: &log, triggerArgs: &triggerArgs}

	queue := ProjectorQueue{
		db:                      fake,
		LeaseOwner:              "test-owner",
		LeaseDuration:           time.Minute,
		ConfigStateDriftTrigger: fake,
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "repo:github.com/example/repo"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-001"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if len(triggerArgs) != 0 {
		t.Fatalf("expected no TriggerConfigStateDrift call for a non-state_snapshot scope, got %v", triggerArgs)
	}
}

// TestProjectorQueueAckSkipsConfigStateDriftTriggerWhenNilTrigger proves the
// hook is a pure no-op (never panics, never adds latency) when
// ConfigStateDriftTrigger is unwired -- the default for every existing caller
// that predates this feature, including bootstrap-index which MUST NOT wire
// it (see runConfigStateDriftTriggerHook's doc comment).
func TestProjectorQueueAckSkipsConfigStateDriftTriggerWhenNilTrigger(t *testing.T) {
	var log []string
	var triggerArgs [][2]string
	fake := configStateDriftTriggerHookFake{log: &log, triggerArgs: &triggerArgs}

	queue := ProjectorQueue{
		db:            fake,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		// ConfigStateDriftTrigger intentionally left nil.
	}

	work := projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "state_snapshot:s3:hash-1"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-state-1"},
	}

	if err := queue.Ack(context.Background(), work, projector.Result{}); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if len(triggerArgs) != 0 {
		t.Fatalf("expected no TriggerConfigStateDrift call when ConfigStateDriftTrigger is nil, got %v", triggerArgs)
	}
}
