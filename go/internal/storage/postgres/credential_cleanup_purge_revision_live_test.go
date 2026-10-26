// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9049 disposable PostgreSQL proof that the operator cleanup's statement A (run from docs/public/deployment/credential-file-cleanup.md) moves the semantic-search cache key: it bumps a ready scope's document projection revision together with the matching vector scope revisions exactly when it deletes documents, leaves the vector build fence alone, ignores scopes with no state rows, and a second pass touches zero rows so the repeat-until-zero loop still ends; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"testing"
	"time"
)

func TestCredentialCleanupPurgeBumpsSearchRevisionsLive(t *testing.T) {
	proof := openReprojectProof(t)
	f := &cleanupFixture{t: t, ctx: t.Context(), db: proof.db, w: time.Now().UTC().Truncate(time.Second), writer: NewContentWriter(SQLDB{DB: proof.db})}
	_, purgeFence := credentialCleanupFences(t)
	purge := splitSections(t, purgeFence, statementHeadPattern)

	// r1 carries ready search state; r2 carries documents but no state rows,
	// so the bump must be a no-op there while the deletes still land.
	r1Scope, r1Gen := f.scope("rev-r1", f.w.Add(-time.Hour))
	f.exec(`INSERT INTO eshu_search_document_projection_state
    (scope_id, generation_id, projection_revision, build_fence, state, document_count, updated_at)
VALUES ($1, $2, 5, 5, 'ready', 1, $3)`, r1Scope, r1Gen, f.w)
	f.exec(`INSERT INTO eshu_search_vector_scope_state
    (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id,
     vector_index_version, projection_revision, build_fence, document_cursor, state, updated_at)
VALUES ($1, $2, 'local', 'search_documents', 'local-hash-v1', 'vector-v1', 5, 2, '', 'ready', $3)`,
		r1Scope, r1Gen, f.w)
	f.searchCopies("rev-r1", r1Scope, r1Gen, ".yarnrc.yml", true, true)
	r2Scope, r2Gen := f.scope("rev-r2", f.w.Add(-time.Hour))
	f.searchCopies("rev-r2", r2Scope, r2Gen, "prod.tfvars", true, true)

	vars := map[string]string{"pred": credentialCleanupPred, "w": f.w.Format(time.RFC3339)}
	pass := func() map[string]int64 {
		touched := map[string]int64{}
		for _, repo := range []string{"rev-r1", "rev-r2"} {
			vars["repo"] = repo
			for _, name := range []string{"A", "B"} {
				for i := 0; ; i++ {
					n := runScript(t, f.ctx, f.db, bindPSQL(t, purge[name], vars))
					touched[name] += n
					if n == 0 || i > 20 {
						break
					}
				}
			}
		}
		return touched
	}
	first := pass()
	if first["A"] == 0 || first["B"] == 0 {
		t.Fatalf("first pass touched A=%d B=%d, want the seeded copies", first["A"], first["B"])
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE repo_id IN ('rev-r1','rev-r2')`); n != 0 {
		t.Fatalf("%d index documents survive the purge", n)
	}
	var docRev, docFence int64
	var docState string
	if err := f.db.QueryRowContext(f.ctx, `SELECT projection_revision, build_fence, state
FROM eshu_search_document_projection_state WHERE scope_id = $1 AND generation_id = $2`,
		r1Scope, r1Gen).Scan(&docRev, &docFence, &docState); err != nil {
		t.Fatalf("read document state after purge: %v", err)
	}
	if docRev != 6 {
		t.Fatalf("document revision after purge = %d, want 6 (one bump for one deleting batch)", docRev)
	}
	if docState != "ready" {
		t.Fatalf("document state after purge = %q, want ready", docState)
	}
	var vecRev, vecFence int64
	var vecState string
	if err := f.db.QueryRowContext(f.ctx, `SELECT projection_revision, build_fence, state
FROM eshu_search_vector_scope_state WHERE scope_id = $1 AND generation_id = $2
  AND provider_profile_id = 'local' AND source_class = 'search_documents'
  AND embedding_model_id = 'local-hash-v1' AND vector_index_version = 'vector-v1'`,
		r1Scope, r1Gen).Scan(&vecRev, &vecFence, &vecState); err != nil {
		t.Fatalf("read vector state after purge: %v", err)
	}
	if vecRev != 6 {
		t.Fatalf("vector revision after purge = %d, want 6 to match the document revision", vecRev)
	}
	if vecFence != 2 {
		t.Fatalf("vector build fence after purge = %d, want 2 (the purge is not a build)", vecFence)
	}
	if vecState != "ready" {
		t.Fatalf("vector state after purge = %q, want ready", vecState)
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_document_projection_state WHERE scope_id = $1`, r2Scope); n != 0 {
		t.Fatalf("the purge created %d projection state rows for a stateless scope", n)
	}
	if second := pass(); second["A"]+second["B"] != 0 {
		t.Fatalf("second pass touched %v, want 0 rows everywhere", second)
	}
}
