// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads is the (iv)
// ArgoCD carve-out proof, the make-or-break accuracy case: an ApplicationSet in
// repo A (control-plane) pulls config from repo B via a git generator. When B's
// content changes (new generation) while A's OWN (scope_id, generation_id) is
// unchanged, a naive memo keyed only on A's partition would incorrectly skip
// A's reload and silently keep the STALE A -> old-B-target edge (or drop the
// edge to the new target). This test proves A is ALWAYS reloaded (never
// memo-skipped) because it is ArgoCD-bearing, and that the resulting edge
// reflects B's NEW content after B changes.
func TestDeferredBackfillPartitionMemoArgoCDCarveOutAlwaysReloads(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionDeferredPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	// repo-control: holds the ArgoCD ApplicationSet with a git-generator pointing
	// at repo-config.
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
	// The ApplicationSet uses a git FILE generator (files[].path glob), the same
	// shape proven end-to-end by
	// TestDiscoverArgoCDApplicationSetEvaluatesGitFileGeneratorYAML in
	// internal/relationships/evidence_test.go: the generator's discovered file
	// content supplies the templated deploy repoURL, so the resolved deploy
	// edge's SourceRepoID is the DEPLOY TARGET and TargetRepoID is the
	// control-plane CONFIG repo (EvidenceKindArgoCDApplicationSetDeploySource /
	// RelDiscoversConfigIn direction).
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
		fmt.Sprintf(`{"repo_id":"repo-control","artifact_type":"argocd","relative_path":"appset.yaml","content":%q}`, appSetYAML)); err != nil {
		t.Fatalf("seed ApplicationSet fact: %v", err)
	}

	// repo-config (v1): the external config repo the ApplicationSet's git file
	// generator targets. Its matching file (services/demo/service.yaml) supplies
	// the templated repoURL the ApplicationSet's template.spec.source.repoURL
	// resolves through.
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

	// repo-target-v1 and repo-target-v2: the two candidate deploy targets.
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
			fmt.Sprintf(`{"repo_id":%q,"name":%q}`, repoID, repoID)); err != nil {
			t.Fatalf("seed repository fact for %q: %v", repoID, err)
		}
	}

	adapter := SQLDB{DB: db}
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	// Pass 1: repo-control's ApplicationSet resolves against repo-config v1's
	// content, which references repo-target-v1. Direction:
	// SourceRepoID=deploy target, TargetRepoID=config repo (RelDiscoversConfigIn).
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass1Edges := evidenceEdgeSet(t, ctx, db)
	if !pass1Edges["repo-target-v1->repo-config"] {
		t.Fatalf("pass 1 missing expected ArgoCD-resolved edge repo-target-v1->repo-config; got %v", pass1Edges)
	}

	// Confirm repo-control's partition was NOT memoized (the write-side
	// ArgoCD-bearing check must have excluded it), even though pass 1 committed
	// its phase row.
	var controlMemoCount int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM deferred_backfill_partition_memo WHERE scope_id = $1", "git:scope-control",
	).Scan(&controlMemoCount); err != nil {
		t.Fatalf("count control memo rows: %v", err)
	}
	if controlMemoCount != 0 {
		t.Fatalf("repo-control (ArgoCD-bearing) got a memo row; it must NEVER be memoized, got %d rows", controlMemoCount)
	}

	// Change repo-config: a NEW generation whose content now references
	// target-service-v2 instead. repo-control's OWN (scope_id, generation_id) is
	// completely unchanged.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		"gen-config-2", "git:scope-config", base.Add(time.Hour)); err != nil {
		t.Fatalf("seed gen-config-2: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2",
		"gen-config-2", "git:scope-config"); err != nil {
		t.Fatalf("activate gen-config-2: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-config-2", "git:scope-config", "gen-config-2", base.Add(time.Hour),
		`{"repo_id":"repo-config","name":"repo-config"}`); err != nil {
		t.Fatalf("seed repo-config repository fact under gen-2: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"content-config-2", "git:scope-config", "gen-config-2", base.Add(time.Hour),
		`{"repo_id":"repo-config","artifact_type":"yaml","relative_path":"services/demo/service.yaml","content":"service:\n  repoURL: https://github.com/example/repo-target-v2.git\n"}`); err != nil {
		t.Fatalf("seed repo-config v2 content fact: %v", err)
	}

	// Pass 2: repo-control's partition is unchanged, but it must STILL reload
	// because it is ArgoCD-bearing, and must pick up repo-config's NEW content
	// (now pointing at repo-target-v2 instead of repo-target-v1). Evidence
	// writes are append-only content-addressed inserts (ON CONFLICT DO NOTHING,
	// see UpsertEvidenceFacts's doc comment) with no retraction path anywhere in
	// this system, so pass 1's repo-target-v1->repo-config row is expected to
	// remain; the correctness assertion this carve-out exists for is that the
	// NEW edge is discovered at all — if repo-control's ArgoCD-bearing
	// partition were wrongly memo-skipped, repo-config's changed content would
	// NEVER be re-evaluated against the ApplicationSet and this edge would never
	// appear.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass2Edges := evidenceEdgeSet(t, ctx, db)
	if !pass2Edges["repo-target-v2->repo-config"] {
		t.Fatalf(
			"pass 2 missing edge repo-target-v2->repo-config after repo-config changed; "+
				"the ArgoCD carve-out failed to reload the ArgoCD-bearing partition. got edges: %v",
			pass2Edges,
		)
	}
}

// TestDeferredBackfillPartitionMemoBootstrapUnchangedFullLoad is the (v)
// bootstrap proof: an empty memo table (first-ever pass) must load every
// partition — identical to the legacy pre-memo full-load behavior — and must
// not error or silently skip anything.
func TestDeferredBackfillPartitionMemoBootstrapUnchangedFullLoad(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionDeferredPartitionMemoSchema(t, db)

	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	fixtures := []memoProofFixture{
		{scopeID: "git:scope-a", genID: "gen-a", repoID: "repo-a", repoName: "alpha-service"},
		{scopeID: "git:scope-b", genID: "gen-b", repoID: "repo-b", repoName: "beta-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, map[string]string{
		"repo-a": "beta-service",
	}, base)

	adapter := SQLDB{DB: db}

	if countMemoRows(t, ctx, db) != 0 {
		t.Fatal("test precondition violated: memo table must start empty")
	}

	partitions, err := loadActiveScopeGenerationPartitions(ctx, adapter)
	if err != nil {
		t.Fatalf("loadActiveScopeGenerationPartitions: %v", err)
	}
	catalog, _, err := loadRepositoryCatalog(ctx, adapter)
	if err != nil {
		t.Fatalf("loadRepositoryCatalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("buildDeferredScopedFactQueryParams returned ok=false for a non-empty catalog")
	}
	fingerprint := deferredCatalogFingerprint(params)

	memoStore := newDeferredBackfillPartitionMemoStore(adapter)
	gateResult, err := applyDeferredPartitionMemoGate(ctx, memoStore, partitions, fingerprint, nil)
	if err != nil {
		t.Fatalf("applyDeferredPartitionMemoGate: %v", err)
	}
	if len(gateResult.Skipped) != 0 {
		t.Fatalf("bootstrap pass (empty memo) skipped %d partitions, want 0", len(gateResult.Skipped))
	}
	if len(gateResult.ToLoad) != len(partitions) {
		t.Fatalf("bootstrap pass loaded %d partitions, want all %d", len(gateResult.ToLoad), len(partitions))
	}

	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v", err)
	}
	if countMemoRows(t, ctx, db) == 0 {
		t.Fatal("bootstrap pass committed no memo rows; expected the write side to memoize non-ArgoCD partitions")
	}
}
