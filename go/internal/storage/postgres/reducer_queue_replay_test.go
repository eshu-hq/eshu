// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueReopenSucceededResetsSucceededWorkItemToPending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	queue := ReducerQueue{
		db:  db,
		Now: func() time.Time { return now },
	}

	reopened, err := queue.ReopenSucceeded(context.Background(), "reducer_scope-1_gen-1_deployment_mapping_repo_1")
	if err != nil {
		t.Fatalf("ReopenSucceeded() error = %v, want nil", err)
	}
	if !reopened {
		t.Fatal("ReopenSucceeded() reopened = false, want true")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'pending'",
		"attempt_count = 0",
		"stage = 'reducer'",
		"status = 'succeeded'",
		"next_attempt_at = NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reopen query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[0], now; got != want {
		t.Fatalf("updated_at arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "reducer_scope-1_gen-1_deployment_mapping_repo_1"; got != want {
		t.Fatalf("work item arg = %v, want %v", got, want)
	}
}

func TestReducerQueueReopenSucceededReturnsFalseWhenNoRowMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{}},
	}
	queue := ReducerQueue{db: db}

	reopened, err := queue.ReopenSucceeded(context.Background(), "missing-work-item")
	if err != nil {
		t.Fatalf("ReopenSucceeded() error = %v, want nil", err)
	}
	if reopened {
		t.Fatal("ReopenSucceeded() reopened = true, want false")
	}
}

func TestReducerQueueReopenSucceededWrapsExecError(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db: &fakeExecQueryer{
			execErrors: []error{errors.New("boom")},
		},
	}

	_, err := queue.ReopenSucceeded(context.Background(), "work-item")
	if err == nil {
		t.Fatal("ReopenSucceeded() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "reopen succeeded reducer work") {
		t.Fatalf("ReopenSucceeded() error = %v, want wrapped reopen context", err)
	}
}

func TestReducerQueueReplayDomainReopensSucceededWorkItem(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	queue := ReducerQueue{
		db:  db,
		Now: func() time.Time { return now },
	}

	replayed, err := queue.ReplayDomain(context.Background(), "scope-1", "gen-1", reducer.DomainWorkloadMaterialization)
	if err != nil {
		t.Fatalf("ReplayDomain() error = %v, want nil", err)
	}
	if !replayed {
		t.Fatal("ReplayDomain() replayed = false, want true")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"scope_id = $2",
		"generation_id = $3",
		"domain = $4",
		"status = 'succeeded'",
		"status = 'pending'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ReplayDomain query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[0], now; got != want {
		t.Fatalf("updated_at arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "scope-1"; got != want {
		t.Fatalf("scope arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[2], "gen-1"; got != want {
		t.Fatalf("generation arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], string(reducer.DomainWorkloadMaterialization); got != want {
		t.Fatalf("domain arg = %v, want %v", got, want)
	}
}

func TestReducerQueueReplayDomainReturnsFalseWhenNoSucceededRowMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{}},
	}
	queue := ReducerQueue{db: db}

	replayed, err := queue.ReplayDomain(context.Background(), "scope-1", "gen-1", reducer.DomainWorkloadMaterialization)
	if err != nil {
		t.Fatalf("ReplayDomain() error = %v, want nil", err)
	}
	if replayed {
		t.Fatal("ReplayDomain() replayed = true, want false")
	}
}

func TestReducerQueueReplayWorkloadMaterializationEnqueuesReplayWhenNoSucceededRowExists(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 10, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{
			rowsAffectedResult{},
			rowsAffectedResult{rowsAffected: 1},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	replayed, err := queue.ReplayWorkloadMaterialization(
		context.Background(),
		"scope-1",
		"gen-1",
		"repo:service-gha",
	)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterialization() error = %v, want nil", err)
	}
	if !replayed {
		t.Fatal("ReplayWorkloadMaterialization() replayed = false, want true")
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	enqueueQuery := db.execs[1].query
	if !strings.Contains(enqueueQuery, "INSERT INTO fact_work_items") {
		t.Fatalf("enqueue query missing insert:\n%s", enqueueQuery)
	}
	if got, want := db.execs[1].args[1], "scope-1"; got != want {
		t.Fatalf("enqueue scope arg = %v, want %v", got, want)
	}
	if got, want := db.execs[1].args[2], "gen-1"; got != want {
		t.Fatalf("enqueue generation arg = %v, want %v", got, want)
	}
	if got, want := db.execs[1].args[3], string(reducer.DomainWorkloadMaterialization); got != want {
		t.Fatalf("enqueue domain arg = %v, want %v", got, want)
	}
	payload, ok := db.execs[1].args[7].([]byte)
	if !ok {
		t.Fatalf("enqueue payload type = %T, want []byte", db.execs[1].args[7])
	}
	if !strings.Contains(string(payload), "deployment mapping resolved stronger evidence") {
		t.Fatalf("enqueue payload = %s, want replay reason", payload)
	}
	if !strings.Contains(string(payload), "repo:service-gha") {
		t.Fatalf("enqueue payload = %s, want replay entity key", payload)
	}
}

func TestReducerQueueCountInFlightByDomainReturnsCount(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{3}}},
		},
	}
	queue := ReducerQueue{db: db}

	count, err := queue.CountInFlightByDomain(
		context.Background(),
		reducer.DomainDeploymentMapping,
	)
	if err != nil {
		t.Fatalf("CountInFlightByDomain() error = %v, want nil", err)
	}
	if got, want := count, 3; got != want {
		t.Fatalf("CountInFlightByDomain() = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "status NOT IN ('succeeded', 'dead_letter')") {
		t.Fatalf("count query = %q, want terminal-status filter", db.queries[0].query)
	}
	if got, want := db.queries[0].args[0], string(reducer.DomainDeploymentMapping); got != want {
		t.Fatalf("domain arg = %v, want %v", got, want)
	}
}

func TestReducerQueueCountInFlightByDomainRejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{db: &fakeExecQueryer{}}

	_, err := queue.CountInFlightByDomain(context.Background(), reducer.Domain("not-real"))
	if err == nil {
		t.Fatal("CountInFlightByDomain() error = nil, want non-nil")
	}
}

// TestReducerQueueValidateEnqueueAcceptsZeroLeaseFields is the load-bearing
// regression test for issue #170. It proves the enqueue path no longer demands
// LeaseOwner/LeaseDuration placeholder values. Before the validate() split,
// callers like IngestionStore.EnqueueConfigStateDriftIntents had to fabricate
// lease values just to pass the single combined validate() check, even though
// the SQL writes NULL for lease_owner/claim_until on insert. After the split,
// validateEnqueue() omits the lease-side checks; only validateClaim() needs
// them.
func TestReducerQueueValidateEnqueueAcceptsZeroLeaseFields(t *testing.T) {
	t.Parallel()

	recorder := &reducerRecordingDB{}
	queue := ReducerQueue{db: recorder}

	// No-op enqueue with empty intents still runs validateEnqueue().
	if _, err := queue.Enqueue(context.Background(), nil); err != nil {
		t.Fatalf("Enqueue(nil) error = %v, want nil (zero lease fields should be allowed on enqueue)", err)
	}

	// Real enqueue with intents - validateEnqueue() must pass without
	// LeaseOwner / LeaseDuration set.
	intents := []projector.ReducerIntent{{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainConfigStateDrift,
		EntityKey:    "entity-1",
		Reason:       "regression test",
		SourceSystem: "test",
	}}
	if _, err := queue.Enqueue(context.Background(), intents); err != nil {
		t.Fatalf("Enqueue(intents) error = %v, want nil (zero lease fields should be allowed on enqueue)", err)
	}
	if recorder.execCount != 1 {
		t.Fatalf("expected one INSERT exec, got %d", recorder.execCount)
	}
}

// TestReducerQueueValidateEnqueueRequiresDB confirms validateEnqueue() still
// rejects a queue with no database handle and that the error string names the
// enqueue side so stack traces and wrapped errors are self-locating.
func TestReducerQueueValidateEnqueueRequiresDB(t *testing.T) {
	t.Parallel()

	var queue ReducerQueue
	intents := []projector.ReducerIntent{{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainConfigStateDrift,
		EntityKey:    "entity-1",
	}}

	_, err := queue.Enqueue(context.Background(), intents)
	if err == nil {
		t.Fatal("Enqueue() error = nil, want validation error for nil db")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "database is required")
	}
	if !strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, want enqueue-side marker %q", err.Error(), "for enqueue")
	}
}

// TestReducerQueueValidateClaimRequiresLeaseOwner confirms validateClaim()
// rejects a queue missing LeaseOwner with a claim-side error marker.
func TestReducerQueueValidateClaimRequiresLeaseOwner(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseDuration: time.Minute,
	}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want validation error for missing lease owner")
	}
	if !strings.Contains(err.Error(), "lease owner") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "lease owner")
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
}

// TestReducerQueueValidateClaimRequiresPositiveLeaseDuration confirms
// validateClaim() rejects a non-positive LeaseDuration on the heartbeat path.
// Heartbeat is the most lease-sensitive consumer because it renews
// claim_until from now+LeaseDuration.
func TestReducerQueueValidateClaimRequiresPositiveLeaseDuration(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:         &fakeExecQueryer{},
		LeaseOwner: "test-owner",
	}

	err := queue.Heartbeat(context.Background(), reducer.Intent{IntentID: "work-1"})
	if err == nil {
		t.Fatal("Heartbeat() error = nil, want validation error for zero lease duration")
	}
	if !strings.Contains(err.Error(), "lease duration") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "lease duration")
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
}

// TestReducerQueueValidateEnqueueRejectsInvalidClaimDomain proves the shared
// ClaimDomain.Validate() check fires on the enqueue path and that the wrapped
// error carries the enqueue-side marker so callers can self-locate failures
// without parsing call stacks.
func TestReducerQueueValidateEnqueueRejectsInvalidClaimDomain(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:          &fakeExecQueryer{},
		ClaimDomain: reducer.Domain("not_a_domain"),
	}

	_, err := queue.Enqueue(context.Background(), []projector.ReducerIntent{{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainConfigStateDrift,
		EntityKey:    "entity-1",
	}})
	if err == nil {
		t.Fatal("Enqueue() error = nil, want unknown-domain validation error")
	}
	if !strings.Contains(err.Error(), "unknown reducer domain") {
		t.Fatalf("error = %q, want unknown-domain message", err.Error())
	}
	if !strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, want enqueue-side marker %q", err.Error(), "for enqueue")
	}
}

// TestReducerQueueValidateClaimAlsoRejectsInvalidClaimDomain proves
// validateClaim() also fails on an invalid ClaimDomain and that the wrapped
// error carries the claim-side marker (not the enqueue-side marker) even
// though the underlying check is shared with the enqueue path. This is the
// regression guard for Copilot review of PR #196: previously validateClaim
// delegated to validateEnqueue verbatim, so a shared-check failure on the
// claim path bubbled up labeled "for enqueue".
func TestReducerQueueValidateClaimAlsoRejectsInvalidClaimDomain(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		ClaimDomain:   reducer.Domain("not_a_domain"),
	}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want unknown-domain validation error")
	}
	if !strings.Contains(err.Error(), "unknown reducer domain") {
		t.Fatalf("error = %q, want unknown-domain message", err.Error())
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
	if strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, must not carry enqueue-side marker on claim path", err.Error())
	}
}

// TestReducerQueueValidateClaimRequiresDBWithClaimSideMarker is the
// regression guard for the validateClaim->validateEnqueue composition trap
// caught in Copilot review of PR #196: a db-nil failure on the claim path
// must say "for claim/ack/heartbeat/fail", not "for enqueue". Before the
// validateShared refactor, validateClaim delegated to validateEnqueue
// verbatim, so the shared db-nil check produced an enqueue-marked error on
// the claim path.
func TestReducerQueueValidateClaimRequiresDBWithClaimSideMarker(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{LeaseOwner: "x", LeaseDuration: time.Minute}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want validation error for nil db")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "database is required")
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
	if strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, must not carry enqueue-side marker on claim path", err.Error())
	}
}
