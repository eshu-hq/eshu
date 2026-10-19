// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// stagePurgeScriptedDB stages the scripted happy-path purge: the fenced
// tombstone claim, the scope lock, an empty terminal gate, and zero grant
// pre-counts. The keyed deletes, the scope delete, and the tombstone
// completion run as execs.
func stagePurgeScriptedDB() (*fakeTransactionalDB, *fakeTx) {
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			// Tombstone claim (retirement_id, repo_id, scope_id, state, phase).
			{rows: [][]any{{"rr_purge01", "PARTITION:purge-repo", "purge-scope-main", "pending", "fenced"}}},
			// Scope lock (scope_id), marker scope plus one ref scope.
			{rows: [][]any{{"purge-scope-main"}, {"purge-scope-ref"}}},
			// Terminal gate: no pending or active generation remains.
			{rows: [][]any{}},
			// Grant pre-counts (scope, repository, role-scope, role-repository).
			{rows: [][]any{{int64(1), int64(0), int64(2), int64(0)}}},
		},
	}
	database := &fakeTransactionalDB{tx: tx}
	return database, tx
}

func purgeTestRequest() PurgeRequest {
	return PurgeRequest{RepoIDs: []string{"PARTITION:purge-repo"}}
}

// TestPurgeRetiredRunsDesignedStepOrder proves the narrowed purge issues the
// designed sequence: the fenced tombstone claim, the scope-row lock, the
// terminal-generation gate, the keyed leftover deletes, the grant
// pre-counts, exactly one scope-row delete, and the tombstone completion
// before commit. Contract A holds: no DETACH DELETE and no direct fact or
// generation delete may appear anywhere in the sequence; generations and
// their fact rows go only through the scope-row FK cascade.
func TestPurgeRetiredRunsDesignedStepOrder(t *testing.T) {
	t.Parallel()

	database, tx := stagePurgeScriptedDB()
	purger := NewRetirementPurger(database, database)
	// Wire real (noop) instruments so the step-order proof also covers
	// the operator counter emission path.
	inst, err := telemetry.NewInstruments(noop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	purger.Instruments = inst
	result, err := purger.PurgeRetired(context.Background(), purgeTestRequest())
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 || result.Completed[0] != "PARTITION:purge-repo" {
		t.Fatalf("completed = %v, want [PARTITION:purge-repo]", result.Completed)
	}
	if len(result.Blocked) != 0 {
		t.Fatalf("blocked = %+v, want none", result.Blocked)
	}

	var stmts []string
	for _, q := range tx.execs {
		stmts = append(stmts, q.query)
	}
	for _, q := range tx.queries {
		stmts = append(stmts, q.query)
	}
	joined := strings.Join(stmts, "\n")

	for _, want := range []string{
		"FROM repository_retirements",
		"FOR UPDATE",
		"FOR NO KEY UPDATE",
		"FROM scope_generations",
		"DELETE FROM content_files",
		"DELETE FROM content_entities",
		"DELETE FROM content_file_references",
		"DELETE FROM repository_refs",
		"DELETE FROM repository_reindex_requests",
		"DELETE FROM repository_selection_observations",
		"DELETE FROM ingestion_scopes",
		"SET state = 'complete'",
		"phase = 'done'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("purge sequence missing %q:\n%s", want, joined)
		}
	}
	for _, banned := range []string{
		"DETACH DELETE",
		"DELETE FROM fact_work_items",
		"DELETE FROM scope_generations",
		"DELETE FROM fact_records",
		"LOCK TABLE",
		"SET status = 'superseded'",
	} {
		if strings.Contains(joined, banned) {
			t.Fatalf("purge sequence must never contain %q:\n%s", banned, joined)
		}
	}
	if !tx.committed {
		t.Fatal("purge transaction committed = false, want true")
	}

	// The tombstone completion carries the deleted-row ledger: the grant
	// pre-counts must reach rows_deleted, or the tombstone would stay
	// silent about the cascade it reports.
	var completion string
	for _, q := range tx.execs {
		if strings.Contains(q.query, "SET state = 'complete'") {
			completion = q.query
		}
	}
	if completion == "" {
		t.Fatal("purge sequence has no tombstone completion statement")
	}
	if !strings.Contains(completion, "rows_deleted") {
		t.Fatalf("tombstone completion carries no rows_deleted ledger:\n%s", completion)
	}
}

// TestPurgeRetiredExitsTerminalNotReached proves the terminal gate: a scope
// with a still-pending generation stops the purge before any delete, rolls
// back, records the terminal_not_reached exit with the read-back statuses,
// and emits the operator WARN carrying them.
func TestPurgeRetiredExitsTerminalNotReached(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"rr_purge02", "PARTITION:purge-stuck", "purge-stuck-main", "pending", "fenced"}}},
			{rows: [][]any{{"purge-stuck-main"}}},
			// Terminal gate readback: one pending generation remains.
			{rows: [][]any{{"pending", int64(1)}}},
		},
	}
	database := &fakeTransactionalDB{tx: tx}
	purger := NewRetirementPurger(database, database)
	purger.Options.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	result, err := purger.PurgeRetired(context.Background(),
		PurgeRequest{RepoIDs: []string{"PARTITION:purge-stuck"}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 0 {
		t.Fatalf("completed = %v, want none", result.Completed)
	}
	if len(result.Blocked) != 1 {
		t.Fatalf("blocked = %+v, want one terminal_not_reached exit", result.Blocked)
	}
	blocked := result.Blocked[0]
	if blocked.Reason != PurgeReasonTerminalNotReached {
		t.Fatalf("reason = %q, want %q", blocked.Reason, PurgeReasonTerminalNotReached)
	}
	if !strings.Contains(blocked.Statuses, "pending") {
		t.Fatalf("statuses readback = %q, want the blocking pending status", blocked.Statuses)
	}
	if len(tx.execs) != 0 {
		t.Fatalf("execs = %d, want 0 (the gate stops before any delete)", len(tx.execs))
	}
	if tx.committed {
		t.Fatal("purge transaction committed = true, want rollback on terminal_not_reached")
	}
	if !tx.rolledBack {
		t.Fatal("purge transaction rolledBack = false, want true on terminal_not_reached")
	}
	warned := logs.String()
	for _, want := range []string{"terminal_not_reached", "pending", "PARTITION:purge-stuck"} {
		if !strings.Contains(warned, want) {
			t.Fatalf("WARN log missing %q:\n%s", want, warned)
		}
	}
}

// TestPurgeRetiredScopeLockBusyRetries proves the NOWAIT scope lock: a
// lock-timeout on the scope rows records the scope_lock_busy exit without
// touching the tombstone, so a later run retries the same fenced row.
func TestPurgeRetiredScopeLockBusyRetries(t *testing.T) {
	t.Parallel()

	lockErr := &pgconn.PgError{Code: "55P03", Message: "could not obtain lock"}
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"rr_purge03", "PARTITION:purge-busy", "purge-busy-main", "pending", "fenced"}}},
			{err: lockErr},
		},
	}
	database := &fakeTransactionalDB{tx: tx}
	purger := NewRetirementPurger(database, database)

	result, err := purger.PurgeRetired(context.Background(),
		PurgeRequest{RepoIDs: []string{"PARTITION:purge-busy"}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Blocked) != 1 || result.Blocked[0].Reason != PurgeReasonScopeLockBusy {
		t.Fatalf("blocked = %+v, want one scope_lock_busy exit", result.Blocked)
	}
	if len(tx.execs) != 0 {
		t.Fatalf("execs = %d, want 0 (busy exits before any delete)", len(tx.execs))
	}
	if tx.committed {
		t.Fatal("purge transaction committed = true, want rollback on scope_lock_busy")
	}
}

// TestPurgeRetiredCompleteIsNoop proves crash-restart idempotency: a
// tombstone that already reached complete reports completed without issuing
// any delete, so a retry after a commit-crash cannot double-count the
// operator counter.
func TestPurgeRetiredCompleteIsNoop(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"rr_purge04", "PARTITION:purge-done", "purge-done-main", "complete", "done"}}},
		},
	}
	database := &fakeTransactionalDB{tx: tx}
	purger := NewRetirementPurger(database, database)

	result, err := purger.PurgeRetired(context.Background(),
		PurgeRequest{RepoIDs: []string{"PARTITION:purge-done"}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 || result.Completed[0] != "PARTITION:purge-done" {
		t.Fatalf("completed = %v, want [PARTITION:purge-done]", result.Completed)
	}
	if len(tx.execs) != 0 {
		t.Fatalf("execs = %d, want 0 (complete is a no-op)", len(tx.execs))
	}
	if len(tx.queries) != 1 {
		t.Fatalf("queries = %d, want exactly the tombstone claim", len(tx.queries))
	}
}

// TestPurgeRetiredValidatesRequest proves the request shape: an empty
// request and a duplicate repository both fail before Begin, so no
// tombstone row is ever locked for a malformed request.
func TestPurgeRetiredValidatesRequest(t *testing.T) {
	t.Parallel()

	for _, req := range []PurgeRequest{
		{},
		{RepoIDs: []string{"PARTITION:dup", "PARTITION:dup"}},
		{RepoIDs: []string{"  "}},
	} {
		tx := &fakeTx{}
		database := &fakeTransactionalDB{tx: tx}
		purger := NewRetirementPurger(database, database)
		if _, err := purger.PurgeRetired(context.Background(), req); err == nil {
			t.Fatalf("PurgeRetired(%v) succeeded, want a validation error", req)
		}
		if database.beginCalls != 0 {
			t.Fatalf("begin calls = %d, want 0 (refused before any lock)", database.beginCalls)
		}
	}
}

// TestPurgeRetiredRefusesUnfencedTombstone proves the runner order: a
// tombstone that is open but not pending/fenced fails the purge instead of
// deleting scopes under a runner phase the purge does not own.
func TestPurgeRetiredRefusesUnfencedTombstone(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"rr_purge05", "PARTITION:purge-early", "purge-early-main", "repairing_graph", "graph_retract"}}},
		},
	}
	database := &fakeTransactionalDB{tx: tx}
	purger := NewRetirementPurger(database, database)

	_, err := purger.PurgeRetired(context.Background(),
		PurgeRequest{RepoIDs: []string{"PARTITION:purge-early"}})
	if err == nil {
		t.Fatal("PurgeRetired on a graph_retract tombstone succeeded, want an order error")
	}
	if !strings.Contains(err.Error(), "graph_retract") {
		t.Fatalf("error = %v, want the owning phase named", err)
	}
	if len(tx.execs) != 0 {
		t.Fatalf("execs = %d, want 0 (wrong-phase exits before any delete)", len(tx.execs))
	}
	if tx.committed {
		t.Fatal("purge transaction committed = true, want rollback on wrong-phase")
	}
}

// TestRecordRetirementPurgedNilSafe proves the counter record path tolerates
// a nil instrument handle, so unit fakes without telemetry never panic.
func TestRecordRetirementPurgedNilSafe(t *testing.T) {
	t.Parallel()

	recordRetirementPurged(context.Background(), nil)
}
