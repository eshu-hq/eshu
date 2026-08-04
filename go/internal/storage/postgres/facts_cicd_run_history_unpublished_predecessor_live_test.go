// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCICDRunCorrelationArtifactPatchRebuildsUnpublishedPredecessor proves an
// artifact-only generation does not depend on the preceding reducer work item
// having published its correlation snapshot. Generation 1 has two runs but no
// reducer_ci_cd_run_correlation facts; generation 2 patches only run A. The
// active generation must still publish both A and unaffected B.
func TestCICDRunCorrelationArtifactPatchRebuildsUnpublishedPredecessor(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unpublished-predecessor proof")
	}

	ctx := context.Background()
	db := openUnpublishedCICDRunPredecessorSchema(t, ctx, dsn)
	seedUnpublishedCICDRunPredecessor(t, ctx, db)
	queueNow := time.Date(2026, time.August, 4, 14, 3, 0, 0, time.UTC)
	queue := NewReducerQueue(SQLDB{DB: db}, "unpublished-predecessor-proof", time.Minute)
	queue.Now = func() time.Time { return queueNow }
	for _, intent := range []projector.ReducerIntent{
		{
			ScopeID:      "scope-ci-unpublished",
			GenerationID: "gen-1",
			Domain:       reducer.DomainCICDRunCorrelation,
			Reason:       "generation 1 runs observed",
			SourceSystem: "ci_cd_run",
		},
		{
			ScopeID:      "scope-ci-unpublished",
			GenerationID: "gen-2",
			Domain:       reducer.DomainCICDRunCorrelation,
			Reason:       "generation 2 artifact observed",
			SourceSystem: "ci_cd_run",
		},
	} {
		if _, err := queue.Enqueue(ctx, []projector.ReducerIntent{intent}); err != nil {
			t.Fatalf("enqueue %s correlation work: %v", intent.GenerationID, err)
		}
		queueNow = queueNow.Add(time.Second)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim active artifact generation: %v", err)
	}
	if !ok || claimed.GenerationID != "gen-2" {
		t.Fatalf("claimed intent = %#v, want active gen-2 after gen-1 supersession", claimed)
	}
	var predecessorStatus string
	if err := db.QueryRowContext(ctx, `
SELECT status
FROM fact_work_items
WHERE scope_id = 'scope-ci-unpublished'
  AND generation_id = 'gen-1'
  AND domain = 'ci_cd_run_correlation'`).Scan(&predecessorStatus); err != nil {
		t.Fatalf("read predecessor work status: %v", err)
	}
	if predecessorStatus != "superseded" {
		t.Fatalf("predecessor work status = %q, want superseded production shape", predecessorStatus)
	}
	store := cicdRunHistoryLiveLoader{FactStore: NewFactStore(SQLDB{DB: db})}
	handler := reducer.CICDRunCorrelationHandler{
		FactLoader: store,
		Writer: reducer.PostgresCICDRunCorrelationWriter{
			DB: SQLDB{DB: db},
			Now: func() time.Time {
				return time.Date(2026, time.August, 4, 15, 0, 0, 0, time.UTC)
			},
		},
	}

	_, err = handler.Handle(ctx, claimed)
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	rows, err := db.QueryContext(ctx, `
SELECT payload->>'run_id'
FROM fact_records
WHERE scope_id = 'scope-ci-unpublished'
  AND generation_id = 'gen-2'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
ORDER BY payload->>'run_id'`)
	if err != nil {
		t.Fatalf("query rebuilt correlations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			t.Fatalf("scan rebuilt correlation: %v", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rebuilt correlations: %v", err)
	}
	if got, want := runIDs, []string{"run-a", "run-b"}; !equalCICDRunIDs(got, want) {
		t.Fatalf("active generation run IDs = %#v, want %#v", got, want)
	}
}

func openUnpublishedCICDRunPredecessorSchema(
	t *testing.T,
	ctx context.Context,
	dsn string,
) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("cicd_unpublished_predecessor_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create unpublished-predecessor schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set unpublished-predecessor search_path: %v", err)
	}
	for _, statement := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("scope_generations"),
		MigrationSQL("fact_records"),
		MigrationSQL("fact_work_items"),
		reducerClaimCapabilityColumnsSchemaSQL,
		MigrationSQL("reducer_work_item_reopened_at"),
		graphProjectionPhaseStateSchemaSQL,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply unpublished-predecessor schema: %v", err)
		}
	}
	return db
}

func seedUnpublishedCICDRunPredecessor(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-ci-unpublished', 'ci_cd_run', 'ci_cd_run', 'acme/api', 'ci_cd_run',
    'acme/api', '2026-08-04T14:00:00Z', '2026-08-04T14:00:00Z', 'active', 'gen-2'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES
    ('gen-1', 'scope-ci-unpublished', 'snapshot', '2026-08-04T14:01:00Z', '2026-08-04T14:01:00Z', 'superseded'),
    ('gen-2', 'scope-ci-unpublished', 'snapshot', '2026-08-04T14:02:00Z', '2026-08-04T14:02:00Z', 'active');`)
	if err != nil {
		t.Fatalf("seed unpublished-predecessor generations: %v", err)
	}
	insertUnpublishedCICDRunFact(t, ctx, db, "run-a-gen-1", "gen-1", "ci.run", "run-a-key",
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"repo-api","commit_sha":"aaa111","status":"completed","result":"success"}`)
	insertUnpublishedCICDRunFact(t, ctx, db, "run-b-gen-1", "gen-1", "ci.run", "run-b-key",
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb222","status":"completed","result":"success"}`)
	insertUnpublishedCICDRunFact(t, ctx, db, "artifact-a-gen-2", "gen-2", "ci.artifact", "artifact-a-key",
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","artifact_id":"artifact-a","artifact_type":"archive"}`)
}

func insertUnpublishedCICDRunFact(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID string,
	generationID string,
	factKind string,
	stableFactKey string,
	payload string,
) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES (
    $1, 'scope-ci-unpublished', $2, $3, $4,
    '1.0.0', 'ci_cd_run', 'reported', 'ci_cd_run',
    $4, '2026-08-04T14:05:00Z', '2026-08-04T14:05:00Z', FALSE, $5::jsonb
)`, factID, generationID, factKind, stableFactKey, payload)
	if err != nil {
		t.Fatalf("insert unpublished-predecessor fact %q: %v", factID, err)
	}
}

func equalCICDRunIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
