// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type cicdRunHistoryLiveLoader struct {
	*FactStore
}

func (cicdRunHistoryLiveLoader) ListActiveCICDRunCorrelationFacts(
	context.Context,
	[]string,
	[]string,
) ([]facts.Envelope, error) {
	return nil, nil
}

// TestCICDRunCorrelationArtifactPatchAgainstRealPostgres proves the complete
// #5770 storage/handler/write path. Generation 3 contains only an artifact for
// run A; the handler rebuilds the latest retained source snapshot and publishes
// both A and unaffected B into active generation 3 without reading a prior
// derived correlation snapshot.
func TestCICDRunCorrelationArtifactPatchAgainstRealPostgres(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres CI/CD artifact patch proof")
	}

	ctx := context.Background()
	db := openCICDRunHistoryLiveSchema(t, ctx, dsn)
	seedCICDRunHistoryLiveFixture(t, ctx, db)

	store := cicdRunHistoryLiveLoader{FactStore: NewFactStore(SQLDB{DB: db})}
	tombstoned, err := store.ListCICDRunFactsForRunKeys(
		ctx,
		"scope-ci",
		"gen-3",
		[]string{"github_actions"},
		[]string{"run-tombstoned"},
		[]string{"1"},
		nil,
	)
	if err != nil {
		t.Fatalf("load tombstoned historical run: %v", err)
	}
	if len(tombstoned) != 0 {
		t.Fatalf("tombstoned historical run was resurrected: %#v", tombstoned)
	}
	optionalRepositoryFacts, err := store.ListCICDRunFactsForRunKeys(
		ctx,
		"scope-ci",
		"gen-3",
		[]string{"github_actions", "github_actions"},
		[]string{"run-repository-omitted", "deployment-repository-omitted"},
		[]string{"1", "1"},
		nil,
	)
	if err != nil {
		t.Fatalf("load historical runs with optional repository anchors: %v", err)
	}
	for _, factID := range []string{
		"run-repository-omitted-gen-1",
		"deployment-for-run-repository-omitted-gen-1",
		"run-for-deployment-repository-omitted-gen-1",
		"deployment-repository-omitted-gen-1",
	} {
		if !containsCICDRunHistoryLiveEnvelope(optionalRepositoryFacts, factID) {
			t.Fatalf("optional-repository history = %#v, want %q", optionalRepositoryFacts, factID)
		}
	}
	workflowFacts, err := store.ListCICDRunFactsForRunKeys(
		ctx,
		"scope-ci",
		"gen-3",
		[]string{"github_actions"},
		[]string{"run-workflow"},
		[]string{"1"},
		nil,
	)
	if err != nil {
		t.Fatalf("load historical workflow-image evidence: %v", err)
	}
	for _, factID := range []string{"run-workflow-gen-1", "workflow-image-live-gen-1"} {
		if !containsCICDRunHistoryLiveEnvelope(workflowFacts, factID) {
			t.Fatalf("workflow history = %#v, want %q", workflowFacts, factID)
		}
	}
	if containsCICDRunHistoryLiveEnvelope(workflowFacts, "workflow-image-retired-gen-1") {
		t.Fatalf("workflow history = %#v, must not resurrect tombstoned evidence", workflowFacts)
	}
	tombstoneRouting, err := store.ListCICDRunFactsForRunKeys(
		ctx,
		"scope-ci",
		"gen-3",
		nil,
		nil,
		nil,
		[]string{"artifact-retired-key"},
	)
	if err != nil {
		t.Fatalf("recover payload-empty artifact tombstone identity: %v", err)
	}
	for _, factID := range []string{"run-retired-artifact-gen-1", "artifact-retired-gen-1"} {
		if !containsCICDRunHistoryLiveEnvelope(tombstoneRouting, factID) {
			t.Fatalf("tombstone routing history = %#v, want %q", tombstoneRouting, factID)
		}
	}
	handler := reducer.CICDRunCorrelationHandler{
		FactLoader: store,
		Writer: reducer.PostgresCICDRunCorrelationWriter{
			DB: SQLDB{DB: db},
			Now: func() time.Time {
				return time.Date(2026, time.August, 4, 12, 10, 0, 0, time.UTC)
			},
		},
	}
	_, err = handler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-artifact-patch",
		ScopeID:      "scope-ci",
		GenerationID: "gen-3",
		SourceSystem: "ci_cd_run",
		Domain:       reducer.DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	first := loadCICDRunHistoryLiveCorrelations(t, ctx, db)
	_, err = handler.Handle(ctx, reducer.Intent{
		IntentID:     "intent-artifact-patch",
		ScopeID:      "scope-ci",
		GenerationID: "gen-3",
		SourceSystem: "ci_cd_run",
		Domain:       reducer.DomainCICDRunCorrelation,
		Cause:        "ci/cd run-scoped evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() replay error = %v, want nil", err)
	}
	second := loadCICDRunHistoryLiveCorrelations(t, ctx, db)
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("replayed correlations changed:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

type cicdRunHistoryLiveCorrelation struct {
	FactID          string
	Payload         string
	RepositoryID    string   `json:"repository_id"`
	CommitSHA       string   `json:"commit_sha"`
	ArtifactDigest  string   `json:"artifact_digest"`
	ImageRef        string   `json:"image_ref"`
	Outcome         string   `json:"outcome"`
	CanonicalWrites int      `json:"canonical_writes"`
	Environment     string   `json:"environment"`
	EnvironmentKind string   `json:"environment_evidence"`
	EvidenceFactIDs []string `json:"evidence_fact_ids"`
}

func loadCICDRunHistoryLiveCorrelations(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) map[string]cicdRunHistoryLiveCorrelation {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT
	fact_id,
    payload->>'run_id',
    payload->>'run_attempt',
	payload::text
FROM fact_records
WHERE scope_id = 'scope-ci'
  AND generation_id = 'gen-3'
  AND fact_kind = 'reducer_ci_cd_run_correlation'
ORDER BY payload->>'run_id'`)
	if err != nil {
		t.Fatalf("query active patched correlations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := make(map[string]cicdRunHistoryLiveCorrelation)
	for rows.Next() {
		var factID string
		var runID string
		var runAttempt string
		var payload string
		if err := rows.Scan(&factID, &runID, &runAttempt, &payload); err != nil {
			t.Fatalf("scan active patched correlation: %v", err)
		}
		key := runID + ":" + runAttempt
		if _, duplicate := got[key]; duplicate {
			t.Fatalf("duplicate active patched correlation for %q", key)
		}
		correlation := cicdRunHistoryLiveCorrelation{FactID: factID, Payload: payload}
		if err := json.Unmarshal([]byte(payload), &correlation); err != nil {
			t.Fatalf("decode active patched correlation %q: %v", key, err)
		}
		got[key] = correlation
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate active patched correlations: %v", err)
	}
	if len(got) != 8 {
		t.Fatalf("active rebuilt correlations = %#v, want all eight latest retained runs", got)
	}
	runA := got["run-a:1"]
	if runA.RepositoryID != "repo-api" || runA.CommitSHA != "abc123" ||
		runA.ArtifactDigest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("run-a patched truth = %#v, want recovered run and later digest", runA)
	}
	if runA.Environment != "prod" || runA.EnvironmentKind != "deploy_event" {
		t.Fatalf("run-a environment truth = %#v, want prod from deployment event", runA)
	}
	for _, want := range []string{"run-a-gen-1", "deployment-a-gen-1", "artifact-a-gen-3"} {
		if !containsCICDRunHistoryLiveString(runA.EvidenceFactIDs, want) {
			t.Fatalf("run-a evidence = %#v, want %q", runA.EvidenceFactIDs, want)
		}
	}
	for _, stale := range []string{"artifact-a-old", "image-a-old"} {
		if containsCICDRunHistoryLiveString(runA.EvidenceFactIDs, stale) {
			t.Fatalf("run-a evidence = %#v, must exclude stale %q", runA.EvidenceFactIDs, stale)
		}
	}
	if _, ok := got["run-b:1"]; !ok {
		t.Fatalf("run-b disappeared from active generation after run-a patch: %#v", got)
	}
	retired := got["run-retired-artifact:1"]
	if retired.Outcome != "derived" || retired.CanonicalWrites != 0 {
		t.Fatalf("retired-artifact outcome = %#v, want derived with no canonical write", retired)
	}
	if retired.ArtifactDigest != "" || retired.ImageRef != "" {
		t.Fatalf("retired-artifact identity = %#v, want retracted artifact and image", retired)
	}
	for _, stale := range []string{"artifact-retired-gen-1", "image-retired"} {
		if containsCICDRunHistoryLiveString(retired.EvidenceFactIDs, stale) {
			t.Fatalf("retired-artifact evidence = %#v, must exclude %q", retired.EvidenceFactIDs, stale)
		}
	}
	malformed := got["run-malformed-artifact:1"]
	if malformed.Outcome != "derived" || malformed.CanonicalWrites != 0 {
		t.Fatalf("malformed-current-artifact outcome = %#v, want derived with no canonical write", malformed)
	}
	if malformed.ArtifactDigest != "" || malformed.ImageRef != "" {
		t.Fatalf("malformed-current-artifact identity = %#v, want retracted retained identity", malformed)
	}
	if containsCICDRunHistoryLiveString(malformed.EvidenceFactIDs, "artifact-malformed-gen-1") {
		t.Fatalf("malformed-current-artifact evidence = %#v, must exclude retained artifact", malformed.EvidenceFactIDs)
	}
	return got
}

func containsCICDRunHistoryLiveString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsCICDRunHistoryLiveEnvelope(envelopes []facts.Envelope, factID string) bool {
	for _, envelope := range envelopes {
		if envelope.FactID == factID {
			return true
		}
	}
	return false
}

func openCICDRunHistoryLiveSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("cicd_run_history_live_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create CI/CD history live schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set CI/CD history live search_path: %v", err)
	}
	for _, definition := range []string{"ingestion_scopes", "scope_generations", "fact_records"} {
		if _, err := db.ExecContext(ctx, MigrationSQL(definition)); err != nil {
			t.Fatalf("apply migration %q: %v", definition, err)
		}
	}
	return db
}

func seedCICDRunHistoryLiveFixture(t *testing.T, ctx context.Context, db *sql.DB) {
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
    ('gen-failed', 'scope-ci', 'snapshot', '2026-08-04T12:02:30Z', '2026-08-04T12:02:30Z', 'failed'),
		('gen-3', 'scope-ci', 'snapshot', '2026-08-04T12:03:00Z', '2026-08-04T12:03:00Z', 'active'),
		('gen-future', 'scope-ci', 'snapshot', '2026-08-04T12:04:00Z', '2026-08-04T12:04:00Z', 'pending'),
		('gen-empty', 'scope-ci', 'snapshot', '2026-08-04T12:05:00Z', '2026-08-04T12:05:00Z', 'superseded'),
		('gen-target', 'scope-ci', 'snapshot', '2026-08-04T12:06:00Z', '2026-08-04T12:06:00Z', 'completed');`); err != nil {
		t.Fatalf("seed CI/CD scope generations: %v", err)
	}

	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-gen-1", "gen-1", "ci.run", "run-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"repo-api","commit_sha":"abc123","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-b-gen-1", "gen-1", "ci.run", "run-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"def456","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-attempt-2", "gen-1", "ci.run", "run-a-attempt-2-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"2","repository_id":"wrong-attempt","commit_sha":"bad","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-failed", "gen-failed", "ci.run", "run-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"wrong-failed","commit_sha":"bad","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-a-future", "gen-future", "ci.run", "run-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"wrong-future","commit_sha":"bad","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-a-gen-3", "gen-3", "ci.artifact", "artifact-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","artifact_id":"artifact-a","artifact_type":"container_image","artifact_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "deployment-a-gen-1", "gen-1", "ci.deployment_event", "deployment-a-key", false,
		`{"provider":"github_actions","deployment_id":"deployment-a","status_id":"status-a","environment":"production","sha":"abc123","state":"success","repository_id":"repo-api"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-tombstoned-gen-1", "gen-1", "ci.run", "run-tombstoned-key", false,
		`{"provider":"github_actions","run_id":"run-tombstoned","run_attempt":"1","repository_id":"repo-api","commit_sha":"old","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-tombstoned-gen-1-tombstone", "gen-1", "ci.run", "run-tombstoned-key", true,
		`{}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-repository-omitted-gen-1", "gen-1", "ci.run", "run-repository-omitted-key", false,
		`{"provider":"github_actions","run_id":"run-repository-omitted","run_attempt":"1","commit_sha":"commit-run-repository-omitted","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "deployment-for-run-repository-omitted-gen-1", "gen-1", "ci.deployment_event", "deployment-for-run-repository-omitted-key", false,
		`{"provider":"github_actions","deployment_id":"deployment-for-run-repository-omitted","status_id":"status-1","environment":"production","sha":"commit-run-repository-omitted","state":"success","repository_id":"repo-api"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-for-deployment-repository-omitted-gen-1", "gen-1", "ci.run", "run-for-deployment-repository-omitted-key", false,
		`{"provider":"github_actions","run_id":"deployment-repository-omitted","run_attempt":"1","repository_id":"repo-api","commit_sha":"commit-deployment-repository-omitted","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "deployment-repository-omitted-gen-1", "gen-1", "ci.deployment_event", "deployment-repository-omitted-key", false,
		`{"provider":"github_actions","deployment_id":"deployment-repository-omitted","status_id":"status-2","environment":"production","sha":"commit-deployment-repository-omitted","state":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-workflow-gen-1", "gen-1", "ci.run", "run-workflow-key", false,
		`{"provider":"github_actions","run_id":"run-workflow","run_attempt":"1","repository_id":"repo-workflow","commit_sha":"workflow123","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "workflow-image-live-gen-1", "gen-1", "ci.workflow_image_evidence", "workflow-image-live-key", false,
		`{"repository_id":"repo-workflow","commit_sha":"workflow123","workflow_path":".github/workflows/release.yml","command_kind":"run","evidence_class":"workflow_image_ref","image_ref":"registry.example.invalid/team/workflow:latest"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "workflow-image-retired-gen-1", "gen-1", "ci.workflow_image_evidence", "workflow-image-retired-key", false,
		`{"repository_id":"repo-workflow","commit_sha":"workflow123","workflow_path":".github/workflows/retired.yml","command_kind":"run","evidence_class":"workflow_image_ref","image_ref":"registry.example.invalid/team/retired:latest"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "workflow-image-retired-gen-2", "gen-2", "ci.workflow_image_evidence", "workflow-image-retired-key", true, `{}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-retired-artifact-gen-1", "gen-1", "ci.run", "run-retired-artifact-key", false,
		`{"provider":"github_actions","run_id":"run-retired-artifact","run_attempt":"1","repository_id":"repo-api","commit_sha":"retired123","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-retired-gen-1", "gen-1", "ci.artifact", "artifact-retired-key", false,
		`{"provider":"github_actions","run_id":"run-retired-artifact","run_attempt":"1","artifact_id":"artifact-retired","artifact_type":"container_image","artifact_digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-retired-gen-2", "gen-2", "ci.artifact", "artifact-retired-key", true, `{}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-retired-gen-3", "gen-3", "ci.artifact", "artifact-retired-key", true, `{}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "run-malformed-artifact-gen-1", "gen-1", "ci.run", "run-malformed-artifact-key", false,
		`{"provider":"github_actions","run_id":"run-malformed-artifact","run_attempt":"1","repository_id":"repo-api","commit_sha":"malformed123","status":"completed","result":"success"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-malformed-gen-1", "gen-1", "ci.artifact", "artifact-malformed-key", false,
		`{"provider":"github_actions","run_id":"run-malformed-artifact","run_attempt":"1","artifact_id":"artifact-malformed","artifact_type":"container_image","artifact_digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "artifact-malformed-gen-3", "gen-3", "ci.artifact", "artifact-malformed-key", false, `{}`)

	insertCICDRunHistoryLiveFact(t, ctx, db, "correlation-a-gen-2", "gen-2", "reducer_ci_cd_run_correlation", "correlation-a-key", false,
		`{"provider":"github_actions","run_id":"run-a","run_attempt":"1","repository_id":"repo-api","commit_sha":"abc123","environment":"prod","environment_evidence":"deploy_event","artifact_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","image_ref":"registry.example.invalid/team/old@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","outcome":"exact","reason":"prior exact artifact","provenance_only":false,"canonical_writes":1,"evidence_fact_ids":["run-a-gen-1","deployment-a-gen-1","artifact-a-old","image-a-old"]}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "correlation-b-gen-2", "gen-2", "reducer_ci_cd_run_correlation", "correlation-b-key", false,
		`{"provider":"github_actions","run_id":"run-b","run_attempt":"1","repository_id":"repo-api","commit_sha":"def456","outcome":"derived","reason":"unaffected prior run","provenance_only":true,"canonical_writes":0,"evidence_fact_ids":["run-b-gen-1"]}`)
	insertCICDRunHistoryLiveFact(t, ctx, db, "correlation-retired-artifact-gen-2", "gen-2", "reducer_ci_cd_run_correlation", "correlation-retired-artifact-key", false,
		`{"provider":"github_actions","run_id":"run-retired-artifact","run_attempt":"1","repository_id":"repo-api","commit_sha":"retired123","artifact_digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","image_ref":"registry.example.invalid/team/retired@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","outcome":"exact","reason":"prior exact artifact","provenance_only":false,"canonical_writes":1,"evidence_fact_ids":["run-retired-artifact-gen-1","artifact-retired-gen-1","image-retired"]}`)
}

func insertCICDRunHistoryLiveFact(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID string,
	generationID string,
	factKind string,
	stableFactKey string,
	isTombstone bool,
	payload string,
) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_confidence, source_system,
    source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES (
    $1, 'scope-ci', $2, $3, $4,
    '1.0.0', 'ci_cd_run', 'reported', 'ci_cd_run',
    $4, '2026-08-04T12:05:00Z', '2026-08-04T12:05:00Z', $5, $6::jsonb
)`, factID, generationID, factKind, stableFactKey, isTombstone, payload)
	if err != nil {
		t.Fatalf("insert CI/CD history fact %q: %v", factID, err)
	}
}
