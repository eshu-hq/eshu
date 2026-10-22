// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// duplicateWriteMarkers are statement fragments that store a generation or its
// content. A commit of an unchanged reconciliation snapshot must issue none of
// them (#7978).
var duplicateWriteMarkers = []string{
	"INSERT INTO scope_generations",
	"INSERT INTO fact_records",
	"eshu_search_index_documents",
	"eshu_search_index_terms",
	"content_files",
	"INSERT INTO ingestion_scopes",
}

// recordedStatements returns every statement the fake database saw: queries
// on the base connection and every query and exec inside each transaction.
func recordedStatements(db *fakeTransactionalDB, txs ...*fakeTx) []string {
	var statements []string
	for _, query := range db.queries {
		statements = append(statements, query.query)
	}
	for _, tx := range txs {
		for _, query := range tx.queries {
			statements = append(statements, query.query)
		}
		for _, exec := range tx.execs {
			statements = append(statements, exec.query)
		}
	}
	return statements
}

func reprojectFixture(reason string, hint string, sha string) (scope.IngestionScope, scope.ScopeGeneration) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	return scope.IngestionScope{
			ScopeID: "scope-7978", SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: "repo-7978",
		}, scope.ScopeGeneration{
			GenerationID: "generation-new", ScopeID: "scope-7978",
			ObservedAt: now.Add(-time.Minute), IngestedAt: now,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
			FreshnessHint: hint, SourceCommitSHA: sha, ReconcileReason: reason,
		}
}

func threeFactStream() <-chan facts.Envelope {
	stream := make(chan facts.Envelope, 3)
	for _, id := range []string{"fact-1", "fact-2", "fact-3"} {
		stream <- facts.Envelope{
			FactID: id, ScopeID: "scope-7978", GenerationID: "generation-new",
			FactKind: "file", StableFactKey: "file:" + id,
			ObservedAt: time.Date(2026, time.October, 11, 11, 59, 0, 0, time.UTC),
		}
	}
	close(stream)
	return stream
}

// TestCommitReconcileUnchangedActiveReprojectsWithoutWrites is the #7978
// regression: a reconciliation snapshot whose hint and commit equal the
// active generation's stores no generation, fact, document, or term row, and
// re-opens exactly one projector work item for the active generation.
func TestCommitReconcileUnchangedActiveReprojectsWithoutWrites(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{true, true}}}}}
	db := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{{
			rows: [][]any{{"generation-active", "hint-same", "sha-same", "active", false, true}},
		}},
	}
	store := NewIngestionStore(db)
	scopeValue, generation := reprojectFixture("interval_elapsed", "hint-same", "sha-same")
	stream := threeFactStream()

	if err := store.CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	if got := len(stream); got != 0 {
		t.Fatalf("fact stream has %d undrained facts, want 0", got)
	}
	statements := recordedStatements(db, tx)
	for _, statement := range statements {
		for _, marker := range duplicateWriteMarkers {
			if strings.Contains(statement, marker) {
				t.Fatalf("unchanged reconcile issued a duplicate write %q:\n%s", marker, statement)
			}
		}
	}
	var reopens []fakeQueryCall
	for _, query := range tx.queries {
		if strings.Contains(query.query, "INSERT INTO fact_work_items") {
			reopens = append(reopens, query)
		}
	}
	if len(reopens) != 1 {
		t.Fatalf("projector re-open statements = %d, want 1; statements:\n%s", len(reopens), strings.Join(statements, "\n---\n"))
	}
	reopen := reopens[0]
	for _, want := range []string{
		"'projector'", "reconcile_requested_at", "reconcile_reason", "status = 'active'",
		"FROM reopened", "WHEN fact_work_items.status IN ('succeeded', 'failed', 'dead_letter') THEN 0", "reconcile_reopen_count",
	} {
		if !strings.Contains(reopen.query, want) {
			t.Fatalf("re-open statement missing %q:\n%s", want, reopen.query)
		}
	}
	if got := reopen.args[2]; got != "generation-active" {
		t.Fatalf("re-open generation arg = %v, want generation-active", got)
	}
	if !strings.Contains(tx.execs[0].query, "FROM ingestion_scopes") || !strings.Contains(tx.execs[0].query, "FOR NO KEY UPDATE") {
		t.Fatalf("first transaction statement = %q, want the scope row lock", tx.execs[0].query)
	}
	if !tx.committed {
		t.Fatal("re-open transaction committed = false, want true")
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonReconcileReprojected); got != 1 {
		t.Fatalf("reconcile_reprojected skips = %d, want 1", got)
	}
}

// TestCommitReconcileUnchangedActiveAlreadyOpenIsPending proves the re-open is
// idempotent: a second reconciliation while the active generation's projector
// work is pending records the request and stores nothing.
func TestCommitReconcileUnchangedActiveAlreadyOpenIsPending(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	tx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{true, false}}}}}
	db := &fakeTransactionalDB{tx: tx, queryResponses: []queueFakeRows{{
		rows: [][]any{{"generation-active", "hint-same", "sha-same", "active", false, true}},
	}}}
	scopeValue, generation := reprojectFixture("graph_dirty", "hint-same", "sha-same")
	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation, threeFactStream()); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonReconcileReprojectionPending); got != 1 {
		t.Fatalf("reconcile_reprojection_pending skips = %d, want 1", got)
	}
	for _, statement := range recordedStatements(db, tx) {
		if strings.Contains(statement, "INSERT INTO scope_generations") || strings.Contains(statement, "INSERT INTO fact_records") {
			t.Fatalf("pending re-open stored a duplicate:\n%s", statement)
		}
	}
}

// TestCommitUnchangedWithoutReconcileSkipsWithoutTransaction keeps the
// ordinary unchanged skip: no transaction and reason unchanged.
func TestCommitUnchangedWithoutReconcileSkipsWithoutTransaction(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	db := &fakeTransactionalDB{tx: &fakeTx{}, queryResponses: []queueFakeRows{{
		rows: [][]any{{"generation-active", "hint-same", "sha-same", "active", false, true}},
	}}}
	scopeValue, generation := reprojectFixture("", "hint-same", "sha-same")
	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation, threeFactStream()); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	if db.beginCalls != 0 {
		t.Fatalf("begin calls = %d, want 0", db.beginCalls)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonUnchanged); got != 1 {
		t.Fatalf("unchanged skips = %d, want 1", got)
	}
}

// TestCommitReconcileMatchingPendingGenerationSkips: a reconciliation that
// matches a generation still pending stores nothing and re-opens nothing; the
// pending generation's own projection is the full re-projection (ruling
// a9-120), counted as pending_same_identity.
func TestCommitReconcileMatchingPendingGenerationSkips(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	db := &fakeTransactionalDB{tx: &fakeTx{}, queryResponses: []queueFakeRows{{
		rows: [][]any{{"generation-pending", "hint-same", "sha-same", "pending", false, true}},
	}}}
	scopeValue, generation := reprojectFixture("interval_elapsed", "hint-same", "sha-same")
	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation, threeFactStream()); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	if db.beginCalls != 0 {
		t.Fatalf("begin calls = %d, want 0", db.beginCalls)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonPendingSameIdentity); got != 1 {
		t.Fatalf("pending_same_identity skips = %d, want 1", got)
	}
}

// TestCommitStoresGenerationWhenIdentityDiffers proves every case outside the
// duplicate definition still stores a generation: a changed hint (content or
// collector, parser, or schema version), a changed commit, and a
// default-branch change (#7678).
func TestCommitStoresGenerationWhenIdentityDiffers(t *testing.T) {
	cases := []struct {
		name                 string
		hint, sha, reason    string
		defaultBranchChanged bool
		wantCandidateQuery   bool
	}{
		{name: "changed hint", hint: "hint-new", sha: "sha-same", reason: "interval_elapsed", wantCandidateQuery: true},
		{name: "changed version", hint: "hint-v3-next", sha: "sha-same", reason: "interval_elapsed", wantCandidateQuery: true},
		{name: "changed commit", hint: "hint-same", sha: "sha-new", reason: "interval_elapsed", wantCandidateQuery: true},
		{name: "changed commit without reconcile", hint: "hint-same", sha: "sha-new", wantCandidateQuery: true},
		{name: "default branch changed", hint: "hint-same", sha: "sha-same", defaultBranchChanged: true},
		{name: "stale re-projection request", hint: "hint-same", sha: "sha-same", reason: scope.ReconcileReasonRequestStale, wantCandidateQuery: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			telemetry.ResetSkippedRefreshCountForTesting()
			t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)
			tx := &fakeTx{}
			db := &fakeTransactionalDB{tx: tx}
			if tc.wantCandidateQuery {
				db.queryResponses = []queueFakeRows{
					{rows: [][]any{{"generation-active", "hint-same", "sha-same", "active", false, true}}},
					// The delta-union base probe (#8189) CountFacts issues
					// before the count: not a delta here, so no union.
					{rows: [][]any{{false, ""}}},
					// The destructive-successor guard's prior fact count:
					// three streamed facts over three prior collector facts
					// stores (collector rows, then all rows; #8750).
					{rows: [][]any{{3, 3}}},
				}
			}
			scopeValue, generation := reprojectFixture(tc.reason, tc.hint, tc.sha)
			generation.DefaultBranchChanged = tc.defaultBranchChanged
			if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation, threeFactStream()); err != nil {
				t.Fatalf("CommitScopeGeneration() error = %v", err)
			}
			var inserted bool
			for _, exec := range tx.execs {
				if strings.Contains(exec.query, "INSERT INTO scope_generations") {
					inserted = true
				}
			}
			if !inserted {
				t.Fatalf("no scope_generations insert; statements:\n%s", strings.Join(recordedStatements(db, tx), "\n---\n"))
			}
			if got := telemetry.SkippedRefreshCount(); got != 0 {
				t.Fatalf("skipped refreshes = %d, want 0", got)
			}
			if tc.defaultBranchChanged && len(db.queries) != 0 {
				t.Fatalf("default-branch change ran %d candidate queries, want none", len(db.queries))
			}
		})
	}
}

// TestCommitReconcileFencedReopenStoresNothing proves the re-open is fenced
// on the generation still being active. When a newer generation activated
// between the check and the scope lock, the re-open matches nothing and its
// transaction rolls back. The snapshot is dropped, not stored: it describes an
// older commit than the generation that just activated, so storing it would
// regress the graph. The sweep decides the scope again next cycle.
func TestCommitReconcileFencedReopenStoresNothing(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	reopenTx := &fakeTx{queryResponses: []queueFakeRows{{rows: [][]any{{false, false}}}}}
	db := &fakeTransactionalDB{txs: []*fakeTx{reopenTx}, queryResponses: []queueFakeRows{{
		rows: [][]any{{"generation-active", "hint-same", "sha-same", "active", false, true}},
	}}}
	scopeValue, generation := reprojectFixture("interval_elapsed", "hint-same", "sha-same")
	stream := threeFactStream()
	if err := NewIngestionStore(db).CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v", err)
	}
	if !reopenTx.rolledBack || reopenTx.committed {
		t.Fatalf("re-open tx committed=%v rolledBack=%v, want rolled back", reopenTx.committed, reopenTx.rolledBack)
	}
	if db.beginCalls != 1 {
		t.Fatalf("begin calls = %d, want 1 (no second transaction stores the snapshot)", db.beginCalls)
	}
	if got := len(stream); got != 0 {
		t.Fatalf("fact stream has %d undrained facts, want 0", got)
	}
	for _, statement := range recordedStatements(db, reopenTx) {
		for _, marker := range duplicateWriteMarkers {
			if strings.Contains(statement, marker) {
				t.Fatalf("fenced reconcile issued a write %q:\n%s", marker, statement)
			}
		}
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonReconcileSuperseded); got != 1 {
		t.Fatalf("reconcile_superseded skips = %d, want 1", got)
	}
}
