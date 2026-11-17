// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// probeHubRows is the evidence rows of the one hub generation the probe seeds:
// a generation far larger than the 53 rows a batch of 50 was sized on. Before the
// row limit one batch held every row lock of it for the whole batch.
const probeHubRows = 120_000

// probeHubCandidates is the hub generation's candidate rows.
const probeHubCandidates = 2_000

// probeHubSliceRows is every row of the first 50-generation slice of the census
// list once the hub is seeded: the hub's evidence, candidate and generation rows,
// and the 49 ordinary orphans after it (40 evidence, 12 candidate and 1 generation
// row each). A pass with this row cap drains exactly that slice and stops.
const probeHubSliceRows = probeHubRows + probeHubCandidates + 1 + 49*(40+12+1)

// probeHubSeed adds one hub orphan generation of probeHubRows evidence rows and
// probeHubCandidates candidate rows. Its id sorts before every ordinary orphan, so
// the first slice of a pass holds it together with 49 ordinary generations.
func (f *liveFixture) probeHubSeed(ctx context.Context, t *testing.T) {
	t.Helper()
	f.probeExec(ctx, t, `INSERT INTO relationship_generations (generation_id, scope, run_id, status, created_at)
VALUES ('hub-0001', 'run-hub', 'run', 'superseded', now())`)
	f.probeExec(ctx, t, `INSERT INTO relationship_evidence_facts (evidence_id, generation_id, evidence_kind, relationship_type, source_repo_id, target_repo_id, source_entity_id, target_entity_id, confidence, rationale, details, observed_at)
SELECT 'hub-0001-ev-'||i, 'hub-0001', 'terraform', 'DEPENDS_ON', 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 0.9,
       'seed rationale text of moderate length for the row', jsonb_build_object('path', 'modules/x/main.tf', 'line', i), now()
FROM generate_series(1, $1::int) i`, probeHubRows)
	f.probeExec(ctx, t, `INSERT INTO relationship_candidates (candidate_id, generation_id, source_repo_id, target_repo_id, source_entity_id, target_entity_id, relationship_type, confidence, evidence_count, rationale, details)
SELECT 'hub-0001-c-'||i, 'hub-0001', 'repo-'||(i%17), 'repo-t', 'src-'||i, 'tgt-'||i, 'DEPENDS_ON', 0.8, 1, 'seed rationale text', '{}'::jsonb
FROM generate_series(1, $1::int) i`, probeHubCandidates)
	f.probeVacuum(ctx, t, "relationship_generations", "relationship_evidence_facts", "relationship_candidates")
}

// probeHubDrain runs one pass whose row cap ends it when the first slice, the hub
// and 49 ordinary generations, is drained, and logs how many batches that took,
// each batch's duration (sorted, so the last is the slowest) and the WAL of the
// whole drain. Each batch holds at most 2*BatchRows+BatchGenerations row locks.
func (f *liveFixture) probeHubDrain(ctx context.Context, t *testing.T) {
	t.Helper()
	var logs bytes.Buffer
	opts := relationshipretention.Options{
		Deletes: true, RowCap: probeHubSliceRows, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	before := f.probeWALNow(ctx, t)
	started := time.Now()
	result, err := relationshipretention.Sweep(ctx, f.database, opts)
	wall := time.Since(started)
	if err != nil {
		t.Fatalf("hub drain: %v", err)
	}
	walBytes := f.probeScalar(ctx, t, `SELECT pg_wal_lsn_diff(pg_current_wal_insert_lsn(), $1::pg_lsn)::bigint::text`, before.lsn)
	after := f.probeWALNow(ctx, t)
	var rows int64
	for _, n := range result.RowsSwept {
		rows += n
	}
	t.Logf("hub drain: %d rows, %d generations, %d batches, capped=%q, wall %s (census %s), skipped %v",
		rows, result.GenerationsSwept, result.Batches, result.Capped, wall.Round(time.Millisecond),
		result.Census.Duration.Round(time.Millisecond), result.Skipped)
	t.Logf("hub drain WAL: lsn diff %s bytes (%s per row), records +%d, fpi +%d; batch durations ms %v",
		walBytes, probeBytesPerRow(walBytes, rows), after.records-before.records, after.images-before.images,
		probeBatchDurations(logs.String()))
}

// probeHubBatch seeds the hub generation, logs the plans of the first bounded
// batch that holds it in batch order (rolled back), and then drains it for real
// right after a CHECKPOINT, the worst case for WAL.
func (f *liveFixture) probeHubBatch(ctx context.Context, t *testing.T) {
	t.Helper()
	f.probeHubSeed(ctx, t)
	census, err := relationshipretention.Sweep(ctx, f.database, relationshipretention.Options{})
	if err != nil {
		t.Fatalf("census before the hub batch: %v", err)
	}
	batch := census.Census.Generations[:relationshipretention.BatchGenerations]
	if batch[0] != "hub-0001" {
		t.Fatalf("the first batch starts at %q, want the hub generation", batch[0])
	}
	f.probeExplainBatch(ctx, t, []probeStep{
		{"hub batch classify", relationshipretention.ClassifySQL, []any{batch}},
		{"hub batch delete evidence", relationshipretention.DeleteEvidenceSQL, []any{batch, relationshipretention.BatchRows}},
		{"hub batch delete candidates", relationshipretention.DeleteCandidatesSQL, []any{batch, relationshipretention.BatchRows}},
		{"hub batch delete generations", relationshipretention.DeleteGenerationsSQL, []any{batch}},
		{"hub batch settle", relationshipretention.SettleSQL, []any{batch}},
	})
	f.probeExec(ctx, t, `CHECKPOINT`)
	f.probeHubDrain(ctx, t)
}
