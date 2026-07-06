// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// seedSucceededReopenWorkItem inserts one succeeded reducer work item for the
// given domain and (scope_id, generation_id) partition, matching the shape
// generation_liveness_sql.go's writer produces for a completed reducer run.
func seedSucceededReopenWorkItem(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID, scopeID, genID, domain string,
	at time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', '{}'::jsonb, $5, $5)`,
		workItemID, scopeID, genID, domain, at); err != nil {
		t.Fatalf("seed succeeded %s work item %q: %v", domain, workItemID, err)
	}
}

// workItemStatus reads back one work item's current status.
func workItemStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(
		ctx,
		"SELECT status FROM fact_work_items WHERE work_item_id = $1", workItemID,
	).Scan(&status); err != nil {
		t.Fatalf("read status for work item %q: %v", workItemID, err)
	}
	return status
}

// intentSnapshot is one (intent_id, canonical payload JSON) pair, comparable
// for byte-identity across two independent runs of the REAL reducer resolver
// against the same evidence.
type intentSnapshot struct {
	intentID string
	payload  string
}

// snapshotSharedProjectionIntents reads every shared_projection_intents row
// for one generation back as a sorted (intent_id, payload) slice, using
// json.Marshal on the decoded payload map (not the raw column bytes) so key
// ordering differences that do not change meaning cannot produce a false
// mismatch.
func snapshotSharedProjectionIntents(t *testing.T, ctx context.Context, db *sql.DB, generationID string) []intentSnapshot {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		"SELECT intent_id, payload FROM shared_projection_intents WHERE generation_id = $1 ORDER BY intent_id",
		generationID)
	if err != nil {
		t.Fatalf("query shared_projection_intents for generation %q: %v", generationID, err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []intentSnapshot
	for rows.Next() {
		var intentID string
		var payloadBytes []byte
		if err := rows.Scan(&intentID, &payloadBytes); err != nil {
			t.Fatalf("scan shared_projection_intents row: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(payloadBytes, &decoded); err != nil {
			t.Fatalf("unmarshal intent payload for %q: %v", intentID, err)
		}
		canonical, err := json.Marshal(decoded)
		if err != nil {
			t.Fatalf("re-marshal intent payload for %q: %v", intentID, err)
		}
		snapshots = append(snapshots, intentSnapshot{intentID: intentID, payload: string(canonical)})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate shared_projection_intents rows: %v", err)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].intentID < snapshots[j].intentID })
	return snapshots
}

// resolveCrossRepoIntents drives the REAL reducer cross-repo resolution
// handler (reducer.CrossRepoRelationshipHandler.Resolve) for one generation
// against the REAL Postgres-backed RelationshipStore (evidence loader) and
// SharedIntentStore (intent writer) — the same production ports
// RunDeferredRelationshipMaintenance's downstream reducer claim/drain path
// uses, not a hand-built stand-in. No readiness gate is configured
// (ReadinessLookup/ReadinessPrefetch left nil), which Resolve treats as
// "bypass the backward-evidence gate" (see cross_repo_resolution.go), matching
// how these tests call Resolve directly rather than through the queue claim
// path.
func resolveCrossRepoIntents(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) int {
	t.Helper()
	adapter := SQLDB{DB: db}
	handler := reducer.CrossRepoRelationshipHandler{
		EvidenceLoader: NewRelationshipStore(adapter),
		IntentWriter:   NewSharedIntentStore(adapter),
	}
	count, err := handler.Resolve(ctx, scopeID, generationID)
	if err != nil {
		t.Fatalf("CrossRepoRelationshipHandler.Resolve(%q, %q) error = %v", scopeID, generationID, err)
	}
	return count
}

// ensureResolverSchema applies the real RelationshipStore/SharedIntentStore
// DDL (EnsureSchema) on top of the hand-rolled proof schema, so the resolver
// intent-row differential test exercises the REAL production schema for
// relationship_assertions/relationship_candidates/resolved_relationships and
// shared_projection_intents, not a re-derived approximation. Both stores use
// CREATE TABLE/COLUMN/INDEX IF NOT EXISTS, so this is safe to layer on top of
// the relationship_evidence_facts table reopenPartitionMemoProofSchemaSQL
// already created (identical column shape) — it adds only the tables/columns
// that are missing.
func ensureResolverSchema(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	adapter := SQLDB{DB: db}
	if err := NewRelationshipStore(adapter).EnsureSchema(ctx); err != nil {
		t.Fatalf("RelationshipStore.EnsureSchema() error = %v", err)
	}
	if err := NewSharedIntentStore(adapter).EnsureSchema(ctx); err != nil {
		t.Fatalf("SharedIntentStore.EnsureSchema() error = %v", err)
	}
}

// seedArgoCDControlFixture seeds the same ArgoCD ApplicationSet + external
// config repo shape TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads
// uses, so this file's reopen-layer ArgoCD proof exercises the identical
// evidence shape the fact-load-layer proof already covers.
func seedArgoCDControlFixture(t *testing.T, ctx context.Context, db *sql.DB, base time.Time) {
	t.Helper()

	if _, err := db.ExecContext(ctx,
		"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", "git:scope-control"); err != nil {
		t.Fatalf("seed scope-control: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		"gen-control", "git:scope-control", base); err != nil {
		t.Fatalf("seed gen-control: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-control", "git:scope-control", "gen-control", base,
		`{"repo_id":"repo-control","name":"control-service"}`); err != nil {
		t.Fatalf("seed repo-control repository fact: %v", err)
	}

	appSetYAML := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: demo
spec:
  generators:
  - git:
      repoURL: https://github.com/example/repo-config.git
      files:
      - path: services/*/service.yaml
  template:
    spec:
      source:
        repoURL: "{{ .service.repoURL }}"
`
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'file', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"appset-control", "git:scope-control", "gen-control", base,
		mustJSONPayload(t, "repo-control", "argocd", "appset.yaml", appSetYAML)); err != nil {
		t.Fatalf("seed ApplicationSet fact: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", "git:scope-config"); err != nil {
		t.Fatalf("seed scope-config: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		"gen-config-1", "git:scope-config", base); err != nil {
		t.Fatalf("seed gen-config-1: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2",
		"gen-config-1", "git:scope-config"); err != nil {
		t.Fatalf("activate gen-config-1: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-config", "git:scope-config", "gen-config-1", base,
		`{"repo_id":"repo-config","name":"repo-config"}`); err != nil {
		t.Fatalf("seed repo-config repository fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"content-config-1", "git:scope-config", "gen-config-1", base,
		`{"repo_id":"repo-config","artifact_type":"yaml","relative_path":"services/demo/service.yaml","content":"service:\n  repoURL: https://github.com/example/repo-target-v1.git\n"}`); err != nil {
		t.Fatalf("seed repo-config v1 content fact: %v", err)
	}

	for _, id := range []string{"target-v1", "target-v2"} {
		scopeID := "git:scope-" + id
		genID := "gen-" + id
		repoID := "repo-" + id
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", scopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			genID, scopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", genID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
			"repo-fact-"+repoID, scopeID, genID, base,
			`{"repo_id":"`+repoID+`","name":"`+repoID+`"}`); err != nil {
			t.Fatalf("seed repository fact for %q: %v", repoID, err)
		}
	}
}

// mustJSONPayload builds the file-fact payload JSON for the ArgoCD fixture,
// matching the shape TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads
// constructs inline via fmt.Sprintf.
func mustJSONPayload(t *testing.T, repoID, artifactType, relativePath, content string) string {
	t.Helper()
	escaped := ""
	for _, r := range content {
		switch r {
		case '\n':
			escaped += `\n`
		case '"':
			escaped += `\"`
		default:
			escaped += string(r)
		}
	}
	return `{"repo_id":"` + repoID + `","artifact_type":"` + artifactType + `","relative_path":"` + relativePath + `","content":"` + escaped + `"}`
}
