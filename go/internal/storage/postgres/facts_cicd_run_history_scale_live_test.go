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

type timedCICDRunHistoryScaleLoader struct {
	cicdRunHistoryLiveLoader
	historyDuration time.Duration
}

func (l *timedCICDRunHistoryScaleLoader) ListCICDRunFactsForScopePatch(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
	providers []string,
	runIDs []string,
	runAttempts []string,
	artifactTombstoneKeys []string,
) ([]facts.Envelope, error) {
	started := time.Now()
	loaded, err := l.FactStore.ListCICDRunFactsForScopePatch(
		ctx,
		scopeID,
		targetGenerationID,
		providers,
		runIDs,
		runAttempts,
		artifactTombstoneKeys,
	)
	l.historyDuration = time.Since(started)
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
// handler, history read, decoder, 1,000-decision rebuild, and writer against a
// retained same-scope backlog. The patch contains 90 live artifacts and 90
// payload-empty tombstones whose latest payload-bearing identities predate an
// intervening tombstone, plus retained repository workflow-image evidence. No
// prior correlation decisions are seeded, so all 1,000 outputs must come from
// retained source facts. It is opt-in because seeding 216,000 step facts is too
// expensive for the normal focused package loop.
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
			"Handle() scale duration = %s, budget %s (history=%s writer=%s)",
			elapsed,
			cicdRunHistoryScaleBudget,
			store.historyDuration,
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
		"manifest scopes=1 generations=25 retained_step_rows=216000 live_artifact_keys=90 tombstone_keys=90 workflow_rows=90 prior_decisions=0 output_decisions=1000 duration=%s history=%s writer=%s budget=%s",
		elapsed,
		store.historyDuration,
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
FROM GENERATE_SERIES(1, 1000) AS run_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-workflow-image-%s', run_number),
    'scope-ci-scale',
    'perf-gen-01',
    'ci.workflow_image_evidence',
    FORMAT('perf-workflow-image-key-%s', run_number),
    '1.0.0',
    'git',
    'observed',
    'git',
    FORMAT('perf-workflow-image-key-%s', run_number),
    '2026-08-04T13:00:01Z',
    '2026-08-04T13:00:01Z',
    FALSE,
    JSONB_BUILD_OBJECT(
        'repository_id', 'repository:r_scale',
        'commit_sha', FORMAT('commit-%s', run_number),
        'workflow_path', FORMAT('.github/workflows/build-%s.yml', run_number),
        'command_kind', 'run',
        'evidence_class', 'workflow_image_unresolved',
        'reason', 'scale fixture intentionally has no static image ref'
    )
FROM GENERATE_SERIES(1, 90) AS run_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-retired-artifact-live-%s', run_number),
    'scope-ci-scale',
    'perf-gen-01',
    'ci.artifact',
    FORMAT('perf-retired-artifact-key-%s', run_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-retired-artifact-key-%s', run_number),
    '2026-08-04T13:00:01Z',
    '2026-08-04T13:00:01Z',
    FALSE,
    JSONB_BUILD_OBJECT(
        'provider', 'github_actions',
        'run_id', FORMAT('run-%s', run_number),
        'run_attempt', '1',
        'artifact_id', FORMAT('retired-artifact-%s', run_number),
        'artifact_type', 'container_image',
        'artifact_digest', 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
    )
FROM GENERATE_SERIES(91, 180) AS run_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-retired-artifact-prior-tombstone-%s', run_number),
    'scope-ci-scale',
    'perf-gen-24',
    'ci.artifact',
    FORMAT('perf-retired-artifact-key-%s', run_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-retired-artifact-key-%s', run_number),
    '2026-08-04T13:00:24Z',
    '2026-08-04T13:00:24Z',
    TRUE,
    '{}'::jsonb
FROM GENERATE_SERIES(91, 180) AS run_number;

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
FROM GENERATE_SERIES(1, 90) AS run_number;

INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
    FORMAT('perf-current-artifact-tombstone-%s', run_number),
    'scope-ci-scale',
    'perf-gen-25',
    'ci.artifact',
    FORMAT('perf-retired-artifact-key-%s', run_number),
    '1.0.0',
    'ci_cd_run',
    'reported',
    'ci_cd_run',
    FORMAT('perf-retired-artifact-key-%s', run_number),
    '2026-08-04T13:00:25Z',
    '2026-08-04T13:00:25Z',
    TRUE,
    '{}'::jsonb
FROM GENERATE_SERIES(91, 180) AS run_number;`)
	if err != nil {
		t.Fatalf("seed retained-history scale fixture: %v", err)
	}
}
