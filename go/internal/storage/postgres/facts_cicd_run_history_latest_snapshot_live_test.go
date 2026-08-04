// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCICDRunCorrelationArtifactPatchUsesLatestRunSnapshot(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the latest CI/CD run snapshot proof")
	}
	tests := []struct {
		name             string
		currentRunID     string
		wantPublishedIDs []string
	}{
		{
			name:             "patch latest snapshot run",
			currentRunID:     "run-b",
			wantPublishedIDs: []string{"run-b"},
		},
		{
			name:             "patch exact older run",
			currentRunID:     "run-a",
			wantPublishedIDs: []string{"run-a", "run-b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runCICDRunLatestSnapshotPatchProof(t, dsn, test.currentRunID)
			if !slices.Equal(got, test.wantPublishedIDs) {
				t.Fatalf("patched run IDs = %#v, want %#v", got, test.wantPublishedIDs)
			}
		})
	}
}

func TestCICDRunCorrelationArtifactPatchDoesNotResurrectOmittedRunEvidence(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the latest CI/CD run evidence proof")
	}
	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunSnapshotGenerations(t, ctx, db)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-1", "gen-1", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "environment-b-gen-1", "gen-1", "ci.environment_observation", "environment-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","environment":"production","source":"deploy_event"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-2", "gen-2", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb222","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-b-gen-3", "gen-3", "ci.artifact", "artifact-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","artifact_id":"artifact-b","artifact_type":"container_image","artifact_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
	runCICDRunSnapshotPatchHandler(t, ctx, db, "intent-latest-run-evidence")

	var environment string
	var hasRetainedEnvironment bool
	if err := db.QueryRowContext(ctx, `
SELECT
    COALESCE(payload->>'environment', ''),
    COALESCE(payload->'evidence_fact_ids', '[]'::jsonb) ? 'environment-b-gen-1'
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
  AND payload->>'run_id' = 'run-b'`).Scan(&environment, &hasRetainedEnvironment); err != nil {
		t.Fatalf("query patched run evidence: %v", err)
	}
	if environment != "" || hasRetainedEnvironment {
		t.Fatalf("patched run environment = %q retained_evidence=%v, want latest snapshot omission preserved", environment, hasRetainedEnvironment)
	}
}

func TestCICDRunCorrelationArtifactTombstoneDoesNotResurrectOmittedRun(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the omitted CI/CD run tombstone proof")
	}
	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunSnapshotGenerations(t, ctx, db)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-gen-1", "gen-1", "ci.run", "run-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"repo-api","commit_sha":"aaa111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-a-gen-1", "gen-1", "ci.artifact", "artifact-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","artifact_id":"artifact-a","artifact_type":"container_image","artifact_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-1", "gen-1", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-2", "gen-2", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb222","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-a-gen-3", "gen-3", "ci.artifact", "artifact-a-key", true, `{}`)
	runCICDRunSnapshotPatchHandler(t, ctx, db, "intent-omitted-run-tombstone")

	rows, err := db.QueryContext(ctx, `
SELECT payload->>'run_id'
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
ORDER BY payload->>'run_id'`)
	if err != nil {
		t.Fatalf("query tombstone patched snapshot: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			t.Fatalf("scan tombstone patched run ID: %v", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tombstone patched run IDs: %v", err)
	}
	if want := []string{"run-b"}; !slices.Equal(runIDs, want) {
		t.Fatalf("tombstone patched run IDs = %#v, want %#v", runIDs, want)
	}
}

func seedCICDRunSnapshotGenerations(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-ci', 'ci_cd_run', 'ci_cd_run', 'acme/api', 'ci_cd_run',
    'acme/api', '2026-08-04T12:00:00Z', '2026-08-04T12:00:00Z', 'active', 'gen-3'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES
    ('gen-1', 'scope-ci', 'snapshot', '2026-08-04T12:01:00Z', '2026-08-04T12:01:00Z', 'superseded'),
    ('gen-2', 'scope-ci', 'snapshot', '2026-08-04T12:02:00Z', '2026-08-04T12:02:00Z', 'superseded'),
    ('gen-3', 'scope-ci', 'snapshot', '2026-08-04T12:03:00Z', '2026-08-04T12:03:00Z', 'active');`); err != nil {
		t.Fatalf("seed CI/CD snapshot generations: %v", err)
	}
}

func runCICDRunSnapshotPatchHandler(t *testing.T, ctx context.Context, db *sql.DB, intentID string) {
	t.Helper()
	store := cicdRunHistoryLiveLoader{FactStore: NewFactStore(SQLDB{DB: db})}
	handler := reducer.CICDRunCorrelationHandler{
		FactLoader: store,
		Writer: reducer.PostgresCICDRunCorrelationWriter{
			DB: SQLDB{DB: db},
			Now: func() time.Time {
				return time.Date(2026, time.August, 4, 12, 10, 0, 0, time.UTC)
			},
		},
	}
	if _, err := handler.Handle(ctx, reducer.Intent{
		IntentID:     intentID,
		ScopeID:      "scope-ci",
		GenerationID: "gen-3",
		SourceSystem: "ci_cd_run",
		Domain:       reducer.DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
}

func runCICDRunLatestSnapshotPatchProof(t *testing.T, dsn, currentRunID string) []string {
	t.Helper()
	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES (
    'scope-ci', 'ci_cd_run', 'ci_cd_run', 'acme/api', 'ci_cd_run',
    'acme/api', '2026-08-04T12:00:00Z', '2026-08-04T12:00:00Z', 'active', 'gen-3'
);
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES
    ('gen-1', 'scope-ci', 'snapshot', '2026-08-04T12:01:00Z', '2026-08-04T12:01:00Z', 'superseded'),
    ('gen-2', 'scope-ci', 'snapshot', '2026-08-04T12:02:00Z', '2026-08-04T12:02:00Z', 'superseded'),
    ('gen-3', 'scope-ci', 'snapshot', '2026-08-04T12:03:00Z', '2026-08-04T12:03:00Z', 'active');`); err != nil {
		t.Fatalf("seed CI/CD snapshot generations: %v", err)
	}
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-gen-1", "gen-1", "ci.run", "run-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"repo-api","commit_sha":"aaa111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-1", "gen-1", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-2", "gen-2", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb222","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(
		t,
		ctx,
		db,
		"artifact-"+currentRunID+"-gen-3",
		"gen-3",
		"ci.artifact",
		"artifact-"+currentRunID+"-key",
		false,
		fmt.Sprintf(
			`{"provider":"github_actions","run_id":%q,"run_attempt":"1","artifact_id":%q,"artifact_type":"container_image","artifact_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`,
			currentRunID,
			"artifact-"+currentRunID,
		),
	)

	store := cicdRunHistoryLiveLoader{FactStore: NewFactStore(SQLDB{DB: db})}
	handler := reducer.CICDRunCorrelationHandler{
		FactLoader: store,
		Writer: reducer.PostgresCICDRunCorrelationWriter{
			DB: SQLDB{DB: db},
			Now: func() time.Time {
				return time.Date(2026, time.August, 4, 12, 10, 0, 0, time.UTC)
			},
		},
	}
	if _, err := handler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-latest-run-snapshot",
		ScopeID:      "scope-ci",
		GenerationID: "gen-3",
		SourceSystem: "ci_cd_run",
		Domain:       reducer.DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	rows, err := db.QueryContext(ctx, `
SELECT payload->>'run_id'
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
ORDER BY payload->>'run_id'`)
	if err != nil {
		t.Fatalf("query patched run snapshot: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			t.Fatalf("scan patched run ID: %v", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate patched run IDs: %v", err)
	}
	return runIDs
}
