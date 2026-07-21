// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// This file is the Postgres-write half of the issue #5445 finding 1
// before/after evidence table. go/internal/relationships (which owns the
// in-memory DiscoverEvidenceWithStats cost proof,
// structured_evidence_streaming_volume_bench_test.go) has no dependency on
// this package (storage/postgres), by the service-boundary ownership table
// in docs/internal/agent-guide.md, so it cannot exercise
// RelationshipStore.UpsertEvidenceFacts directly. This file measures the
// write side from here instead.
//
// It is a MODELED cost, not a live-Postgres wall-clock number: latencyExecQueryer
// stands in for a real connection with a fixed per-statement round-trip
// latency, the same technique BenchmarkDeferredBackfillSerial
// (ingestion_backfill_bench_test.go) already uses to characterize the #3704
// batched-write path without requiring a live database in this proof. The
// real question this file answers is structural and IS answered exactly by
// this technique: before issue #5445, the streaming path never called
// UpsertEvidenceFacts with these 8 buckets' rows at all (their discovery
// always returned zero evidence -- see
// TestStreamingEvidenceVolume_RepresentativeRepo in
// go/internal/relationships), so the marginal Postgres write cost was
// exactly zero additional ExecContext calls. After the fix, the marginal
// cost is bounded by ceil(newEvidenceCount/evidenceInsertBatchRows)
// additional multi-row ExecContext calls per commit -- never one call per
// row, because UpsertEvidenceFacts already batches (issue #3704, pre-existing
// infrastructure this change does not touch). A live-Postgres wall-clock
// confirmation of the per-statement latency constant used below is the
// natural pre-merge follow-up and is called out as not run in this proof.
type latencyExecQueryer struct {
	stmtLatency time.Duration
	execCount   int
}

func (db *latencyExecQueryer) QueryContext(context.Context, string, ...any) (Rows, error) {
	time.Sleep(db.stmtLatency)
	return &queueFakeRows{}, nil
}

func (db *latencyExecQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	time.Sleep(db.stmtLatency)
	db.execCount++
	return fakeResult{}, nil
}

// representativeStreamingCommitEvidenceCount is the NEW evidence-fact count
// TestStreamingEvidenceVolume_RepresentativeRepo
// (go/internal/relationships/structured_evidence_streaming_volume_bench_test.go)
// measured on its 24-file representative platform/gitops repo fixture: OLD
// (pre-#5445, real-parser streaming shape) = 0, NEW = 28. Duplicated here as
// a literal (not imported -- storage/postgres must not import relationships'
// test-only fixture builder) so this benchmark's modeled write cost uses the
// SAME measured volume rather than an invented one.
const representativeStreamingCommitEvidenceCount = 28

func representativeStreamingCommitEvidence(n int) []relationships.EvidenceFact {
	facts := make([]relationships.EvidenceFact, 0, n)
	for i := 0; i < n; i++ {
		facts = append(facts, relationships.EvidenceFact{
			EvidenceKind:     relationships.EvidenceKind("terraform_module"),
			RelationshipType: relationships.RelationshipType("depends_on"),
			SourceRepoID:     "repo-platform",
			TargetRepoID:     "repo-target-" + itoa(i),
			Confidence:       0.9,
			Rationale:        "module reference",
		})
	}
	return facts
}

// BenchmarkUpsertEvidenceFactsStreamingDelta_RepresentativeCommit reports the
// modeled per-commit Postgres write cost BEFORE (0 rows, issue #5445's
// finding-1 bug: these 8 buckets never produced evidence on the streaming
// path) and AFTER (representativeStreamingCommitEvidenceCount rows, the
// measured real-parser volume) this fix, using a 1ms simulated per-statement
// round-trip latency (a conservative same-region Postgres estimate; the repo's
// own #3704 evidence used 50us for a tighter same-host bound, see
// ingestion_backfill_bench_test.go's latencyBackfillDB).
func BenchmarkUpsertEvidenceFactsStreamingDelta_RepresentativeCommit(b *testing.B) {
	const simulatedRoundTripLatency = time.Millisecond

	b.Run("Before_ZeroEvidenceRows", func(b *testing.B) {
		facts := representativeStreamingCommitEvidence(0)
		fake := &latencyExecQueryer{stmtLatency: simulatedRoundTripLatency}
		store := NewRelationshipStore(fake)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := store.UpsertEvidenceFacts(context.Background(), "gen-1", facts); err != nil {
				b.Fatalf("UpsertEvidenceFacts() error = %v, want nil", err)
			}
		}
		b.ReportMetric(float64(fake.execCount)/float64(max(b.N, 1)), "execs/commit")
	})

	b.Run("After_RepresentativeEvidenceRows", func(b *testing.B) {
		facts := representativeStreamingCommitEvidence(representativeStreamingCommitEvidenceCount)
		fake := &latencyExecQueryer{stmtLatency: simulatedRoundTripLatency}
		store := NewRelationshipStore(fake)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := store.UpsertEvidenceFacts(context.Background(), "gen-1", facts); err != nil {
				b.Fatalf("UpsertEvidenceFacts() error = %v, want nil", err)
			}
		}
		b.ReportMetric(float64(fake.execCount)/float64(max(b.N, 1)), "execs/commit")
	})
}
