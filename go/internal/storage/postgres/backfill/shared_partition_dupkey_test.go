// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

// Shared-partition duplicate-conflict-key proof for the deferred-backfill
// dup-key bug found by a live Ifá gate run.
//
// The original defect: writeDeferredBackfillBatch (ingestion_backfill.go)
// looped per repository and appended one reducer.GraphProjectionPhaseState and
// one ScopeGenerationPartition memo candidate per repo.
// graph_projection_phase_state's conflict target (scope_id,
// acceptance_unit_id, source_run_id, generation_id, keyspace, phase) and
// deferred_backfill_partition_memo's conflict target (scope_id, generation_id)
// are both functions of (ScopeID, GenerationID) alone -- the per-repo
// phase/memo rows built from repoGeneration carry AcceptanceUnitID = ScopeID
// and SourceRunID = GenerationID, and a constant Keyspace/Phase. When N
// repositories in one batch shared a (scope, generation) partition (the
// ingestion commit path accepts multi-repo scopes; production git sync just
// happens to commit one repo per scope), the batch upsert carried N
// byte-identical conflict keys in one INSERT ... ON CONFLICT DO UPDATE and
// Postgres rejected the whole batch transaction with SQLSTATE 21000 ("ON
// CONFLICT DO UPDATE command cannot affect row a second time"). The rollback
// meant NO repository in the batch got its readiness published, including
// well-formed ones sharing the batch with no other bug of their own.
//
// Publication has since moved out of the batch into the per-partition fan-in
// (PublishDeferredBackfillPartitions), which iterates partitions and therefore
// cannot construct a duplicate conflict key at all. These tests keep the
// original guarantee pinned at its new home: they drive the real batched entry
// point end to end and assert that repositories sharing a (scope, generation)
// produce exactly ONE phase row and ONE memo row for that partition, while
// repositories in DISTINCT partitions still get one row each -- the collapse
// must not over-reach.

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestDeferredBackfillPublishesOneRowPerPartitionHermetic is the
// DSN-independent sibling of the live proofs below. The live tests are the real
// end-to-end proof (they alone run against Postgres conflict-key semantics),
// but they self-skip whenever ESHU_POSTGRES_DSN is unset, and CI is not
// guaranteed to set it -- a DSN-gated-only regression test is a guard that
// exists on paper and never runs in that case. This test needs no database: it
// drives the real batched entry point against the package's existing
// concurrencyProbeDB fake (ingestion_backfill_concurrency_test.go), which
// records every ExecContext call verbatim instead of enforcing Postgres
// conflict-key semantics, and asserts on the CONSTRUCTED row count across the
// publication upserts -- the exact quantity the partition keying controls -- so
// it has teeth without needing a live ON CONFLICT rejection to prove it.
//
// The repositories are deliberately spread across several batches, because
// after the fan-in change that is the shape the guarantee has to survive: a
// partition's rows now come from a step that runs after every batch, not from
// whichever batch happened to see the partition first.
func TestDeferredBackfillPublishesOneRowPerPartitionHermetic(t *testing.T) {
	t.Parallel()

	// repo-shared-a and repo-shared-b share one (scope, generation) partition
	// (scope-shared, gen-shared) -- the multi-repo scope the ingestion commit
	// path accepts and the original dup-key defect choked on. repo-other and
	// repo-solo own their own partitions, so the collapse must not reach them.
	// 4 repos, 3 distinct partitions.
	activeGen := [][]any{
		{"repo-shared-a", "scope-shared", "gen-shared"},
		{"repo-shared-b", "scope-shared", "gen-shared"},
		{"repo-other", "scope-other", "gen-other"},
		{"repo-solo", "scope-solo", "gen-solo"},
	}
	db := &concurrencyProbeDB{activeGenRows: activeGen}
	store, _, phases := testStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	// One repository per batch, so repo-shared-a and repo-shared-b reach the
	// fan-in from two SEPARATE committed batches.
	store.BatchSize = 1
	store.Workers = 1

	published, _, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"fingerprint-hermetic",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil", err)
	}
	if published != 3 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d, want 3 (one per distinct (scope, generation) partition, not one per repo and not one per scope)", published)
	}

	partitions := phases.publishedPartitions()
	wantPartitions := map[[2]string]struct{}{
		{"scope-shared", "gen-shared"}: {},
		{"scope-other", "gen-other"}:   {},
		{"scope-solo", "gen-solo"}:     {},
	}
	if !reflect.DeepEqual(partitions, wantPartitions) {
		t.Fatalf("published partitions = %v, want %v", partitions, wantPartitions)
	}
	memoRows := publishedUpsertRowCount(t, db.allEvidence, upsertDeferredBackfillPartitionMemoBatchPrefix, deferredBackfillPartitionMemoColumnsPerRow, 3)
	if memoRows != 3 {
		t.Fatalf("deferred_backfill_partition_memo upserts carried %d row(s) in total, want 3", memoRows)
	}
}

// TestDeferredBackfillWithholdsPublicationForSupersededGenerationHermetic pins
// the GenerationID axis of the partition key. A partition key that carried only
// ScopeID would treat a batch's (scope-shared, gen-stale) contribution as the
// same partition as (scope-shared, gen-shared) and publish it. The real key
// carries the generation, and the fan-in's under-lock re-read then finds that
// the scope's active generation is gen-shared, not gen-stale, and withholds
// publication for the superseded partition while still publishing the live one.
//
// Note the shape this test can and cannot model: a scope has exactly ONE active
// generation, so "the same scope active at two generations at once" is not
// representable in Postgres. The reachable form of the axis is a batch that
// observed a generation which has since been superseded, which is what the fake
// reproduces by answering the fan-in's per-scope lookup with gen-shared.
func TestDeferredBackfillWithholdsPublicationForSupersededGenerationHermetic(t *testing.T) {
	t.Parallel()

	// The fake answers LoadActiveGenerationForScope from the FIRST row matching
	// the scope, so scope-shared resolves to gen-shared: repo-stale's batch
	// contribution under gen-stale is a superseded partition.
	activeGen := [][]any{
		{"repo-live", "scope-shared", "gen-shared"},
		{"repo-stale", "scope-shared", "gen-stale"},
	}
	db := &concurrencyProbeDB{activeGenRows: activeGen}
	store, _, phases := testStore(db)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.BatchSize = 1
	store.Workers = 1

	published, _, err := store.writeDeferredBackfillInBatches(
		context.Background(),
		map[string][]relationships.EvidenceFact{},
		nil,
		"fingerprint-superseded",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("writeDeferredBackfillInBatches() error = %v, want nil (a superseded generation is a skip, not a failure)", err)
	}
	if published != 1 {
		t.Fatalf("writeDeferredBackfillInBatches() published = %d, want 1 (only the live generation)", published)
	}

	partitions := phases.publishedPartitions()
	wantPartitions := map[[2]string]struct{}{
		{"scope-shared", "gen-shared"}: {},
	}
	if !reflect.DeepEqual(partitions, wantPartitions) {
		t.Fatalf("published partitions = %v, want %v (no publication for the superseded generation)", partitions, wantPartitions)
	}
	memoRows := publishedUpsertRowCount(t, db.allEvidence, upsertDeferredBackfillPartitionMemoBatchPrefix, deferredBackfillPartitionMemoColumnsPerRow, 1)
	if memoRows != 1 {
		t.Fatalf("deferred_backfill_partition_memo upserts carried %d row(s), want 1; a memo for the superseded generation would suppress its reload", memoRows)
	}
}

// publishedUpsertRowCount sums the rows carried by every recorded ExecContext
// call whose query starts with prefix (len(args) / columnsPerRow each), and
// pins the number of calls at wantCalls. Both halves matter: the row total is
// the dedupe assertion, and the call count pins that publication happens once
// per partition rather than once per repository or once per batch, so a
// regression that publishes the right rows from the wrong place still fails.
func publishedUpsertRowCount(t *testing.T, calls []fakeExecCall, prefix string, columnsPerRow, wantCalls int) int {
	t.Helper()
	var matched []fakeExecCall
	for _, call := range calls {
		if strings.HasPrefix(call.query, prefix) {
			matched = append(matched, call)
		}
	}
	if len(matched) != wantCalls {
		t.Fatalf("recorded %d ExecContext call(s) matching prefix %q, want exactly %d", len(matched), prefix, wantCalls)
	}
	total := 0
	for _, call := range matched {
		if columnsPerRow <= 0 || len(call.args)%columnsPerRow != 0 {
			t.Fatalf("upsert args len = %d not divisible by columnsPerRow = %d", len(call.args), columnsPerRow)
		}
		total += len(call.args) / columnsPerRow
	}
	return total
}
