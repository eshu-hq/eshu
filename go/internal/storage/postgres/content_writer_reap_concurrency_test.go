// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/lib/pq"
)

// TestContentWriterReapConcurrentDifferentRepos is the concurrency proof for
// the #5329 content_entities reap (reapStaleContentEntities /
// upsertAndReapEntities in content_writer_reap.go).
//
// The conflict domain a same-file reap could corrupt is (repo_id,
// relative_path): the anti-join reads the fresh entity_id set for a path from
// this call's in-memory entityUpserts and deletes anything not in it. Two
// Write() calls racing on the SAME (repo_id, relative_path) with two
// DIFFERENT fresh sets could each reap the other's just-written rows (see
// content_writer_reap.go's doc comment on the scope invariant this reap
// depends on). That interleaving is prevented upstream, not inside
// ContentWriter: go/internal/storage/postgres/projector_queue_claim_sql.go's
// claimProjectorWorkQuery holds a scope_id-scoped NOT EXISTS guard --
//
//	AND NOT EXISTS (
//	    SELECT 1 FROM fact_work_items AS inflight
//	    WHERE inflight.stage = 'projector'
//	      AND inflight.scope_id = work.scope_id
//	      AND inflight.work_item_id <> work.work_item_id
//	      AND inflight.status IN ('claimed', 'running')
//	      AND inflight.claim_until > $1
//	)
//
// -- so no two generations for the same scope_id (one Eshu repository) can be
// claimed/running at once, and every content-entity fact for a scope's
// touched files is loaded into ONE generation's ContentWriter.Write() call
// (FactStore.LoadFacts loads the complete generation before Project runs; see
// go/internal/projector/service.go's processWork). Two concurrent Write()
// calls therefore never target the same (repo_id, relative_path) in
// production. If that invariant is ever relaxed, this reap must adopt a
// durable per-path fresh-set marker instead of the current in-memory
// entityUpserts before same-file concurrent writes are safe again.
//
// What IS a realistic, and required-safe, concurrent pattern: multiple
// projector workers each processing a DIFFERENT repo's generation at the
// same time, all calling ContentWriter.Write concurrently against the shared
// ExecQueryer connection pool. This test proves that pattern is race-free: N
// goroutines run Write() for N distinct repos, each touching the same
// relative path name ("package.json") but under a distinct repo_id, so the
// (repo_id, relative_path) conflict domain never overlaps across goroutines.
// go test -race must report no data race, and every repo's reap DELETE must
// carry exactly that repo's own fresh entity_id (never another repo's).
func TestContentWriterReapConcurrentDifferentRepos(t *testing.T) {
	t.Parallel()

	const repoCount = 16

	db := &fakeExecQueryer{}
	writer := NewContentWriter(db)
	writer.Now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }

	var wg sync.WaitGroup
	errs := make([]error, repoCount)
	for i := 0; i < repoCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repoID := fmt.Sprintf("repo-%d", i)
			mat := content.Materialization{
				RepoID:       repoID,
				ScopeID:      fmt.Sprintf("scope-%d", i),
				GenerationID: fmt.Sprintf("gen-%d", i),
				// Every goroutine touches the SAME relative_path name, under
				// a DIFFERENT repo_id, so the conflict domain (repo_id,
				// relative_path) never overlaps -- this is the realistic
				// concurrent pattern (distinct repos processed in parallel),
				// not the same-repo race the scope_id claim exclusivity
				// upstream already rules out.
				Entities: []content.EntityRecord{
					{
						EntityID:   fmt.Sprintf("%s:package.json:Variable:dep:1", repoID),
						Path:       "package.json",
						EntityType: "Variable",
						EntityName: "dep",
						StartLine:  1,
					},
				},
			}
			_, err := writer.Write(context.Background(), mat)
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Write() for repo-%d error = %v, want nil", i, err)
		}
	}

	// Every repo's reap DELETE must carry exactly its own fresh entity_id and
	// nothing else -- proves the concurrent goroutines never cross-
	// contaminated each other's freshIDsByPath grouping despite all touching
	// a path literally named "package.json".
	reapsByRepo := make(map[string]pq.StringArray)
	for _, e := range db.execs {
		if !strings.Contains(e.query, "DELETE FROM content_entities") || !strings.Contains(e.query, "entity_id <> ALL") {
			continue
		}
		repoID, ok := e.args[0].(string)
		if !ok {
			t.Fatalf("reap repo_id arg type = %T, want string", e.args[0])
		}
		freshIDs, ok := e.args[2].(pq.StringArray)
		if !ok {
			t.Fatalf("reap fresh-id arg type = %T, want pq.StringArray", e.args[2])
		}
		if _, dup := reapsByRepo[repoID]; dup {
			t.Fatalf("more than one reap DELETE for %s", repoID)
		}
		reapsByRepo[repoID] = freshIDs
	}

	if got, want := len(reapsByRepo), repoCount; got != want {
		t.Fatalf("distinct repos reaped = %d, want %d", got, want)
	}
	for i := 0; i < repoCount; i++ {
		repoID := fmt.Sprintf("repo-%d", i)
		freshIDs, ok := reapsByRepo[repoID]
		if !ok {
			t.Fatalf("no reap DELETE found for %s", repoID)
		}
		want := fmt.Sprintf("%s:package.json:Variable:dep:1", repoID)
		if len(freshIDs) != 1 || freshIDs[0] != want {
			t.Fatalf("%s reap fresh ids = %v, want [%s]", repoID, []string(freshIDs), want)
		}
	}
}
