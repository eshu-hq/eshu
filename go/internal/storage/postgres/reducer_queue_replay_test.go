// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueReopenSucceededResetsSucceededWorkItemToPending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	queue := ReducerQueue{
		database: db,
		Now:      func() time.Time { return now },
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
		"container_image_identity_v2_authorized_status = CASE",
		"THEN 'pending'",
		"stage = 'reducer'",
		"status = 'succeeded'",
		"next_attempt_at = NULL",
		// #5848/#5837 round-3 P2-2: reopen must reset reopened_at alongside
		// attempt_count, so Intent.CycleStartedAt (which the AWS drift
		// elapsed-time bound reads) gets a fresh grace window instead of
		// inheriting the row's original, possibly already-elapsed, created_at.
		"reopened_at = $1",
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
	queue := ReducerQueue{database: db}

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
		database: &fakeExecQueryer{
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
		database: db,
		Now:      func() time.Time { return now },
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
		"container_image_identity_v2_authorized_status = CASE",
		"THEN 'pending'",
		"reopened_at = $1",
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
	queue := ReducerQueue{database: db}

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
		database:      db,
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
	queue := ReducerQueue{database: db}

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

	queue := ReducerQueue{database: &fakeExecQueryer{}}

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
	queue := ReducerQueue{database: recorder}

	// No-op enqueue with empty intents still runs validateEnqueue().
	if _, err := queue.Enqueue(context.Background(), nil); err != nil {
		t.Fatalf("Enqueue(nil) error = %v, want nil (zero lease fields should be allowed on enqueue)", err)
	}

	// Real enqueue with intents - validateEnqueue() must pass without
	// LeaseOwner / LeaseDuration set.
	intents := []runtime.ReducerIntent{{
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
	intents := []runtime.ReducerIntent{{
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
		database:      &fakeExecQueryer{},
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
		database:   &fakeExecQueryer{},
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
		database:    &fakeExecQueryer{},
		ClaimDomain: reducer.Domain("not_a_domain"),
	}

	_, err := queue.Enqueue(context.Background(), []runtime.ReducerIntent{{
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
		database:      &fakeExecQueryer{},
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

// TestReopenSucceededReducerSetClauseCoversClaimColumns pins the shared
// reopen SET clause (#7731) to every state column the reducer claim path
// reads. The three reopen queries compose this clause, so dropping an
// assignment here fails hermetically instead of silently narrowing a reopen.
func TestReopenSucceededReducerSetClauseCoversClaimColumns(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"status = 'pending'",
		"attempt_count = 0",
		"container_image_identity_v2_authorized_status = CASE",
		"WHEN container_image_identity_v2_required THEN 'pending'",
		"container_image_identity_v3_authorized_status = CASE",
		"WHEN container_image_identity_v3_required THEN 'pending'",
		"lease_owner = NULL",
		"claim_until = NULL",
		"visible_at = $1",
		"next_attempt_at = NULL",
		"updated_at = $1",
		"reopened_at = $1",
		"failure_class = NULL",
		"failure_message = NULL",
		"failure_details = NULL",
	} {
		if !strings.Contains(ReopenSucceededReducerSetClause, want) {
			t.Fatalf("shared reopen SET clause missing %q:\n%s", want, ReopenSucceededReducerSetClause)
		}
	}
}

// sharedReopenAssignment matches one `column = ` assignment at a line start
// of the shared reopen SET clause.
var sharedReopenAssignment = regexp.MustCompile(`^([a-z][a-z0-9_]*) = `)

// sharedReopenColumns derives every LHS assignment target from
// ReopenSucceededReducerSetClause itself, so a future state column added to
// the shared clause (#7731) automatically becomes required by its mirrors.
// WHEN/ELSE/END lines carry no assignment and are skipped; anything else that
// is not an assignment fails loudly instead of parsing silently.
func sharedReopenColumns(t *testing.T) []string {
	t.Helper()

	var columns []string
	for _, line := range strings.Split(ReopenSucceededReducerSetClause, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "WHEN ") || strings.HasPrefix(upper, "ELSE") || strings.HasPrefix(upper, "END") {
			continue
		}
		m := sharedReopenAssignment.FindStringSubmatch(trimmed)
		if m == nil {
			t.Fatalf("shared reopen SET clause has unparsable line %q", line)
		}
		columns = append(columns, m[1])
	}
	if len(columns) == 0 {
		t.Fatal("shared reopen SET clause yielded no columns")
	}
	return columns
}

// retriggerColumnCase extracts the CASE expression the re-trigger CTE assigns
// to column: the `column = CASE` occurrence at an identifier boundary
// (so `status` never matches inside `..._authorized_status`) on a
// non-comment line, through its closing END. It reports false when no live
// assignment exists.
func retriggerColumnCase(column string) (string, bool) {
	needle := regexp.MustCompile(`\b` + column + ` = CASE`)
	for _, loc := range needle.FindAllStringIndex(serviceCatalogRuntimeRetriggerCTEs, -1) {
		lineStart := strings.LastIndex(serviceCatalogRuntimeRetriggerCTEs[:loc[0]], "\n") + 1
		rest := serviceCatalogRuntimeRetriggerCTEs[loc[0]:]
		lineEnd := strings.Index(rest, "\n")
		line := serviceCatalogRuntimeRetriggerCTEs[lineStart:]
		if lineEnd >= 0 {
			line = serviceCatalogRuntimeRetriggerCTEs[lineStart : loc[0]+lineEnd]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		end := strings.Index(rest, "END")
		if end < 0 {
			return "", false
		}
		return rest[:end+len("END")], true
	}
	return "", false
}

// TestServiceCatalogRetriggerMirrorsSharedReopenColumns pins the #8169
// succeeded-row reopen to the shared reopen SET clause (#7731). The
// re-trigger UPDATE cannot compose ReopenSucceededReducerSetClause directly --
// its assignments are conditional CASE branches because the same statement
// also flags claimed/running rows -- so this guard fails hermetically if a
// shared-clause column silently misses the re-trigger path or loses its
// succeeded branch.
func TestServiceCatalogRetriggerMirrorsSharedReopenColumns(t *testing.T) {
	t.Parallel()

	for _, column := range sharedReopenColumns(t) {
		block, ok := retriggerColumnCase(column)
		if !ok {
			t.Errorf("re-trigger CTE missing succeeded-branch assignment for %q", column)
			continue
		}
		if !strings.Contains(block, "succeeded") {
			t.Errorf("re-trigger CTE assignment for %q has no succeeded branch:\n%s", column, block)
		}
	}
}
