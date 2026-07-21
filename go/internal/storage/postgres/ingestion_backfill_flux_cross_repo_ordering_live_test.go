// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// seedFluxOrderingScopeGenRepo seeds an activated scope/generation plus a
// repository fact carrying the given remote_url, so the deferred backfill's
// catalog includes it with a resolvable RemoteURL (issue #5483 C2).
func seedFluxOrderingScopeGenRepo(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, genID, repoID, name, remoteURL string,
	observedAt time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, $2) "+
			"ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id",
		scopeID, genID); err != nil {
		t.Fatalf("seed scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
		genID, scopeID, observedAt); err != nil {
		t.Fatalf("seed generation %q: %v", genID, err)
	}
	payload := `{"repo_id":"` + repoID + `","name":"` + name + `","remote_url":"` + remoteURL + `"}`
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'repository', $1, 'git', $1, $4, $4, $5::jsonb)`,
		"repo-fact-"+genID, scopeID, genID, observedAt, payload); err != nil {
		t.Fatalf("seed repository fact for %q: %v", repoID, err)
	}
}

// seedFluxGitRepositoryFileFact seeds the "file" fact a Flux GitRepository
// manifest produces: parsed_file_data carries a non-empty flux_git_repositories
// array (with the reconciliation url) plus the EMPTY struct keys the parser
// serializes into every YAML file (flux_kustomizations/argocd_applications/
// argocd_applicationsets). The empty keys prove the deferred candidate
// predicate admits the fact ONLY via the non-empty flux_git_repositories array,
// never via a bare-key or an ArgoCD false positive. The fact carries NO
// artifact_type and NO content, exactly like the real fileFactEnvelope.
func seedFluxGitRepositoryFileFact(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID, scopeID, genID, repoID, relativePath, name, url string,
	observedAt time.Time,
) {
	t.Helper()
	payload := `{"repo_id":"` + repoID + `","relative_path":"` + relativePath + `","language":"yaml",` +
		`"parsed_file_data":{"lang":"yaml",` +
		`"flux_git_repositories":[{"name":"` + name + `","url":"` + url + `","lang":"yaml"}],` +
		`"flux_kustomizations":[],"argocd_applications":[],"argocd_applicationsets":null}}`
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'file', $1, 'git', $1, $4, $4, $5::jsonb)`,
		factID, scopeID, genID, observedAt, payload); err != nil {
		t.Fatalf("seed flux git-repository file fact %q: %v", factID, err)
	}
}

// TestDeferredBackfillRecoversFluxCrossRepoEvidenceOnSourceBeforeTarget is the
// #5483 C2 source-before-target ordering regression (codex P1). A Flux
// GitRepository manifest is committed to the config repo BEFORE its target
// deploy repo is indexed. The manifest's cross-repo DEPLOYS_FROM edge must be an
// honest non-link at first (target absent from the catalog), then materialize
// once the target repo is indexed and the deferred corpus-wide backfill
// re-discovers evidence under the changed catalog.
//
// RED before the fix: deferredRelationshipFamilyCandidatePredicateSQL did not
// admit the Flux GitRepository file fact (no artifact_type, no content, arbitrary
// path), so pass 2's re-discovery never loaded the manifest and the edge never
// appeared. GREEN after: the Flux carve-out admits the fact and the edge
// materializes on the catalog change.
func TestDeferredBackfillRecoversFluxCrossRepoEvidenceOnSourceBeforeTarget(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionDeferredPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	const targetURL = "https://github.com/acme/payments-deploy.git"

	// Config repo holds the Flux GitRepository manifest whose spec.url names the
	// (not-yet-indexed) deploy repo. The config repo's own remote_url is
	// different, so the manifest is a genuine cross-repo reference, not a self
	// reference.
	seedFluxOrderingScopeGenRepo(t, ctx, db,
		"git:scope-config", "gen-config", "repo-config", "gitops-config",
		"https://github.com/acme/gitops-config.git", base)
	seedFluxGitRepositoryFileFact(t, ctx, db,
		"flux-gitrepo-config", "git:scope-config", "gen-config", "repo-config",
		"clusters/prod/git-repository.yaml", "app-source", targetURL, base)

	// Pass 1: the deploy repo is NOT indexed yet. The manifest's cross-repo
	// url resolves to no catalog repository: an honest non-link, no edge.
	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	if edges := evidenceEdgeSet(t, ctx, db); edges["repo-config->repo-deploy"] {
		t.Fatalf("pass 1 fabricated a cross-repo edge before the target repo was indexed: %v", edges)
	}

	// Index the deploy repo whose remote_url matches the manifest's spec.url.
	// This is the catalog change that must re-trigger deferred re-discovery over
	// the already-committed config-repo manifest partition.
	seedFluxOrderingScopeGenRepo(t, ctx, db,
		"git:scope-deploy", "gen-deploy", "repo-deploy", "payments-deploy",
		targetURL, base.Add(time.Hour))

	// Pass 2: the catalog now contains the deploy repo. The deferred backfill
	// must reload the config-repo partition (its memo fingerprint changed with
	// the catalog) and re-run DiscoverEvidence over the Flux manifest, which now
	// resolves by strict NormalizeRemoteURL equality to the deploy repo.
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	edges := evidenceEdgeSet(t, ctx, db)
	if !edges["repo-config->repo-deploy"] {
		t.Fatalf(
			"pass 2 missing cross-repo DEPLOYS_FROM edge repo-config->repo-deploy after the target repo was indexed; "+
				"the deferred backfill failed to re-discover the Flux GitRepository manifest evidence. got edges: %v",
			edges,
		)
	}
}

// TestDeferredBackfillRecoversFluxCrossRepoEvidenceOnRemoteURLChange is the
// #5483 C2 remote_url-change regression (codex P1). The deploy repo is indexed
// first, but with a remote_url that does NOT match the manifest (a mirror the
// config repo does not reference). No edge resolves. When the deploy repo later
// changes its remote_url to the one the manifest names (a new generation, a
// catalog change), the deferred backfill must re-resolve the manifest and the
// DEPLOYS_FROM edge must materialize — never before, keeping strict-equality
// never-fabricate.
func TestDeferredBackfillRecoversFluxCrossRepoEvidenceOnRemoteURLChange(t *testing.T) {
	dsn := dsnForDeferredPartitionMemoProof(t)
	ctx := context.Background()
	db := openDeferredPartitionMemoProofDB(t, dsn)
	provisionDeferredPartitionMemoSchema(t, db)

	base := time.Date(2026, time.July, 20, 0, 0, 0, 0, time.UTC)
	const manifestURL = "https://github.com/acme/payments-deploy.git"
	const staleURL = "https://gitlab.example.com/acme/payments-deploy.git"

	seedFluxOrderingScopeGenRepo(t, ctx, db,
		"git:scope-config", "gen-config", "repo-config", "gitops-config",
		"https://github.com/acme/gitops-config.git", base)
	seedFluxGitRepositoryFileFact(t, ctx, db,
		"flux-gitrepo-config", "git:scope-config", "gen-config", "repo-config",
		"clusters/prod/git-repository.yaml", "app-source", manifestURL, base)

	// Deploy repo indexed with a DIFFERENT remote host than the manifest names.
	// The manifest's spec.url must NOT strict-equal it: honest non-link.
	seedFluxOrderingScopeGenRepo(t, ctx, db,
		"git:scope-deploy", "gen-deploy-1", "repo-deploy", "payments-deploy",
		staleURL, base.Add(time.Hour))

	store := NewIngestionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }
	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 1 BackfillAllRelationshipEvidence() error = %v", err)
	}
	if edges := evidenceEdgeSet(t, ctx, db); edges["repo-config->repo-deploy"] {
		t.Fatalf("pass 1 fabricated an edge to a repo whose remote_url does not match the manifest: %v", edges)
	}

	// The deploy repo changes its remote_url to the one the manifest names (a
	// new, newer generation). The catalog RemoteURL for repo-deploy now matches
	// the manifest's normalized spec.url.
	seedFluxOrderingScopeGenRepo(t, ctx, db,
		"git:scope-deploy", "gen-deploy-2", "repo-deploy", "payments-deploy",
		manifestURL, base.Add(2*time.Hour))

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("pass 2 BackfillAllRelationshipEvidence() error = %v", err)
	}
	edges := evidenceEdgeSet(t, ctx, db)
	if !edges["repo-config->repo-deploy"] {
		t.Fatalf(
			"pass 2 missing cross-repo DEPLOYS_FROM edge repo-config->repo-deploy after the deploy repo's remote_url "+
				"changed to match the manifest; the deferred backfill failed to re-resolve. got edges: %v",
			edges,
		)
	}
}
