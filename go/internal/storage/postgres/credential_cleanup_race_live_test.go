// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 disposable PostgreSQL concurrency proof that the operator cleanup's files, entities, and references statements (run from docs/public/deployment/credential-file-cleanup.md) interleaved with the production ContentWriter rewriting the same paths under a post-watermark generation never leave a post-watermark row blank: either the writer commits first and the guard skips the row, or the purge locks first and the writer's later upsert overwrites the blank; 40 randomized iterations, no deadlock; and that statements A and B interleaved with concurrent search-document upserts plus the activation flip across both lock orders never delete a post-watermark active document or fact, with the generation-guard-removal mutation deleting the rebuilt rows; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestCredentialCleanupPurgeRacesTheContentWriterLive(t *testing.T) {
	proof := openReprojectProof(t)
	f := &cleanupFixture{t: t, ctx: t.Context(), db: proof.db, w: time.Now().UTC().Truncate(time.Second), writer: NewContentWriter(SQLDB{DB: proof.db})}
	_, purgeFence := credentialCleanupFences(t)
	purge := splitSections(t, purgeFence, statementHeadPattern)
	raw := "npmAuthToken: " + cleanupSeed + "\nnpmRegistryServer: \"https://registry.internal.acme.io/\"\n"
	redacted := "npmAuthToken: REDACTED_api_token\nnpmRegistryServer: \"https://registry.internal.acme.io/\"\n"
	rng := rand.New(rand.NewPCG(8833, 1))
	purgeFirst, writerFirst := 0, 0

	for iteration := 0; iteration < 40; iteration++ {
		repo := fmt.Sprintf("race-%d", iteration)
		scopeID, preGen := f.scope(repo, f.w.Add(-time.Hour))
		f.content(repo, scopeID, preGen, cleanupPolicyPaths, raw)
		postGen := scopeID + "-post"
		f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'pending')`, postGen, scopeID, f.w.Add(time.Hour))
		vars := map[string]string{"pred": credentialCleanupPred, "w": f.w.Format(time.RFC3339), "repo": repo}

		var wg sync.WaitGroup
		wg.Add(2)
		purgeDelay := time.Duration(rng.IntN(4)) * time.Millisecond
		writerDelay := time.Duration(rng.IntN(4)) * time.Millisecond
		// Odd iterations hold the purge's batch locks before its rewrite, so
		// the writer's upsert waits on them and lands after the blank.
		hold := time.Duration(iteration%2) * 150 * time.Millisecond
		if hold > 0 {
			writerDelay += 50 * time.Millisecond
		}
		var touched int64
		go func() {
			defer wg.Done()
			time.Sleep(purgeDelay)
			for _, name := range []string{"files", "entities", "refs"} {
				for i := 0; i < 5; i++ {
					n := runScriptHolding(t, f.ctx, f.db, bindPSQL(t, purge[name], vars), hold)
					touched += n
					if n == 0 {
						break
					}
				}
			}
		}()
		go func() {
			defer wg.Done()
			time.Sleep(writerDelay)
			materialization := content.Materialization{RepoID: repo, ScopeID: scopeID, GenerationID: postGen}
			for i, path := range cleanupPolicyPaths {
				materialization.Records = append(materialization.Records, content.Record{Path: path, Body: redacted})
				materialization.Entities = append(materialization.Entities, content.EntityRecord{
					EntityID: fmt.Sprintf("entity-%s-%s-%d", repo, preGen, i), Path: path, EntityType: "Variable",
					EntityName: "v", StartLine: 1, EndLine: 2, SourceCache: redacted,
				})
			}
			if _, err := f.writer.Write(f.ctx, materialization); err != nil {
				t.Errorf("iteration %d: writer: %v", iteration, err)
			}
		}()
		wg.Wait()
		if touched > 0 {
			purgeFirst++
		} else {
			writerFirst++
		}

		// Every row now carries the writer's post-watermark generation and the
		// redacted body; none is blank, and no raw copy is left.
		if n := f.count(`SELECT count(*) FROM content_files WHERE repo_id = $1 AND generation_id = $2 AND content = $3`, repo, postGen, redacted); n != int64(len(cleanupPolicyPaths)) {
			t.Fatalf("iteration %d: %d of %d files hold the rebuilt body (purge delay %v, writer delay %v)",
				iteration, n, len(cleanupPolicyPaths), purgeDelay, writerDelay)
		}
		if n := f.count(`SELECT count(*) FROM content_entities WHERE repo_id = $1 AND source_cache = $2`, repo, redacted); n != int64(len(cleanupPolicyPaths)) {
			t.Fatalf("iteration %d: %d of %d entities hold the rebuilt source_cache", iteration, n, len(cleanupPolicyPaths))
		}
		if n := f.count(`SELECT count(*) FROM content_file_references WHERE repo_id = $1`, repo); n != int64(len(cleanupPolicyPaths)) {
			t.Fatalf("iteration %d: %d reference rows, want the rebuilt %d", iteration, n, len(cleanupPolicyPaths))
		}
		if n := f.count(`SELECT count(*) FROM content_files WHERE repo_id = $1 AND strpos(content, $2) > 0`, repo, cleanupSeed); n != 0 {
			t.Fatalf("iteration %d: %d files still hold the raw body", iteration, n)
		}
	}
	t.Logf("purge blanked first in %d iterations; the writer rebuilt first in %d", purgeFirst, writerFirst)
	if purgeFirst == 0 || writerFirst == 0 {
		t.Fatalf("the race exercised one order only (purge first %d, writer first %d); both must run", purgeFirst, writerFirst)
	}
}

// searchRebuiltCopies seeds the rebuilt (post-watermark) search copies for one
// path the way the reindex rebuild writes them: an index document, its term,
// and its reducer fact, all carrying redacted context text. It returns the
// first write error instead of failing the test so race goroutines can report
// it with Errorf; sequential callers fail on the returned error.
func (f *cleanupFixture) searchRebuiltCopies(repo, scopeID, generation, path string) error {
	const redacted = "token: REDACTED"
	documentID := "searchdoc:content_file:" + repo + ":" + path
	factID := "fact-" + generation + "-" + path
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
VALUES ($1, $2, $3, $4, $5, 'content_file', jsonb_build_object('Path', $6::text, 'ContextText', $7::text), 2, now())`,
		scopeID, generation, documentID, factID, repo, path, redacted); err != nil {
		return err
	}
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, $3, 'tk', lower($4::text), 1)`, scopeID, generation, documentID, redacted); err != nil {
		return err
	}
	_, err := f.db.ExecContext(f.ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'reducer_eshu_search_document', $1, 'git', $1, now(), now(),
    jsonb_build_object('repo_id', $4::text, 'document', jsonb_build_object('Path', $5::text, 'ContextText', $6::text)))`,
		factID, scopeID, generation, repo, path, redacted)
	return err
}

func TestCredentialCleanupPurgeABRacesTheSearchDocumentWriterLive(t *testing.T) {
	proof := openReprojectProof(t)
	f := &cleanupFixture{t: t, ctx: t.Context(), db: proof.db, w: time.Now().UTC().Truncate(time.Second), writer: NewContentWriter(SQLDB{DB: proof.db})}
	_, purgeFence := credentialCleanupFences(t)
	purge := splitSections(t, purgeFence, statementHeadPattern)
	rng := rand.New(rand.NewPCG(9166, 1))
	const iterations = 16
	purgeFirst, writerFirst := 0, 0

	for iteration := 0; iteration < iterations; iteration++ {
		repo := fmt.Sprintf("ab-race-%d", iteration)
		scopeID, preGen := f.scope(repo, f.w.Add(-time.Hour))
		for _, path := range cleanupPolicyPaths {
			f.searchCopies(repo, scopeID, preGen, path, true, true)
		}
		postGen := scopeID + "-post"
		f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'pending')`, postGen, scopeID, f.w.Add(time.Hour))
		vars := map[string]string{"pred": credentialCleanupPred, "w": f.w.Format(time.RFC3339), "repo": repo}

		// Even iterations bias purge-first (the writer sleeps past the
		// purge); odd iterations bias writer-first. The bias is verified
		// below from commit timestamps, not assumed from the sleeps.
		purgeDelay := time.Duration(rng.IntN(4)) * time.Millisecond
		writerDelay := time.Duration(rng.IntN(4)) * time.Millisecond
		if iteration%2 == 0 {
			writerDelay += 250 * time.Millisecond
		} else {
			purgeDelay += 250 * time.Millisecond
		}
		var wg sync.WaitGroup
		wg.Add(2)
		var purgeStart, purgeEnd, flipAt time.Time
		var touched int64
		go func() {
			defer wg.Done()
			time.Sleep(purgeDelay)
			purgeStart = time.Now()
			for _, name := range []string{"A", "B"} {
				for i := 0; ; i++ {
					n := runScript(t, f.ctx, f.db, bindPSQL(t, purge[name], vars))
					touched += n
					if n == 0 || i > 20 {
						break
					}
				}
			}
			purgeEnd = time.Now()
		}()
		go func() {
			defer wg.Done()
			time.Sleep(writerDelay)
			for _, path := range cleanupPolicyPaths {
				if err := f.searchRebuiltCopies(repo, scopeID, postGen, path); err != nil {
					t.Errorf("iteration %d: rebuild: %v", iteration, err)
					return
				}
			}
			if _, err := f.db.ExecContext(f.ctx, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, postGen, scopeID); err != nil {
				t.Errorf("iteration %d: flip: %v", iteration, err)
				return
			}
			flipAt = time.Now()
		}()
		wg.Wait()

		switch {
		case !purgeEnd.IsZero() && !flipAt.IsZero() && purgeEnd.Before(flipAt):
			purgeFirst++
			if touched == 0 {
				t.Fatalf("iteration %d: the purge committed first but touched 0 rows; the harness is vacuous", iteration)
			}
			if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE generation_id = $1`, preGen); n != 0 {
				t.Fatalf("iteration %d: %d pre-watermark documents survive a purge-first pass", iteration, n)
			}
			if n := f.count(`SELECT count(*) FROM fact_records WHERE generation_id = $1 AND fact_kind = 'reducer_eshu_search_document'`, preGen); n != 0 {
				t.Fatalf("iteration %d: %d pre-watermark facts survive a purge-first pass", iteration, n)
			}
			if n := f.count(`SELECT count(*) FROM eshu_search_index_terms WHERE generation_id = $1`, preGen); n != 0 {
				t.Fatalf("iteration %d: %d pre-watermark terms survive a purge-first pass", iteration, n)
			}
		case !purgeStart.IsZero() && !flipAt.IsZero() && flipAt.Before(purgeStart):
			writerFirst++
			if touched != 0 {
				t.Fatalf("iteration %d: the flip committed first but the purge touched %d rows", iteration, touched)
			}
		default:
			t.Fatalf("iteration %d: overlap (purge %v-%v, flip %v); neither lock order was clean", iteration, purgeStart, purgeEnd, flipAt)
		}

		// No post-watermark active document, term, or fact is deleted in any order.
		if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE repo_id = $1 AND generation_id = $2`, repo, postGen); n != int64(len(cleanupPolicyPaths)) {
			t.Fatalf("iteration %d: %d of %d rebuilt documents survive", iteration, n, len(cleanupPolicyPaths))
		}
		if n := f.count(`SELECT count(*) FROM eshu_search_index_terms WHERE scope_id = $1 AND generation_id = $2`, scopeID, postGen); n != int64(len(cleanupPolicyPaths)) {
			t.Fatalf("iteration %d: %d of %d rebuilt terms survive", iteration, n, len(cleanupPolicyPaths))
		}
		if n := f.count(`SELECT count(*) FROM fact_records WHERE generation_id = $1 AND fact_kind = 'reducer_eshu_search_document'`, postGen); n != int64(len(cleanupPolicyPaths)) {
			t.Fatalf("iteration %d: %d of %d rebuilt facts survive", iteration, n, len(cleanupPolicyPaths))
		}
		var active string
		if err := f.db.QueryRowContext(f.ctx, `SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1`, scopeID).Scan(&active); err != nil || active != postGen {
			t.Fatalf("iteration %d: active generation %q (%v), want the rebuilt %q", iteration, active, err, postGen)
		}
	}
	t.Logf("purge first in %d iterations; the writer first in %d", purgeFirst, writerFirst)
	if purgeFirst == 0 || writerFirst == 0 {
		t.Fatalf("the race exercised one order only (purge first %d, writer first %d); both must run", purgeFirst, writerFirst)
	}
}

// TestCredentialCleanupPurgeABGuardRemovalDeletesRebuiltLive is the RED check
// for the A/B race proof: the same writer-first setup run through the page's
// statements with the generation guard stripped must delete the just-rebuilt
// active documents and facts. If the unguarded statements spare them, the
// race proof above is vacuous and this test fails.
func TestCredentialCleanupPurgeABGuardRemovalDeletesRebuiltLive(t *testing.T) {
	proof := openReprojectProof(t)
	f := &cleanupFixture{t: t, ctx: t.Context(), db: proof.db, w: time.Now().UTC().Truncate(time.Second), writer: NewContentWriter(SQLDB{DB: proof.db})}
	_, purgeFence := credentialCleanupFences(t)
	purge := splitSections(t, purgeFence, statementHeadPattern)
	const guardLine = "    AND g.ingested_at < :'w'\n"
	unguarded := make(map[string]string, 2)
	for _, name := range []string{"A", "B"} {
		if got := strings.Count(purge[name], guardLine); got != 1 {
			t.Fatalf("statement %s holds %d generation guards, want 1: the mutation premise drifted", name, got)
		}
		unguarded[name] = strings.Replace(purge[name], guardLine, "", 1)
	}

	repo := "ab-guard-mutation"
	scopeID, preGen := f.scope(repo, f.w.Add(-time.Hour))
	for _, path := range cleanupPolicyPaths {
		f.searchCopies(repo, scopeID, preGen, path, true, true)
	}
	postGen := scopeID + "-post"
	f.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'snapshot', $3, $3, 'pending')`, postGen, scopeID, f.w.Add(time.Hour))
	for _, path := range cleanupPolicyPaths {
		if err := f.searchRebuiltCopies(repo, scopeID, postGen, path); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	f.exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, postGen, scopeID)
	vars := map[string]string{"pred": credentialCleanupPred, "w": f.w.Format(time.RFC3339), "repo": repo}

	// Phase 1: the guarded statements spare every rebuilt row. The flip
	// already moved the scope to the rebuilt generation, so the raw
	// pre-watermark copies are inactive and correctly survive too; phase 2
	// proves the statements execute by deleting through the same predicates.
	for _, name := range []string{"A", "B"} {
		for i := 0; ; i++ {
			if n := runScript(t, f.ctx, f.db, bindPSQL(t, purge[name], vars)); n == 0 || i > 20 {
				break
			}
		}
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE generation_id = $1`, preGen); n != int64(len(cleanupPolicyPaths)) {
		t.Fatalf("guarded pass: %d of %d inactive pre-watermark documents survive", n, len(cleanupPolicyPaths))
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE repo_id = $1 AND generation_id = $2`, repo, postGen); n != int64(len(cleanupPolicyPaths)) {
		t.Fatalf("guarded pass: %d of %d rebuilt documents survive", n, len(cleanupPolicyPaths))
	}
	if n := f.count(`SELECT count(*) FROM fact_records WHERE generation_id = $1 AND fact_kind = 'reducer_eshu_search_document'`, postGen); n != int64(len(cleanupPolicyPaths)) {
		t.Fatalf("guarded pass: %d of %d rebuilt facts survive", n, len(cleanupPolicyPaths))
	}

	// Phase 2: with the generation guard removed, the same statements must
	// delete the just-rebuilt active rows: the silent search gap.
	for _, name := range []string{"A", "B"} {
		for i := 0; ; i++ {
			if n := runScript(t, f.ctx, f.db, bindPSQL(t, unguarded[name], vars)); n == 0 || i > 20 {
				break
			}
		}
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE generation_id = $1`, preGen); n != int64(len(cleanupPolicyPaths)) {
		t.Fatalf("unguarded statements deleted %d of %d inactive pre-watermark documents; the mutation removed more than the guard", n, len(cleanupPolicyPaths))
	}
	if n := f.count(`SELECT count(*) FROM eshu_search_index_documents WHERE repo_id = $1 AND generation_id = $2`, repo, postGen); n != 0 {
		t.Fatalf("unguarded statements spared %d rebuilt documents; the guard-removal check is vacuous", n)
	}
	if n := f.count(`SELECT count(*) FROM fact_records WHERE generation_id = $1 AND fact_kind = 'reducer_eshu_search_document'`, postGen); n != 0 {
		t.Fatalf("unguarded statements spared %d rebuilt facts; the guard-removal check is vacuous", n)
	}
}
