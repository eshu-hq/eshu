// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestCrossRepoDeadCodeUngrantedConsumerProbeLive proves the two things no fake
// driver can: that the probe's walk answers the same question as the
// `NOT (repository_id = ANY($grant))` it replaces, and that it answers it
// without reading a group.
//
// The probe walks a producer entity's distinct (repository_id, scope_id) pairs
// in index order, seeks each pair's active row by full key equality, and stops
// at the first pair that is both outside the grant and live. The differential
// drives the shipped statement against real rows for ten named grant shapes --
// the eight in the table below plus the two 500-id grants -- and requires the
// same producer entities back from both statements, every time.
//
// Five guards cover what the answers cannot see, because each of the mutations
// they exist for leaves every entity's verdict correct. One reads the plan, and
// carries three assertions:
//
//   - the walk's per-step seek must reach an index condition rather than a
//     filter, or a step scans the entity's remaining rows;
//   - the liveness seek's index condition must carry all four key columns, or a
//     step scans the pair's retained generations for its active row;
//   - the granted-repository skip must reach an index condition too, or a
//     granted repository is walked scope by scope.
//
// The other four measure the work done, because the mutations they catch change
// no plan node and no verdict:
//
//   - the recursive term's row count for a page carrying a wide fan-out entity,
//     which is the walk's stop condition;
//   - the same count for an entity whose granted consumer repository carries
//     fifty scopes, which is the granted skip;
//   - the buffers one entity costs when every consumer repository also holds
//     each retained generation;
//   - the exact step count on the stale-consumer axis, which is the bound the
//     walk's contract used to state wrongly.
//
// The retained-generation axis is why the third of those reads buffers rather
// than rows. A group holds one row per generation the retention runner still
// keeps, the active row is the newest of them, and ent-retained carries 200 of
// them in every one of its consumer repositories; the two shapes agree on rows,
// so only buffers separate them.
//
// Every guard runs twice, once with the values in hand and once under
// plan_cache_mode = force_generic_plan, which is where pgx's statement cache
// puts these reads in production.
//
// Run with:
//
//	ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 \
//	ESHU_POSTGRES_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1
func TestCrossRepoDeadCodeUngrantedConsumerProbeLive(t *testing.T) {
	if os.Getenv("ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE") != "1" {
		t.Skip("set ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close Postgres: %v", err)
		}
	})
	// One connection for the whole test so the proof schema's search_path is
	// the one every statement, including the reader's, actually runs under.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema := fmt.Sprintf("cross_repo_dead_code_probe_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop proof schema: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set proof search path: %v", err)
	}
	seedCrossRepoDeadCodeProbeSchema(ctx, t, db)

	// One producer entity per fan-in shape the probe has to get right. The
	// consumer repository names are spaced so a hidden one can sit below the
	// smallest granted id, between two granted ids, or above the largest.
	seedCrossRepoDeadCodeProbeRows(ctx, t, db, []crossRepoDeadCodeProbeRow{
		{entityID: "ent-spread", repositoryID: "repo-a", depth: 1, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-c", depth: 2, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-e", depth: 1, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-g", depth: 3, generationID: "gen-active"},
		{entityID: "ent-spread", repositoryID: "repo-i", depth: 1, generationID: "gen-active"},
		{entityID: "ent-middle", repositoryID: "repo-e", depth: 1, generationID: "gen-active"},
		// Only the producer's own repository consumes this one, and the
		// statement excludes it, so no grant can make it hidden.
		{entityID: "ent-self", repositoryID: "repo-producer", depth: 1, generationID: "gen-active"},
		// Depth 0 is the root's own row, not a consumer edge.
		{entityID: "ent-depth-zero", repositoryID: "repo-z", depth: 0, generationID: "gen-active"},
		// A superseded generation is not evidence of anything.
		{entityID: "ent-stale", repositoryID: "repo-z", depth: 1, generationID: "gen-stale"},
	})
	// ent-busy is the shape the probe exists for: a producer entity whose
	// fan-in is far too large to read per request. Its consumers are the same
	// five repositories as ent-spread's, so it answers identically -- but only
	// a plan that seeks can answer it without reading the group, which is what
	// the plan subtest below checks.
	seedCrossRepoDeadCodeProbeFanIn(ctx, t, db, "ent-busy", crossRepoDeadCodeProbeFanInRepositories, 40000)
	// ent-fanout is the axis the walk's stop condition governs: 200 DISTINCT
	// consumer repositories, one row each, every one of them live. Its smallest
	// is repo-x000, which no grant below names, so that first pair is hidden --
	// ungranted AND live -- and a walk that stops there takes one step for it
	// where a walk that does not takes 200.
	seedCrossRepoDeadCodeProbeFanIn(ctx, t, db, "ent-fanout", crossRepoDeadCodeProbeFanOutRepositories, 1)
	// ent-retained is the axis a single-generation fixture cannot show: the
	// same five consumer repositories as ent-spread, but every one of them
	// also holds a row from each of 200 superseded generations the retention
	// runner still keeps. Its answer is ent-spread's; its cost is not, unless a
	// step can seek the active row rather than scan the group for it.
	seedCrossRepoDeadCodeProbeRetainedGenerations(
		ctx, t, db, "ent-retained", crossRepoDeadCodeProbeFanInRepositories,
		crossRepoDeadCodeProbeRetainedGenerations,
	)

	// The scope axis. A repository is not one ingestion scope: a repository
	// ingested by several has one active generation per scope, so the walk
	// steps over (repository, scope) PAIRS. That makes a GRANTED repository
	// covered by many scopes a cost the grant cannot see -- ent-scopes-granted
	// carries one granted repository under 50 scopes and one hidden consumer
	// past it, so a walk that steps to the next repository takes two steps
	// where one stepping pair by pair takes 51.
	//
	// The other two are the ungranted side, where the scopes DO have to be
	// walked because any one of them could hold the live row:
	// ent-scopes-ungranted's ungranted repository has 50 scopes whose only rows
	// are superseded and one whose row is live, and ent-scopes-ungranted-stale's
	// has 50 stale scopes and nothing live at all.
	seedCrossRepoDeadCodeProbeGrantedScopeFanOut(
		ctx, t, db, "ent-scopes-granted", "repo-a", "repo-z",
		crossRepoDeadCodeProbeScopesPerRepository,
	)
	seedCrossRepoDeadCodeProbeUngrantedScopeFanOut(
		ctx, t, db, "ent-scopes-ungranted", "repo-y", "y",
		crossRepoDeadCodeProbeScopesPerRepository, true,
	)
	seedCrossRepoDeadCodeProbeUngrantedScopeFanOut(
		ctx, t, db, "ent-scopes-ungranted-stale", "repo-w", "w",
		crossRepoDeadCodeProbeScopesPerRepository, false,
	)

	// ent-stale-repos is the axis the stop condition does NOT bound: 300
	// ungranted consumer repositories that used to call the symbol and no longer
	// do, whose rows the retention runner still keeps, and one live hidden
	// consumer after them. None of the 300 is hidden, so the walk steps past
	// every one of them.
	seedCrossRepoDeadCodeProbeStaleConsumerFanOut(
		ctx, t, db, "ent-stale-repos", "repo-v",
		crossRepoDeadCodeProbeStaleConsumerRepositories,
	)

	page := []string{
		"ent-spread", "ent-middle", "ent-self", "ent-depth-zero",
		"ent-stale", "ent-absent", "ent-busy", "ent-retained",
	}
	reader := NewContentReader(db)

	// The subtests live in sibling files, one per kind of question, so no file
	// here approaches the 500-line cap: the differential answers in
	// code_dead_code_cross_repo_ungranted_probe_live_answer_test.go, the plan
	// guards in ..._plan_test.go, the work guards in ..._work_test.go, the
	// fixtures they all seed in ..._fixture_test.go, and the EXPLAIN plumbing
	// both guard families share in ..._explain_test.go.
	runCrossRepoDeadCodeProbeGrantShapes(ctx, t, db, reader, page)
	runCrossRepoDeadCodeProbeSeekGuard(ctx, t, db, page)
	runCrossRepoDeadCodeProbeStopCondition(ctx, t, db, page)
	runCrossRepoDeadCodeProbeRetainedGenerationCost(ctx, t, db)
	runCrossRepoDeadCodeProbeGrantedScopeCost(ctx, t, db)
	runCrossRepoDeadCodeProbeStaleConsumerCost(ctx, t, db, reader)
	runCrossRepoDeadCodeProbeUngrantedScopeWalk(ctx, t, db, reader)
	runCrossRepoDeadCodeProbeBroadGrant(ctx, t, db, reader, page)
}
