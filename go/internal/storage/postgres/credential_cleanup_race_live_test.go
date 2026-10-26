// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8833 disposable PostgreSQL concurrency proof that the operator cleanup's files, entities, and references statements (run from docs/public/deployment/credential-file-cleanup.md) interleaved with the production ContentWriter rewriting the same paths under a post-watermark generation never leave a post-watermark row blank: either the writer commits first and the guard skips the row, or the purge locks first and the writer's later upsert overwrites the blank; 40 randomized iterations, no deadlock; isolated bootstrapped schema per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"fmt"
	"math/rand/v2"
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
