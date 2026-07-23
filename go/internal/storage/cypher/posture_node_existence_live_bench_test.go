// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

// postureExistenceBenchPrefix namespaces every node this file creates so
// cleanup can target exactly its own rows and never touch a concurrent
// worktree's fixtures on a shared NornicDB container.
const postureExistenceBenchPrefix = "5652-existence-bench-"

// postureExistenceBenchUIDs returns n candidate uid strings as []any, the
// same shape filterRowsToExistingCloudResourceUIDs sends as
// $candidate_uids.
func postureExistenceBenchUIDs(prefix string, n int) []any {
	uids := make([]any, 0, n)
	for i := 0; i < n; i++ {
		uids = append(uids, fmt.Sprintf("%s%s-%d", postureExistenceBenchPrefix, prefix, i))
	}
	return uids
}

// postureExistenceBenchSeed MERGEs one CloudResource node per uid in uids so
// the existence read has real matches to find, mirroring the shipped
// writers' contract that a candidate uid only reaches the MERGE-anchored
// write after this read confirms it. Seeding uses a single batched MERGE,
// not b.N round trips, so seed cost never pollutes the timed loop.
func postureExistenceBenchSeed(ctx context.Context, tb testing.TB, runner *boltRetractTestRunner, uids []any) {
	tb.Helper()
	rows := make([]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, map[string]any{"uid": uid})
	}
	if err := boltWriteStatement(ctx, runner,
		`UNWIND $rows AS row MERGE (r:CloudResource {uid: row.uid})`,
		map[string]any{"rows": rows}); err != nil {
		tb.Fatalf("seed CloudResource nodes: %v", err)
	}
}

// postureExistenceBenchCleanup deletes exactly the uids this run created, in
// chunks of at most DefaultBatchSize. A single unchunked
// `MATCH (r:CloudResource) WHERE r.uid STARTS WITH ... DETACH DELETE r`
// against thousands of nodes reliably hit
// Neo.TransientError.Transaction.Outdated on this backend (measured: the
// N2000Stress and N500LargeStore cases below both failed cleanup this way,
// exhausting the Bolt driver's 30s retry budget) -- an unanchored,
// non-uid-keyed bulk delete is exactly the write shape
// docs/public/reference/nornicdb-pitfalls.md warns is conflict-prone at
// scale. Deleting by explicit uid list, chunked at the SAME bound production
// write batches use, avoids it; this is cleanup-only and has no bearing on
// the read latency this file measures.
func postureExistenceBenchCleanup(tb testing.TB, runner *boltRetractTestRunner, uidSets ...[]any) {
	tb.Helper()
	ctx := context.Background()
	for _, uids := range uidSets {
		for start := 0; start < len(uids); start += DefaultBatchSize {
			end := start + DefaultBatchSize
			if end > len(uids) {
				end = len(uids)
			}
			if err := boltWriteStatement(ctx, runner,
				`UNWIND $uids AS uid MATCH (r:CloudResource {uid: uid}) DETACH DELETE r`,
				map[string]any{"uids": uids[start:end]}); err != nil {
				tb.Errorf("cleanup live posture-existence bench nodes [%d:%d]: %v", start, end, err)
			}
		}
	}
}

// runPostureExistenceReaderBench times filterRowsToExistingCloudResourceUIDs
// -- the exact production read function four posture writers now call once
// per write, inside the graphowner.LockOnlyGate advisory-lock hold (see
// cmd/reducer/canonical_graph_writers.go, internal/graphowner/lock_only_gate.go)
// -- against candidateCount rows, all pre-seeded as existing CloudResource
// nodes, over the real Bolt driver.
func runPostureExistenceReaderBench(b *testing.B, candidateCount int) {
	runner := openBoltTestRunner(b)
	b.Cleanup(func() { runner.close(context.Background()) })

	uids := postureExistenceBenchUIDs(fmt.Sprintf("n%d", candidateCount), candidateCount)
	b.Cleanup(func() { postureExistenceBenchCleanup(b, runner, uids) })
	postureExistenceBenchSeed(context.Background(), b, runner, uids)

	rows := make([]map[string]any, 0, candidateCount)
	for _, uid := range uids {
		rows = append(rows, map[string]any{"uid": uid.(string)})
	}
	reader := &boltPostureExistenceReader{runner: runner}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filtered, err := filterRowsToExistingCloudResourceUIDs(ctx, reader, rows)
		if err != nil {
			b.Fatalf("filterRowsToExistingCloudResourceUIDs: %v", err)
		}
		if len(filtered) != len(rows) {
			b.Fatalf("filtered = %d rows, want %d (all pre-seeded as existing)", len(filtered), len(rows))
		}
	}
}

// BenchmarkPostureExistenceReaderLive_N10 measures the added existence-read
// cost for a small candidate batch (well under the 500-row shipped batch
// size), the common case for a reducer generation touching only a handful of
// posture-eligible resources.
func BenchmarkPostureExistenceReaderLive_N10(b *testing.B) {
	runPostureExistenceReaderBench(b, 10)
}

// BenchmarkPostureExistenceReaderLive_N500ShippedBatchSize measures the read
// at exactly cypher.DefaultBatchSize (500) -- the SAME value
// graphowner.lockChunkSize uses to bound advisory-lock chunk size
// (internal/graphowner/gated_writer.go), so this is the worst-case number of
// rows the read runs over inside a single Postgres advisory-lock hold. This
// is the number the #5652 lock-hold impact assessment is based on.
func BenchmarkPostureExistenceReaderLive_N500ShippedBatchSize(b *testing.B) {
	runPostureExistenceReaderBench(b, DefaultBatchSize)
}

// BenchmarkPostureExistenceReaderLive_N2000Stress measures the read at 4x the
// shipped batch size as a stress upper bound -- production writers never
// call filterRowsToExistingCloudResourceUIDs with more than DefaultBatchSize
// rows in one call (see writeChunk/lockChunkSize), so this case is not a
// shipped shape; it exists to show how the read's cost scales with N.
func BenchmarkPostureExistenceReaderLive_N2000Stress(b *testing.B) {
	runPostureExistenceReaderBench(b, 2000)
}

// BenchmarkPostureExistenceReaderLive_N500LargeStore re-runs the shipped
// 500-candidate case with a large unrelated CloudResource population (5,000
// distractor nodes seeded once, outside the timed loop) already resident in
// the graph -- 10x the total CloudResource population of
// BenchmarkPostureExistenceReaderLive_N500ShippedBatchSize.
// filterRowsToExistingCloudResourceUIDs's read
// (postureCloudResourceExistingUIDsCypher) is anchored on
// `MATCH (resource:CloudResource {uid: candidate_uid})`, a property-map match
// on the indexed/uniquely-constrained CloudResource.uid property
// (go/internal/graph.SchemaStatementsForBackend(SchemaBackendNornicDB) emits
// both `cloud_resource_uid_unique` and `nornicdb_cloud_resource_uid_lookup`).
// An index-seek anchor's cost is a function of the candidate count, not total
// graph size; comparing this benchmark's ns/op against
// BenchmarkPostureExistenceReaderLive_N500ShippedBatchSize (same candidate
// count, 10x fewer total CloudResource nodes in the store) is the empirical
// index-seek-vs-full-scan proof this package's live benchmarks use in place
// of a Bolt PROFILE plan, because the pinned NornicDB v1.1.11 Bolt transport
// does not return PROFILE metadata (confirmed: PROFILE/EXPLAIN produce a nil
// ProfiledPlan over Bolt on this backend; the SAME limitation is already
// documented for the #5410 SQL-relationships live benchmark, see
// docs/internal/evidence/5410-sql-relationships-performance.md). A materially
// higher ns/op here than the small-store benchmark would indicate the read is
// NOT index-backed (a full :CloudResource label scan); a comparable ns/op
// confirms it is.
func BenchmarkPostureExistenceReaderLive_N500LargeStore(b *testing.B) {
	runner := openBoltTestRunner(b)
	b.Cleanup(func() { runner.close(context.Background()) })

	const distractorCount = 5000
	distractorUIDs := postureExistenceBenchUIDs("distractor", distractorCount)
	uids := postureExistenceBenchUIDs("n500-largestore", DefaultBatchSize)
	b.Cleanup(func() { postureExistenceBenchCleanup(b, runner, distractorUIDs, uids) })

	postureExistenceBenchSeed(context.Background(), b, runner, distractorUIDs)
	postureExistenceBenchSeed(context.Background(), b, runner, uids)

	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, map[string]any{"uid": uid.(string)})
	}
	reader := &boltPostureExistenceReader{runner: runner}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filtered, err := filterRowsToExistingCloudResourceUIDs(ctx, reader, rows)
		if err != nil {
			b.Fatalf("filterRowsToExistingCloudResourceUIDs: %v", err)
		}
		if len(filtered) != len(rows) {
			b.Fatalf("filtered = %d rows, want %d (all pre-seeded as existing)", len(filtered), len(rows))
		}
	}
}

// TestLivePostureExistenceReaderDedupesDuplicateCandidateUIDs is the live
// read-back proof for dropping `RETURN DISTINCT` from
// postureCloudResourceExistingUIDsCypher. Without DISTINCT, a candidate_uid
// value repeated in $candidate_uids matches the SAME CloudResource node once
// per UNWIND repetition and so can produce more than one existing_uid row for
// that uid; filterRowsToExistingCloudResourceUIDs is expected to fold that
// into a single Go-side set membership check regardless, so the caller-visible
// filtered result is unaffected. This drives the real Bolt driver against the
// real (post-removal) query text, not a mock, so it would catch a regression
// where duplicate rows corrupted the returned existing set instead of just
// duplicating harmlessly.
func TestLivePostureExistenceReaderDedupesDuplicateCandidateUIDs(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	uids := postureExistenceBenchUIDs("distinct-removed", 1)
	existingUID := uids[0].(string)
	missingUID := postureExistenceBenchPrefix + "distinct-removed-missing"
	t.Cleanup(func() { postureExistenceBenchCleanup(t, runner, uids) })
	postureExistenceBenchSeed(ctx, t, runner, uids)

	reader := &boltPostureExistenceReader{runner: runner}

	// Two rows carry the SAME existing uid (the shape that used to rely on
	// RETURN DISTINCT to collapse in Cypher) plus one row for a uid that was
	// never seeded.
	rows := []map[string]any{
		{"uid": existingUID, "slot": "first"},
		{"uid": existingUID, "slot": "second"},
		{"uid": missingUID, "slot": "third"},
	}

	filtered, err := filterRowsToExistingCloudResourceUIDs(ctx, reader, rows)
	if err != nil {
		t.Fatalf("filterRowsToExistingCloudResourceUIDs: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered = %d rows, want 2 (both duplicate rows for the confirmed-existing uid, "+
			"missing uid dropped) -- removing RETURN DISTINCT must not corrupt the confirmed-uid set: %v",
			len(filtered), filtered)
	}
	for _, row := range filtered {
		if row["uid"] != existingUID {
			t.Fatalf("filtered row uid = %v, want %q (the confirmed-existing uid); missing uid must not "+
				"survive the filter", row["uid"], existingUID)
		}
	}

	// Direct read-back of the reader itself: with DISTINCT removed, the raw
	// query is allowed to return one row per UNWIND repetition, but every
	// returned existing_uid must still be exactly the confirmed uid -- no
	// duplicate-uid input may leak a row for the missing uid.
	records, err := reader.Run(ctx, postureCloudResourceExistingUIDsCypher, map[string]any{
		"candidate_uids": []any{existingUID, existingUID, missingUID},
	})
	if err != nil {
		t.Fatalf("reader.Run: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("reader.Run returned 0 rows, want at least 1 (the confirmed-existing uid, possibly duplicated)")
	}
	for _, record := range records {
		if record["existing_uid"] != existingUID {
			t.Fatalf("record existing_uid = %v, want %q", record["existing_uid"], existingUID)
		}
	}
}
