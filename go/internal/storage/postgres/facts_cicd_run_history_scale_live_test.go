// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const cicdRunHistoryScaleBudget = 5 * time.Second

const cicdRunHistoryScopeRankTheoryQuery = `
WITH requested_run_keys AS MATERIALIZED (
    SELECT
        'github_actions'::text AS provider,
        FORMAT('run-%s', run_number)::text AS run_id,
        '1'::text AS run_attempt
    FROM GENERATE_SERIES(1, 90) AS run_number
),
ranked_run_facts AS MATERIALIZED (
    SELECT
        fact.fact_kind,
        fact.stable_fact_key,
        fact.is_tombstone,
        fact.payload,
        ROW_NUMBER() OVER (
            PARTITION BY fact.fact_kind, fact.stable_fact_key
            ORDER BY generation.ingested_at DESC,
                     generation.generation_id DESC,
                     fact.observed_at DESC,
                     fact.fact_id DESC
        ) AS fact_rank
    FROM fact_records AS fact
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.scope_id = 'scope-ci-scale'
      AND fact.fact_kind = ANY(ARRAY[
          'ci.run',
          'ci.artifact',
          'ci.environment_observation',
          'ci.trigger_edge',
          'ci.step'
      ]::text[])
      AND generation.status IN ('active', 'completed', 'superseded')
      AND (generation.ingested_at, generation.generation_id)
          < ('2026-08-04T13:00:25Z'::timestamptz, 'perf-gen-25')
)
SELECT COUNT(*)
FROM ranked_run_facts AS fact
WHERE fact.fact_rank = 1
  AND fact.is_tombstone = FALSE
  AND EXISTS (
      SELECT 1
      FROM requested_run_keys AS requested
      WHERE BTRIM(fact.payload->>'provider') = requested.provider
        AND BTRIM(fact.payload->>'run_id') = requested.run_id
        AND COALESCE(NULLIF(BTRIM(fact.payload->>'run_attempt'), ''), '1') = requested.run_attempt
  )`

type timedCICDRunHistoryScaleLoader struct {
	cicdRunHistoryLiveLoader
	historyDuration  time.Duration
	previousDuration time.Duration
}

func (l *timedCICDRunHistoryScaleLoader) ListCICDRunFactsForRunKeys(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
	providers []string,
	runIDs []string,
	runAttempts []string,
) ([]facts.Envelope, error) {
	started := time.Now()
	loaded, err := l.FactStore.ListCICDRunFactsForRunKeys(
		ctx,
		scopeID,
		targetGenerationID,
		providers,
		runIDs,
		runAttempts,
	)
	l.historyDuration = time.Since(started)
	return loaded, err
}

func (l *timedCICDRunHistoryScaleLoader) ListPreviousCICDRunCorrelationFacts(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
) ([]facts.Envelope, error) {
	started := time.Now()
	loaded, err := l.FactStore.ListPreviousCICDRunCorrelationFacts(ctx, scopeID, targetGenerationID)
	l.previousDuration = time.Since(started)
	return loaded, err
}

type timedCICDRunHistoryScaleWriter struct {
	writer   reducer.PostgresCICDRunCorrelationWriter
	duration time.Duration
}

func (w *timedCICDRunHistoryScaleWriter) WriteCICDRunCorrelations(
	ctx context.Context,
	write reducer.CICDRunCorrelationWrite,
) (reducer.CICDRunCorrelationWriteResult, error) {
	started := time.Now()
	result, err := w.writer.WriteCICDRunCorrelations(ctx, write)
	w.duration = time.Since(started)
	return result, err
}

// TestCICDRunCorrelationArtifactPatchAtRetainedHistoryScale runs the shipped
// handler, history reads, decoder, 1,000-decision merge, and writer against a
// retained same-scope backlog. It is opt-in because seeding 216,000 step facts
// is too expensive for the normal focused package loop.
func TestCICDRunCorrelationArtifactPatchAtRetainedHistoryScale(t *testing.T) {
	if os.Getenv("ESHU_CICD_RUN_HISTORY_SCALE_PROOF") != "1" {
		t.Skip("set ESHU_CICD_RUN_HISTORY_SCALE_PROOF=1 for the retained-history performance proof")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN for the retained-history performance proof")
	}

	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunHistoryScaleFixture(t, ctx, db)
	if os.Getenv("ESHU_CICD_RUN_HISTORY_SCOPE_RANK_THEORY") == "1" {
		started := time.Now()
		var selectedFacts int
		if err := db.QueryRowContext(ctx, cicdRunHistoryScopeRankTheoryQuery).Scan(&selectedFacts); err != nil {
			t.Fatalf("run scope-rank theory query: %v", err)
		}
		elapsed := time.Since(started)
		if selectedFacts != 9_090 {
			t.Fatalf("scope-rank theory selected facts = %d, want 9090", selectedFacts)
		}
		if elapsed > cicdRunHistoryScaleBudget {
			t.Fatalf("scope-rank theory duration = %s, budget %s", elapsed, cicdRunHistoryScaleBudget)
		}
		t.Logf("scope-rank theory selected_facts=9090 duration=%s budget=%s", elapsed, cicdRunHistoryScaleBudget)
		return
	}
	store := &timedCICDRunHistoryScaleLoader{
		cicdRunHistoryLiveLoader: cicdRunHistoryLiveLoader{FactStore: NewFactStore(SQLDB{DB: db})},
	}
	writer := &timedCICDRunHistoryScaleWriter{writer: reducer.PostgresCICDRunCorrelationWriter{
		DB: SQLDB{DB: db},
		Now: func() time.Time {
			return time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
		},
	}}
	handler := reducer.CICDRunCorrelationHandler{
		FactLoader: store,
		Writer:     writer,
	}

	started := time.Now()
	_, err := handler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-artifact-patch-scale",
		ScopeID:      "scope-ci-scale",
		GenerationID: "perf-gen-25",
		SourceSystem: "ci_cd_run",
		Domain:       reducer.DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Handle() scale error = %v", err)
	}
	if elapsed > cicdRunHistoryScaleBudget {
		t.Fatalf(
			"Handle() scale duration = %s, budget %s (history=%s previous=%s writer=%s)",
			elapsed,
			cicdRunHistoryScaleBudget,
			store.historyDuration,
			store.previousDuration,
			writer.duration,
		)
	}

	var factsWritten int
	var distinctFactIDs int
	var patchedArtifacts int
	err = db.QueryRowContext(ctx, `
SELECT
    COUNT(*),
    COUNT(DISTINCT fact_id),
    COUNT(*) FILTER (WHERE COALESCE(payload->>'artifact_digest', '') <> '')
FROM fact_records
WHERE scope_id = 'scope-ci-scale'
  AND generation_id = 'perf-gen-25'
  AND fact_kind = 'reducer_ci_cd_run_correlation'`).Scan(
		&factsWritten,
		&distinctFactIDs,
		&patchedArtifacts,
	)
	if err != nil {
		t.Fatalf("count scale correlations: %v", err)
	}
	if factsWritten != 1_000 || distinctFactIDs != 1_000 || patchedArtifacts != 90 {
		t.Fatalf(
			"scale correlations = rows:%d ids:%d patched:%d, want 1000/1000/90",
			factsWritten,
			distinctFactIDs,
			patchedArtifacts,
		)
	}
	t.Logf(
		"manifest scopes=1 generations=25 retained_step_rows=216000 patch_keys=90 prior_decisions=1000 output_decisions=1000 duration=%s history=%s previous=%s writer=%s budget=%s",
		elapsed,
		store.historyDuration,
		store.previousDuration,
		writer.duration,
		cicdRunHistoryScaleBudget,
	)
}

func seedCICDRunHistoryScaleFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-ci-scale', 'ci_cd_run', 'ci_cd_run', 'synthetic/scale', 'ci_cd_run',
    'synthetic/scale', '2026-08-04T13:00:00Z', '2026-08-04T13:00:00Z', 'active', 'perf-gen-25'
);

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
)
SELECT
    FORMAT('perf-gen-%s', LPAD(number::text, 2, '0')),
    'scope-ci-scale',
    'snapshot',
    '2026-08-04T13:00:00Z'::timestamptz + number * interval '1 second',
    '2026-08-04T13:00:00Z'::timestamptz + number * interval '1 second',
    'superseded'
FROM GENERATE_SERIES(1, 24) AS number;

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES (
    'perf-gen-25', 'scope-ci-scale', 'snapshot',
    '2026-08-04T13:00:25Z', '2026-08-04T13:00:25Z', 'active'
);

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-run-%s', run_number),
    'scope-ci-scale',
    'perf-gen-01',
    'ci.run',
    FORMAT('perf-run-key-%s', run_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-run-key-%s', run_number),
    '2026-08-04T13:00:01Z',
    '2026-08-04T13:00:01Z',
    FALSE,
    JSONB_BUILD_OBJECT(
        'provider', 'github_actions',
        'run_id', FORMAT('run-%s', run_number),
        'run_attempt', '1',
        'repository_id', 'repository:r_scale',
        'commit_sha', FORMAT('commit-%s', run_number)
    )
FROM GENERATE_SERIES(1, 90) AS run_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-step-g%s-r%s-s%s', generation_number, run_number, step_number),
    'scope-ci-scale',
    FORMAT('perf-gen-%s', LPAD(generation_number::text, 2, '0')),
    'ci.step',
    FORMAT('perf-step-key-r%s-s%s', run_number, step_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-step-key-r%s-s%s', run_number, step_number),
    '2026-08-04T13:00:00Z'::timestamptz + generation_number * interval '1 second',
    '2026-08-04T13:00:00Z'::timestamptz + generation_number * interval '1 second',
    FALSE,
    JSONB_BUILD_OBJECT(
        'provider', 'github_actions',
        'run_id', FORMAT('run-%s', run_number),
        'run_attempt', '1',
        'step_number', step_number::text
    )
FROM GENERATE_SERIES(1, 24) AS generation_number
CROSS JOIN GENERATE_SERIES(1, 90) AS run_number
CROSS JOIN GENERATE_SERIES(1, 100) AS step_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-prior-correlation-%s', run_number),
    'scope-ci-scale',
    'perf-gen-24',
    'reducer_ci_cd_run_correlation',
    FORMAT('perf-prior-correlation-key-%s', run_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-prior-correlation-key-%s', run_number),
    '2026-08-04T13:00:24Z',
    '2026-08-04T13:00:24Z',
    FALSE,
    JSONB_BUILD_OBJECT(
        'provider', 'github_actions',
        'run_id', FORMAT('run-%s', run_number),
        'run_attempt', '1',
        'repository_id', 'repository:r_scale',
        'commit_sha', FORMAT('commit-%s', run_number),
        'outcome', 'derived',
        'reason', 'prior retained decision',
        'provenance_only', TRUE,
        'canonical_writes', 0,
        'evidence_fact_ids', JSONB_BUILD_ARRAY(FORMAT('perf-run-%s', run_number))
    )
FROM GENERATE_SERIES(1, 1000) AS run_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-current-artifact-%s', run_number),
    'scope-ci-scale',
    'perf-gen-25',
    'ci.artifact',
    FORMAT('perf-current-artifact-key-%s', run_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-current-artifact-key-%s', run_number),
    '2026-08-04T13:00:25Z',
    '2026-08-04T13:00:25Z',
    FALSE,
    JSONB_BUILD_OBJECT(
        'provider', 'github_actions',
        'run_id', FORMAT('run-%s', run_number),
        'run_attempt', '1',
        'artifact_id', FORMAT('artifact-%s', run_number),
        'artifact_type', 'container_image',
        'artifact_digest', 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
    )
FROM GENERATE_SERIES(1, 90) AS run_number;`)
	if err != nil {
		t.Fatalf("seed retained-history scale fixture: %v", err)
	}
}
