// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const documentationWriteProofSetupSQL = `
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;
CREATE TABLE fact_records (
  fact_id TEXT PRIMARY KEY,
  scope_id TEXT NOT NULL,
  generation_id TEXT NOT NULL,
  fact_kind TEXT NOT NULL,
  stable_fact_key TEXT NOT NULL,
  schema_version TEXT NOT NULL DEFAULT '0.0.0',
  collector_kind TEXT NOT NULL DEFAULT 'unknown',
  fencing_token BIGINT NOT NULL DEFAULT 0,
  source_confidence TEXT NOT NULL DEFAULT 'unknown',
  source_system TEXT NOT NULL,
  source_fact_key TEXT NOT NULL,
  source_uri TEXT NULL,
  source_record_id TEXT NULL,
  observed_at TIMESTAMPTZ NOT NULL,
  ingested_at TIMESTAMPTZ NOT NULL,
  is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX fact_records_scope_generation_idx
  ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);
CREATE INDEX fact_records_documentation_sources_observed_idx
  ON fact_records (observed_at DESC, fact_id DESC)
  WHERE fact_kind = 'documentation_source' AND is_tombstone = FALSE;
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'doc:' || n, 'scope:largest-search-proof', 'generation:search-proof',
  (ARRAY[
    'documentation_source', 'documentation_document', 'documentation_section',
    'documentation_link', 'documentation_entity_mention',
    'documentation_claim_candidate', 'semantic.documentation_observation'
  ])[1 + (n % 7)],
  'doc:' || n, '1.0.0', 'proof', 0, 'high', 'proof', 'doc:' || n,
  timestamptz '2026-02-01 00:00:00+00' + n * interval '1 millisecond',
  timestamptz '2026-02-01 00:00:00+00' + n * interval '1 millisecond',
  jsonb_build_object(
    'display_name', 'Document ' || n,
    'title', CASE WHEN n % 1000 = 0 THEN 'needle title ' || n ELSE 'ordinary title ' || n END,
    'heading_text', 'Heading ' || n % 100,
    'content', 'documentation content ' || n || ' ' || repeat(md5(n::text) || ' ', 8),
    'target_uri', 'https://example.invalid/docs/' || n
  )
FROM generate_series(1, 1600000) AS n;
ANALYZE fact_records;
`

const documentationWriteProofBroadGINSQL = `
CREATE INDEX fact_records_documentation_facts_search_trgm_candidate
ON fact_records USING GIN ((
  LOWER(
    COALESCE(payload->>'display_name', '') || ' ' ||
    COALESCE(payload->>'title', '') || ' ' ||
    COALESCE(payload->>'heading_text', '') || ' ' ||
    COALESCE(payload->>'content', '') || ' ' ||
    COALESCE(payload->>'target_uri', '')
  )
) gin_trgm_ops)
WHERE fact_kind IN (
  'documentation_source', 'documentation_document', 'documentation_section',
  'documentation_link', 'documentation_entity_mention',
  'documentation_claim_candidate', 'semantic.documentation_observation'
) AND is_tombstone = FALSE;
ANALYZE fact_records;
`

const documentationWriteProofScopedGINSQL = `
CREATE INDEX fact_records_documentation_facts_scope_search_gin_candidate
ON fact_records USING GIN (
  scope_id, fact_kind,
  (LOWER(
    COALESCE(payload->>'display_name', '') || ' ' ||
    COALESCE(payload->>'title', '') || ' ' ||
    COALESCE(payload->>'heading_text', '') || ' ' ||
    COALESCE(payload->>'content', '') || ' ' ||
    COALESCE(payload->>'target_uri', '')
  )) gin_trgm_ops
)
WHERE fact_kind IN (
  'documentation_source', 'documentation_document', 'documentation_section',
  'documentation_link', 'documentation_entity_mention',
  'documentation_claim_candidate', 'semantic.documentation_observation'
) AND is_tombstone = FALSE;
ANALYZE fact_records;
`

type documentationWriteProofSample struct {
	ExecutionMS  float64
	PlanningMS   float64
	SharedHits   int64
	SharedReads  int64
	SharedDirty  int64
	SharedWrites int64
}

type documentationWriteProofPlanNode struct {
	SharedHits   int64 `json:"Shared Hit Blocks"`
	SharedReads  int64 `json:"Shared Read Blocks"`
	SharedDirty  int64 `json:"Shared Dirtied Blocks"`
	SharedWrites int64 `json:"Shared Written Blocks"`
}

func TestDocumentationFactsSearchIndexWriteTaxLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute,
	)
	if _, err := db.ExecContext(ctx, documentationWriteProofSetupSQL); err != nil {
		t.Fatalf("create and seed documentation write proof: %v", err)
	}

	baseline := measureDocumentationWriteProof(t, ctx, db, "baseline")
	if _, err := db.ExecContext(ctx, documentationWriteProofBroadGINSQL); err != nil {
		t.Fatalf("create broad documentation search GIN: %v", err)
	}
	broad := measureDocumentationWriteProof(t, ctx, db, "broad_gin")
	if _, err := db.ExecContext(ctx, "DROP INDEX fact_records_documentation_facts_search_trgm_candidate"); err != nil {
		t.Fatalf("drop broad documentation search GIN: %v", err)
	}
	if _, err := db.ExecContext(ctx, documentationWriteProofScopedGINSQL); err != nil {
		t.Fatalf("create scoped documentation search GIN: %v", err)
	}
	scoped := measureDocumentationWriteProof(t, ctx, db, "scoped_gin")

	logDocumentationWriteProof(t, "baseline", baseline)
	logDocumentationWriteProof(t, "broad_gin", broad)
	logDocumentationWriteProof(t, "scoped_gin", scoped)
	baselineMedian := medianDocumentationWriteProof(baseline).ExecutionMS
	for name, samples := range map[string][]documentationWriteProofSample{
		"broad_gin":  broad,
		"scoped_gin": scoped,
	} {
		ratio := medianDocumentationWriteProof(samples).ExecutionMS / baselineMedian
		if ratio <= 1.5 {
			t.Fatalf("%s median write ratio = %.2fx, want a measured regression above 1.50x before rejecting it", name, ratio)
		}
		t.Logf("WRITE_PROOF_RATIO candidate=%s median_ratio=%.3fx", name, ratio)
	}
}

func TestDocumentationFindingIndexWriteTaxLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	if _, err := db.ExecContext(ctx, documentationAggregateWriteProofSetupSQL); err != nil {
		t.Fatalf("create aggregate write proof schema: %v", err)
	}
	batch := documentationFindingWriteProofBatchForTest("aggregate-write:")
	oldOnly := measureDocumentationWriteProofBatch(t, ctx, db, batch, "old_only")
	if _, err := db.ExecContext(ctx, documentationAggregateWriteProofListIndexesSQL); err != nil {
		t.Fatalf("create unfiltered and selective list indexes: %v", err)
	}
	triple := measureDocumentationWriteProofBatch(t, ctx, db, batch, "triple_final")
	oldMedian := medianDocumentationWriteProof(oldOnly).ExecutionMS
	tripleMedian := medianDocumentationWriteProof(triple).ExecutionMS
	ratio := tripleMedian / oldMedian
	logDocumentationWriteProof(t, "old_only", oldOnly)
	logDocumentationWriteProof(t, "triple_final", triple)
	t.Logf("WRITE_PROOF_RATIO candidate=triple_final median_ratio=%.3fx", ratio)
	if ratio > 1.5 {
		t.Fatalf("triple final median write ratio = %.3fx, want <= 1.50x", ratio)
	}
}

const documentationAggregateWriteProofSetupSQL = `
CREATE TABLE fact_records (
  fact_id TEXT PRIMARY KEY, scope_id TEXT NOT NULL, generation_id TEXT NOT NULL,
  fact_kind TEXT NOT NULL, stable_fact_key TEXT NOT NULL,
  schema_version TEXT NOT NULL DEFAULT '0.0.0', collector_kind TEXT NOT NULL,
  fencing_token BIGINT NOT NULL DEFAULT 0, source_confidence TEXT NOT NULL,
  source_system TEXT NOT NULL, source_fact_key TEXT NOT NULL, source_uri TEXT,
  source_record_id TEXT, observed_at TIMESTAMPTZ NOT NULL, ingested_at TIMESTAMPTZ NOT NULL,
  is_tombstone BOOLEAN NOT NULL DEFAULT FALSE, payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX fact_records_documentation_findings_visible_idx ON fact_records
((payload->>'finding_type'), (payload->>'source_id'), (payload->>'document_id'),
 (payload->>'status'), (payload->>'truth_level'), (payload->>'freshness_state'),
 observed_at DESC, fact_id DESC)
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied';
`

const documentationAggregateWriteProofListIndexesSQL = `
CREATE INDEX fact_records_documentation_findings_read_idx ON fact_records
 (observed_at DESC, fact_id DESC)
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE;
CREATE INDEX fact_records_documentation_findings_filter_idx ON fact_records
((payload->>'finding_type'), (payload->>'source_id'), (payload->>'document_id'),
 (payload->>'status'), (payload->>'truth_level'), (payload->>'freshness_state'),
 observed_at DESC, fact_id DESC)
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE;
`

func documentationWriteProofBatchForTest(prefix string) []facts.Envelope {
	observedAt := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	batch := make([]facts.Envelope, 0, factBatchSize)
	for row := 1; row <= factBatchSize; row++ {
		rowID := prefix + strconv.Itoa(row)
		batch = append(batch, facts.Envelope{
			FactID:           rowID,
			ScopeID:          "scope:largest-search-proof",
			GenerationID:     "generation:search-proof",
			FactKind:         "documentation_document",
			StableFactKey:    rowID,
			SchemaVersion:    "1.0.0",
			CollectorKind:    "proof",
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt.Add(time.Duration(row) * time.Millisecond),
			Payload: map[string]any{
				"display_name": "Write " + strconv.Itoa(row),
				"title":        "Write title " + strconv.Itoa(row),
				"heading_text": "Write heading",
				"content":      strings.Repeat(fmt.Sprintf("%032x ", row), 8),
				"target_uri":   "https://example.invalid/write/" + strconv.Itoa(row),
			},
			SourceRef: facts.Ref{
				SourceSystem: "proof",
				ScopeID:      "scope:largest-search-proof",
				GenerationID: "generation:search-proof",
				FactKey:      rowID,
			},
		})
	}
	return batch
}

func documentationFindingWriteProofBatchForTest(prefix string) []facts.Envelope {
	batch := documentationWriteProofBatchForTest(prefix)
	for index := range batch {
		batch[index].FactKind = "documentation_finding"
		batch[index].Payload = map[string]any{
			"finding_type":    "documentation_drift",
			"source_id":       "source:aggregate-write",
			"document_id":     batch[index].FactID,
			"status":          "open",
			"truth_level":     "observed",
			"freshness_state": "fresh",
			"permissions": map[string]any{
				"viewer_can_read_source": true,
				"source_acl_evaluated":   true,
			},
			"states": map[string]any{"permission_decision": "allowed"},
		}
	}
	return batch
}

func measureDocumentationWriteProof(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	name string,
) []documentationWriteProofSample {
	t.Helper()
	runDocumentationWriteWarmup(t, ctx, db, name+":warmup:")
	samples := make([]documentationWriteProofSample, 0, 3)
	for sample := 1; sample <= 3; sample++ {
		prefix := fmt.Sprintf("%s:sample:%d:", name, sample)
		samples = append(samples, explainDocumentationWriteBatch(t, ctx, db, prefix))
	}
	return samples
}

func measureDocumentationWriteProofBatch(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	batch []facts.Envelope,
	name string,
) []documentationWriteProofSample {
	t.Helper()
	samples := make([]documentationWriteProofSample, 0, 3)
	for sample := 1; sample <= 3; sample++ {
		query, args, err := buildDocumentationStreamingWriteProofQuery(batch)
		if err != nil {
			t.Fatalf("build %s production write batch: %v", name, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin %s production write batch: %v", name, err)
		}
		var raw []byte
		err = tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw)
		if rollbackErr := tx.Rollback(); rollbackErr != nil && err == nil {
			t.Fatalf("rollback %s production write batch: %v", name, rollbackErr)
		}
		if err != nil {
			t.Fatalf("explain %s production write batch: %v", name, err)
		}
		samples = append(samples, decodeDocumentationWriteProofSample(t, raw))
	}
	return samples
}

func buildDocumentationStreamingWriteProofQuery(batch []facts.Envelope) (string, []any, error) {
	return buildUpsertFactBatchQuery(upsertFactBatchSuffixReturningFactID, batch)
}

func runDocumentationWriteWarmup(t *testing.T, ctx context.Context, db *sql.DB, prefix string) {
	t.Helper()
	batch := documentationWriteProofBatchForTest(prefix)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin documentation write warmup: %v", err)
	}
	accepted, err := upsertFactBatchReturningAccepted(ctx, SQLTx{Tx: tx}, batch)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("run production documentation write warmup: %v", err)
	}
	if len(accepted) != len(batch) {
		_ = tx.Rollback()
		t.Fatalf("production documentation write warmup accepted %d facts, want %d", len(accepted), len(batch))
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback documentation write warmup: %v", err)
	}
}

func explainDocumentationWriteBatch(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	prefix string,
) documentationWriteProofSample {
	t.Helper()
	query, args, err := buildDocumentationStreamingWriteProofQuery(
		documentationWriteProofBatchForTest(prefix),
	)
	if err != nil {
		t.Fatalf("build production documentation write batch: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin documentation write proof: %v", err)
	}
	var raw []byte
	if err := tx.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, SUMMARY, FORMAT JSON) "+query,
		args...,
	).Scan(&raw); err != nil {
		_ = tx.Rollback()
		t.Fatalf("explain production documentation write batch: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback documentation write proof: %v", err)
	}
	return decodeDocumentationWriteProofSample(t, raw)
}

func decodeDocumentationWriteProofSample(t *testing.T, raw []byte) documentationWriteProofSample {
	t.Helper()
	var plans []struct {
		Plan          documentationWriteProofPlanNode `json:"Plan"`
		PlanningTime  float64                         `json:"Planning Time"`
		ExecutionTime float64                         `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode documentation write plan: err=%v raw=%s", err, raw)
	}
	return documentationWriteProofSample{
		ExecutionMS:  plans[0].ExecutionTime,
		PlanningMS:   plans[0].PlanningTime,
		SharedHits:   plans[0].Plan.SharedHits,
		SharedReads:  plans[0].Plan.SharedReads,
		SharedDirty:  plans[0].Plan.SharedDirty,
		SharedWrites: plans[0].Plan.SharedWrites,
	}
}

func medianDocumentationWriteProof(samples []documentationWriteProofSample) documentationWriteProofSample {
	ordered := append([]documentationWriteProofSample(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ExecutionMS < ordered[j].ExecutionMS
	})
	return ordered[len(ordered)/2]
}

func logDocumentationWriteProof(t *testing.T, name string, samples []documentationWriteProofSample) {
	t.Helper()
	for index, sample := range samples {
		t.Logf(
			"WRITE_PROOF_SAMPLE candidate=%s sample=%d rows=%d execution_ms=%.3f planning_ms=%.3f shared_hits=%d shared_reads=%d shared_dirtied=%d shared_written=%d",
			name,
			index+1,
			factBatchSize,
			sample.ExecutionMS,
			sample.PlanningMS,
			sample.SharedHits,
			sample.SharedReads,
			sample.SharedDirty,
			sample.SharedWrites,
		)
	}
}
