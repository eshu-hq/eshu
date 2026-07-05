// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestDeferredBackfillPartitionMemoNoChangeRerunSkipsAndIsIdentical is the (i)
// 0/0 determinism proof (issue #3624 Track 1 / B'): running the deferred
// backfill twice in a row over an UNCHANGED catalog and fact corpus must (a)
// skip every partition's fact load on pass 2 (no ArgoCD-bearing partitions in
// this fixture), (b) insert ZERO new evidence rows on pass 2 (the ON CONFLICT
// DO NOTHING content-addressed insert converges, and the memo gate never even
// re-derives the evidence to attempt inserting it), and (c) leave the
// discovered evidence edge set byte-identical between the two passes.
func TestDeferredBackfillPartitionMemoNoChangeRerunSkipsAndIsIdentical(t *testing.T) {
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
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass1Evidence := countEvidenceRows(t, ctx, db)
	pass1Edges := evidenceEdgeSet(t, ctx, db)
	pass1Memos := countMemoRows(t, ctx, db)

	if pass1Evidence == 0 {
		t.Fatal("pass 1 discovered zero evidence rows; fixture is not exercising the cross-repo edge")
	}
	if pass1Memos == 0 {
		t.Fatal("pass 1 did not write any partition memo rows")
	}
	if !pass1Edges["repo-a->repo-b"] {
		t.Fatalf("pass 1 missing expected edge repo-a->repo-b; got %v", pass1Edges)
	}

	// Pass 2: identical catalog, identical facts, identical generations. Every
	// partition should be memo-skippable now.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass2Evidence := countEvidenceRows(t, ctx, db)
	pass2Edges := evidenceEdgeSet(t, ctx, db)

	if pass2Evidence != pass1Evidence {
		t.Fatalf("pass 2 evidence row count = %d, want unchanged %d (0 NEW rows expected)", pass2Evidence, pass1Evidence)
	}
	if len(pass2Edges) != len(pass1Edges) {
		t.Fatalf("pass 2 edge set size = %d, want %d", len(pass2Edges), len(pass1Edges))
	}
	for edge := range pass1Edges {
		if !pass2Edges[edge] {
			t.Fatalf("pass 2 lost edge %q present in pass 1: byte-identical determinism violated", edge)
		}
	}
	for edge := range pass2Edges {
		if !pass1Edges[edge] {
			t.Fatalf("pass 2 introduced NEW edge %q absent from pass 1: byte-identical determinism violated", edge)
		}
	}
}

// TestDeferredBackfillPartitionMemoCatalogChangeInvalidatesAll is the (ii)
// catalog-change proof: adding a repository to the catalog flips the shared
// catalog fingerprint, so every partition's memo becomes stale and the next
// pass must reload all of them (none skipped), even though no partition's own
// (scope_id, generation_id) changed.
func TestDeferredBackfillPartitionMemoCatalogChangeInvalidatesAll(t *testing.T) {
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
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	pass1Memos := countMemoRows(t, ctx, db)
	if pass1Memos == 0 {
		t.Fatal("pass 1 wrote no memo rows")
	}

	// Onboard a NEW repository (a third scope/generation/repo). This changes the
	// $2 repo_id catalog array, flipping the fingerprint for every partition.
	newFixture := []memoProofFixture{
		{scopeID: "git:scope-c", genID: "gen-c", repoID: "repo-c", repoName: "gamma-service"},
	}
	seedMemoProofScopesAndFacts(t, ctx, db, newFixture, nil, base)

	partitions, err := loadActiveScopeGenerationPartitions(ctx, adapter)
	if err != nil {
		t.Fatalf("loadActiveScopeGenerationPartitions: %v", err)
	}
	catalog, err := loadRepositoryCatalog(ctx, adapter)
	if err != nil {
		t.Fatalf("loadRepositoryCatalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("buildDeferredScopedFactQueryParams returned ok=false for a non-empty catalog")
	}
	currentFingerprint := deferredCatalogFingerprint(params)

	memoStore := newDeferredBackfillPartitionMemoStore(adapter)
	gateResult, err := applyDeferredPartitionMemoGate(ctx, memoStore, partitions, currentFingerprint, nil)
	if err != nil {
		t.Fatalf("applyDeferredPartitionMemoGate: %v", err)
	}
	if len(gateResult.Skipped) != 0 {
		t.Fatalf("expected ZERO partitions skipped after a catalog change, got %d skipped: %v",
			len(gateResult.Skipped), gateResult.Skipped)
	}
	if len(gateResult.ToLoad) != len(partitions) {
		t.Fatalf("expected ALL %d partitions to reload after a catalog change, got %d",
			len(partitions), len(gateResult.ToLoad))
	}
}

// TestDeferredBackfillPartitionMemoGenerationChangeReloadsOnlyThatPartition is
// the (iii) generation-change proof: advancing ONE scope's generation (its own
// facts changed) must reload only that partition; a sibling scope whose
// generation is unchanged, under the SAME catalog fingerprint, must be
// memo-skippable.
func TestDeferredBackfillPartitionMemoGenerationChangeReloadsOnlyThatPartition(t *testing.T) {
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
	store := NewIngestionStore(adapter)
	store.Now = func() time.Time { return base }

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}

	// Advance scope-a's generation only (its facts changed; scope-b is untouched
	// and the catalog's repo_id/alias SET is unchanged, so the fingerprint is
	// identical).
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		"gen-a-2", "git:scope-a", base.Add(time.Hour)); err != nil {
		t.Fatalf("seed advanced generation: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2",
		"gen-a-2", "git:scope-a"); err != nil {
		t.Fatalf("activate advanced generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-repo-a-2", "git:scope-a", "gen-a-2", base.Add(time.Hour),
		`{"repo_id":"repo-a","name":"alpha-service"}`); err != nil {
		t.Fatalf("seed repository fact under advanced generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'content', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"content-repo-a-2", "git:scope-a", "gen-a-2", base.Add(time.Hour),
		`{"repo_id":"repo-a","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"beta-service\""}`); err != nil {
		t.Fatalf("seed content fact under advanced generation: %v", err)
	}

	partitions, err := loadActiveScopeGenerationPartitions(ctx, adapter)
	if err != nil {
		t.Fatalf("loadActiveScopeGenerationPartitions: %v", err)
	}
	catalog, err := loadRepositoryCatalog(ctx, adapter)
	if err != nil {
		t.Fatalf("loadRepositoryCatalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("buildDeferredScopedFactQueryParams returned ok=false for a non-empty catalog")
	}
	currentFingerprint := deferredCatalogFingerprint(params)

	memoStore := newDeferredBackfillPartitionMemoStore(adapter)
	gateResult, err := applyDeferredPartitionMemoGate(ctx, memoStore, partitions, currentFingerprint, nil)
	if err != nil {
		t.Fatalf("applyDeferredPartitionMemoGate: %v", err)
	}

	skippedScopes := make(map[string]bool)
	for _, p := range gateResult.Skipped {
		skippedScopes[p.ScopeID] = true
	}
	toLoadScopes := make(map[string]bool)
	for _, p := range gateResult.ToLoad {
		toLoadScopes[p.ScopeID] = true
	}

	if !toLoadScopes["git:scope-a"] {
		t.Fatal("scope-a (advanced generation) must reload; it did not appear in ToLoad")
	}
	if !skippedScopes["git:scope-b"] {
		t.Fatal("scope-b (unchanged generation, unchanged catalog) must be memo-skippable; it did not appear in Skipped")
	}
}
