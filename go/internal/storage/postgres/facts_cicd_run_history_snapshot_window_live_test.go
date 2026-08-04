// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
)

func TestCICDRunCorrelationLiveArtifactRecoversOnlyOmittedRunAnchor(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the omitted CI/CD run anchor proof")
	}
	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunSnapshotGenerations(t, ctx, db)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-gen-1", "gen-1", "ci.run", "run-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"repo-api","commit_sha":"aaa111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "environment-a-gen-1", "gen-1", "ci.environment_observation", "environment-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","environment":"production","source":"deploy_event"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-2", "gen-2", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb222","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-a-gen-3", "gen-3", "ci.artifact", "artifact-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","artifact_id":"artifact-a","artifact_type":"container_image","artifact_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	runCICDRunSnapshotPatchHandler(t, ctx, db, "intent-live-omitted-run")

	var environment string
	var hasOldEnvironment bool
	if err := db.QueryRowContext(ctx, `
SELECT
    COALESCE(payload->>'environment', ''),
    COALESCE(payload->'evidence_fact_ids', '[]'::jsonb) ? 'environment-a-gen-1'
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
  AND payload->>'run_id' = 'run-a'`).Scan(&environment, &hasOldEnvironment); err != nil {
		t.Fatalf("query recovered omitted run: %v", err)
	}
	if environment != "" || hasOldEnvironment {
		t.Fatalf("recovered run environment = %q old_evidence=%v, want only the old ci.run anchor", environment, hasOldEnvironment)
	}

	before := readCICDRunSnapshotPayloads(t, ctx, db)
	runCICDRunSnapshotPatchHandler(t, ctx, db, "intent-live-omitted-run")
	after := readCICDRunSnapshotPayloads(t, ctx, db)
	if before != after {
		t.Fatalf("retry payloads changed:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestCICDRunCorrelationArtifactTombstoneRetractsBaselineArtifact(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the baseline artifact tombstone proof")
	}
	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunSnapshotGenerations(t, ctx, db)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-2", "gen-2", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb222","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-b-gen-2", "gen-2", "ci.artifact", "artifact-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","artifact_id":"artifact-b","artifact_type":"container_image","artifact_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-b-gen-3", "gen-3", "ci.artifact", "artifact-b-key", true, `{}`)
	runCICDRunSnapshotPatchHandler(t, ctx, db, "intent-baseline-artifact-tombstone")

	var digest string
	var hasOldArtifact bool
	if err := db.QueryRowContext(ctx, `
SELECT
    COALESCE(payload->>'artifact_digest', ''),
    COALESCE(payload->'evidence_fact_ids', '[]'::jsonb) ? 'artifact-b-gen-2'
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
  AND payload->>'run_id' = 'run-b'`).Scan(&digest, &hasOldArtifact); err != nil {
		t.Fatalf("query baseline artifact tombstone result: %v", err)
	}
	if digest != "" || hasOldArtifact {
		t.Fatalf("baseline artifact digest = %q old_evidence=%v, want retracted", digest, hasOldArtifact)
	}
}

func TestListCICDRunFactsForScopePatchKeepsOnlyAuthoritativeWindowEvidence(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the CI/CD authoritative-window proof")
	}
	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunSnapshotGenerations(t, ctx, db)
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status
) VALUES (
    'gen-0', 'scope-ci', 'snapshot', '2026-08-04T12:00:30Z', '2026-08-04T12:00:30Z', 'superseded'
)`); err != nil {
		t.Fatalf("seed pre-baseline CI/CD generation: %v", err)
	}
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-1", "gen-1", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"bbb111","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "environment-b-gen-0", "gen-0", "ci.environment_observation", "environment-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","environment":"production","source":"deploy_event"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "workflow-old-gen-0", "gen-0", "ci.workflow_image_evidence", "workflow-old-key", false,
		`{"repository_id":"repo-api","image_ref":"registry.example.com/old:latest"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "deployment-old-gen-0", "gen-0", "ci.deployment_event", "deployment-old-key", false,
		`{"sha":"bbb111","repository_id":"repo-api","environment":"production","state":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "step-b-gen-2", "gen-2", "ci.step", "step-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","step_number":"1"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-b-gen-2", "gen-2", "ci.artifact", "artifact-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","artifact_id":"artifact-b","artifact_type":"container_image","artifact_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "workflow-new-gen-2", "gen-2", "ci.workflow_image_evidence", "workflow-new-key", false,
		`{"repository_id":"repo-api","image_ref":"registry.example.com/new:latest"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "deployment-new-gen-2", "gen-2", "ci.deployment_event", "deployment-new-key", false,
		`{"sha":"bbb111","repository_id":"repo-api","environment":"staging","state":"success"}`)

	loaded, err := NewFactStore(SQLDB{DB: db}).ListCICDRunFactsForScopePatch(
		ctx,
		"scope-ci",
		"gen-3",
		[]string{"github_actions"},
		[]string{"run-b"},
		[]string{"1"},
	)
	if err != nil {
		t.Fatalf("ListCICDRunFactsForScopePatch() error = %v, want nil", err)
	}
	got := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		got = append(got, envelope.FactID)
	}
	slices.Sort(got)
	want := []string{"artifact-b-gen-2", "deployment-new-gen-2", "run-b-gen-1", "step-b-gen-2", "workflow-new-gen-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("authoritative window fact IDs = %#v, want %#v", got, want)
	}
}

func readCICDRunSnapshotPayloads(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	var payloads string
	if err := db.QueryRowContext(ctx, `
SELECT COALESCE(JSONB_AGG(payload ORDER BY payload->>'run_id'), '[]'::jsonb)::text
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'`).Scan(&payloads); err != nil {
		t.Fatalf("query CI/CD run snapshot payloads: %v", err)
	}
	return payloads
}
