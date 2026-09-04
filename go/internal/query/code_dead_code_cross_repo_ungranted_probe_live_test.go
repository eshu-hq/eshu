// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
// Three plan and work guards cover what the answers cannot see, because each of
// the mutations they exist for leaves every entity's verdict correct:
//
//   - the walk's per-step seek must reach an index condition rather than a
//     filter, or a step scans the entity's remaining rows;
//   - the liveness seek's index condition must carry all four key columns, or a
//     step scans the pair's retained generations for its active row;
//   - the recursive term's measured row count must stay inside a budget, or the
//     walk has stopped stopping at the first hidden pair.
//
// The retained-generation axis is the reason the second exists. A group holds
// one row per generation the retention runner still keeps, the active row is
// the newest of them, and ent-retained carries 200 of them in every one of its
// consumer repositories; the guard for it reads buffers rather than rows,
// because the two shapes agree on rows.
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
	// consumer repositories, one row each. Its smallest is repo-x000, which no
	// grant below names, so a walk that stops at the first ungranted repository
	// takes one step for it and a walk that does not takes 200.
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

	page := []string{
		"ent-spread", "ent-middle", "ent-self", "ent-depth-zero",
		"ent-stale", "ent-absent", "ent-busy", "ent-retained",
	}
	reader := NewContentReader(db)

	cases := []struct {
		name  string
		grant []string
		want  []string
	}{
		{name: "every consumer granted", grant: crossRepoDeadCodeProbeFanInRepositories},
		{
			name:  "hidden consumer below the smallest granted id",
			grant: []string{"repo-c", "repo-e", "repo-g", "repo-i"},
			want:  []string{"ent-busy", "ent-retained", "ent-spread"},
		},
		{
			name:  "hidden consumer between two granted ids",
			grant: []string{"repo-a", "repo-c", "repo-g", "repo-i"},
			want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
		},
		{
			name:  "hidden consumer above the largest granted id",
			grant: []string{"repo-a", "repo-c", "repo-e", "repo-g"},
			want:  []string{"ent-busy", "ent-retained", "ent-spread"},
		},
		{
			// One granted id makes both outer ranges and no interior one, and
			// ent-middle -- whose only consumer is that id -- must stay unflagged
			// while ent-spread, which has consumers on both sides of it, does not.
			name:  "single-element grant",
			grant: []string{"repo-e"},
			want:  []string{"ent-busy", "ent-retained", "ent-spread"},
		},
		{
			name:  "grant wider than the corpus",
			grant: []string{"repo-a", "repo-c", "repo-e", "repo-g", "repo-i", "repo-producer", "repo-z"},
		},
		{
			name:  "grant disjoint from every consumer",
			grant: []string{"repo-b", "repo-d"},
			want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
		},
		{
			name:  "grant naming only the producer repository",
			grant: []string{"repo-producer"},
			want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(ctx, "repo-producer", page, testCase.grant)
			if err != nil {
				t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
			}
			got := make([]string, 0, len(hidden))
			for entityID := range hidden {
				got = append(got, entityID)
			}
			sort.Strings(got)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("hidden = %#v, want %#v", got, testCase.want)
			}
			// The range rewrite is only worth anything if it agrees with the
			// NOT IN it replaced. Ask the same question the slow way and
			// require an identical answer, so a bound that drifts by one
			// operator fails here rather than in a tenant's results.
			reference := crossRepoDeadCodeProbeReference(ctx, t, db, "repo-producer", page, testCase.grant)
			if !slices.Equal(got, reference) {
				t.Fatalf("probe = %#v, NOT IN reference = %#v; the ranges are not the complement of the grant", got, reference)
			}
		})
	}

	t.Run("the walk seeks and never scans a group", func(t *testing.T) {
		// This is the property the shape rests on, and no behavioural
		// assertion above can see it: a walk whose per-step lookup lands in a
		// filter instead of an index condition still returns the right
		// entities, and reads the producer entity's whole fan-in to do it.
		//
		// Which index the planner picks is left alone deliberately; the index
		// the migrations must leave behind is asserted separately, and the plan
		// it produces at corpus scale is measured in
		// docs/internal/evidence/5167-code-family-batch-1.md.
		assertCrossRepoDeadCodeProbeIndexExists(ctx, t, db)
		// Both plan modes, because they are not the same question. pgx caches
		// server-side prepared statements, so these reads run on a GENERIC
		// plan in production -- built once with no parameter values -- and the
		// shape this walk replaced planned identically to it under a custom
		// plan and then lost its bounds from the Index Cond under a generic
		// one. A guard that only ever asks the planner with the values in hand
		// cannot see that class of regression at all.
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeProbePlan(
					ctx, t, db, mode, "EXPLAIN ",
					"repo-producer", page, crossRepoDeadCodeProbeFanInRepositories,
				)
				if strings.Contains(plan, "Seq Scan on code_reachability_rows") {
					t.Fatalf("probe fell back to a sequential scan over code_reachability_rows:\n%s", plan)
				}
				// Two seeks per step, and each has to reach the index for a
				// different reason: the pair lookup so a step does not scan
				// the entity's remaining rows, the liveness lookup so it does
				// not scan the pair's retained generations. A bitmap path
				// splits the same qual across Index Cond and Recheck Cond;
				// both mean the qual reached the index, a Filter does not.
				stepped := false
				for _, line := range strings.Split(plan, "\n") {
					if !strings.Contains(line, "ROW(repository_id, scope_id) >") {
						continue
					}
					if !strings.Contains(line, "Index Cond:") && !strings.Contains(line, "Recheck Cond:") {
						t.Fatalf("the walk's step is applied as %q rather than an index condition:\n%s", strings.TrimSpace(line), plan)
					}
					stepped = true
				}
				if !stepped {
					t.Fatalf("no plan node carries the walk's per-step seek; the probe shape has drifted:\n%s", plan)
				}
				// The liveness lookup has to carry all four key columns. Three
				// of them would leave the generation a filter over the pair's
				// retained rows, which is exactly the scan migration 101 exists
				// to remove, and the answer would not change.
				if !crossRepoDeadCodeProbeHasLivenessSeek(plan) {
					t.Fatalf("no index condition carries the full (entity_id, repository_id, scope_id, generation_id) liveness seek; a step is scanning the pair's generations:\n%s", plan)
				}
				// The step from a GRANTED pair skips to the entity's next
				// repository, and that skip is a bare repository_id range
				// rather than the pair's row comparison. It has to reach the
				// index too: left as a filter, a granted repository costs one
				// read per scope covering it, which is the whole reason the
				// branch exists.
				if !crossRepoDeadCodeProbeHasRepositorySkipSeek(plan) {
					t.Fatalf("no index condition carries the granted repository skip; a granted repository is walked scope by scope:\n%s", plan)
				}
			})
		}
	})

	t.Run("the walk stops at the first ungranted repository", func(t *testing.T) {
		// The stop condition is a bound on work, not on the answer: dropping it
		// leaves every entity's verdict identical and turns each walk into a
		// full enumeration of that entity's distinct consumer repositories. No
		// assertion on the result can see that, so this one counts the rows the
		// recursive term actually produced.
		//
		// ent-fanout has 200 distinct consumer repositories and its smallest is
		// ungranted, so its walk must be one step. With 250 page entities whose
		// walks are a handful of steps each, the recursive CTE stays in the low
		// hundreds of rows; without the stop condition ent-fanout alone adds
		// about 200.
		// Both plan modes again, for the same reason: the work a generic plan
		// does is the work production does.
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeProbePlan(
					ctx, t, db, mode, "EXPLAIN (ANALYZE) ",
					"repo-producer",
					append(append([]string(nil), page...), "ent-fanout"),
					crossRepoDeadCodeProbeFanInRepositories,
				)
				walkRows := crossRepoDeadCodeProbeWalkRows(t, plan)
				if walkRows > crossRepoDeadCodeProbeWalkRowBudget {
					t.Fatalf("the recursive walk produced %d rows, want at most %d; it is no longer stopping at the first ungranted repository:\n%s",
						walkRows, crossRepoDeadCodeProbeWalkRowBudget, plan)
				}
			})
		}
	})

	t.Run("a step seeks past every retained generation", func(t *testing.T) {
		// The bound this shape exists for, and the one no answer assertion can
		// see: an (entity_id, repository_id) group holds one row per retained
		// generation, and the active row is the newest of them, so a step that
		// scans the group for it pays for retention on every step. ent-retained
		// carries 200 retained generations in each of its five consumer
		// repositories with every one of them granted, which is the case that
		// walks all five.
		//
		// Buffers, not rows, because rows are what the two shapes agree on. The
		// walk produces the same handful of steps either way; what changes is
		// how many pages a step touches to find its active row.
		//
		// Both plan modes, for the reason the other guards give: a generic plan
		// is what pgx's statement cache runs in production.
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeProbePlan(
					ctx, t, db, mode, "EXPLAIN (ANALYZE, BUFFERS) ",
					"repo-producer",
					[]string{"ent-retained"},
					crossRepoDeadCodeProbeFanInRepositories,
				)
				buffers := crossRepoDeadCodeProbeBuffers(t, plan)
				if buffers > crossRepoDeadCodeProbeRetainedBufferBudget {
					t.Fatalf("the walk touched %d buffers for one entity, want at most %d; a step is reading a retained-generation group instead of seeking past it:\n%s",
						buffers, crossRepoDeadCodeProbeRetainedBufferBudget, plan)
				}
			})
		}
	})

	t.Run("a granted repository costs one step however many scopes cover it", func(t *testing.T) {
		// The bound this shape restored. A walk stepping over
		// (repository, scope) pairs tests the grant by repository, so a granted
		// repository covered by 50 ingestion scopes cost 50 steps -- the walk
		// then passed more granted PAIRS than the grant has repositories, and
		// min(d, N) + 1 was not a bound on it at all. Stepping to the next
		// REPOSITORY from a granted pair restores it.
		//
		// No answer assertion can see this: every one of those 50 pairs is
		// granted, so it is never hidden and the verdict is identical either
		// way. The guard counts the rows the recursive term produced, the way
		// the stop-condition guard above does.
		//
		// Both plan modes, because a generic plan is what pgx's statement cache
		// runs in production.
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeProbePlan(
					ctx, t, db, mode, "EXPLAIN (ANALYZE) ",
					"repo-producer",
					[]string{"ent-scopes-granted"},
					crossRepoDeadCodeProbeFanInRepositories,
				)
				walkRows := crossRepoDeadCodeProbeWalkRows(t, plan)
				if walkRows > crossRepoDeadCodeProbeScopeFanOutWalkRowBudget {
					t.Fatalf("the walk produced %d rows for one entity, want at most %d; a granted repository is being walked scope by scope:\n%s",
						walkRows, crossRepoDeadCodeProbeScopeFanOutWalkRowBudget, plan)
				}
			})
		}
	})

	t.Run("an ungranted repository's scopes are walked until one is live", func(t *testing.T) {
		// The other half of the same rule, and the reason the skip is
		// conditional rather than unconditional. A repository the caller cannot
		// see hides a consumer if ANY of its scopes has a live row, so its
		// scopes have to be walked -- skipping to the next repository from an
		// ungranted pair would answer "nothing hidden" for
		// ent-scopes-ungranted, whose live row sits in the 51st scope of its
		// only consumer repository, behind 50 whose rows are all superseded.
		// ent-scopes-ungranted-stale is the same shape with nothing live in any
		// scope, so the walk exhausts the repository and correctly finds
		// nothing.
		hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(
			ctx, "repo-producer",
			[]string{"ent-scopes-granted", "ent-scopes-ungranted", "ent-scopes-ungranted-stale"},
			crossRepoDeadCodeProbeFanInRepositories,
		)
		if err != nil {
			t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
		}
		got := make([]string, 0, len(hidden))
		for entityID := range hidden {
			got = append(got, entityID)
		}
		sort.Strings(got)
		want := []string{"ent-scopes-granted", "ent-scopes-ungranted"}
		if !slices.Equal(got, want) {
			t.Fatalf("hidden = %#v, want %#v", got, want)
		}
		reference := crossRepoDeadCodeProbeReference(
			ctx, t, db, "repo-producer",
			[]string{"ent-scopes-granted", "ent-scopes-ungranted", "ent-scopes-ungranted-stale"},
			crossRepoDeadCodeProbeFanInRepositories,
		)
		if !slices.Equal(got, reference) {
			t.Fatalf("probe = %#v, NOT IN reference = %#v; the scope walk is not the complement of the grant", got, reference)
		}
	})

	// The read this walk replaced cost one index probe per granted repository
	// per producer entity, so a caller with a broad grant paid for the grant
	// rather than for the answer. These grants are the sizes that exposed it:
	// at 500 granted repositories the old shape took 633 ms on the corpus-scale
	// seed against 5.0 ms for this one. Correctness at those sizes is what is
	// asserted here; the timings are in the evidence doc.
	t.Run("a broad grant changes the answer for no entity", func(t *testing.T) {
		broad := make([]string, 0, 500)
		broad = append(broad, crossRepoDeadCodeProbeFanInRepositories...)
		for i := 0; len(broad) < 500; i++ {
			candidate := fmt.Sprintf("repo-pad%04d", i)
			if !slices.Contains(crossRepoDeadCodeProbeFanInRepositories, candidate) {
				broad = append(broad, candidate)
			}
		}
		for _, testCase := range []struct {
			name  string
			grant []string
			want  []string
		}{
			{name: "500 granted, every consumer among them", grant: broad},
			{
				name:  "500 granted, one consumer left out",
				grant: append(append([]string(nil), broad[:2]...), broad[3:]...),
				want:  []string{"ent-busy", "ent-middle", "ent-retained", "ent-spread"},
			},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(ctx, "repo-producer", page, testCase.grant)
				if err != nil {
					t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
				}
				got := make([]string, 0, len(hidden))
				for entityID := range hidden {
					got = append(got, entityID)
				}
				sort.Strings(got)
				if !slices.Equal(got, testCase.want) {
					t.Fatalf("hidden = %#v, want %#v", got, testCase.want)
				}
				reference := crossRepoDeadCodeProbeReference(ctx, t, db, "repo-producer", page, testCase.grant)
				if !slices.Equal(got, reference) {
					t.Fatalf("probe = %#v, NOT IN reference = %#v", got, reference)
				}
			})
		}
	})
}

// assertCrossRepoDeadCodeProbeIndexExists fails when the shipped migrations did
// not leave exactly the index the probe's walk seeks on at corpus scale: the
// four-column key built, and the two-column one it supersedes gone. A build
// that kept both would still answer correctly and would make every reachability
// write maintain a redundant btree, which no result assertion can see.
func assertCrossRepoDeadCodeProbeIndexExists(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	for indexName, want := range map[string]int{
		"code_reachability_entity_repository_scope_generation_idx": 1,
		"code_reachability_entity_repository_idx":                  0,
	} {
		var count int
		if err := db.QueryRowContext(
			ctx,
			"SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1",
			indexName,
		).Scan(&count); err != nil {
			t.Fatalf("look up %s: %v", indexName, err)
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", indexName, count, want)
		}
	}
}

// crossRepoDeadCodeProbeFanInRepositories are the consumer repositories the
// fan-in fixture spreads its rows across.
var crossRepoDeadCodeProbeFanInRepositories = []string{"repo-a", "repo-c", "repo-e", "repo-g", "repo-i"}

// crossRepoDeadCodeProbeFanOutRepositories are 200 distinct consumer
// repositories for one producer entity. They sort after every repository the
// grants in this test name, so a grant can leave all of them out.
var crossRepoDeadCodeProbeFanOutRepositories = func() []string {
	repositories := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		repositories = append(repositories, fmt.Sprintf("repo-x%03d", i))
	}
	return repositories
}()

// seedCrossRepoDeadCodeProbeFanIn gives one producer entity perRepositoryRows
// active-generation consumer rows in each of the named repositories. Row fan-in
// is the axis the walk must NOT be sensitive to: it visits each distinct
// consumer repository once and never looks at a second row of any of them.
func seedCrossRepoDeadCodeProbeFanIn(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryIDs []string,
	perRepositoryRows int,
) {
	t.Helper()

	for _, repositoryID := range repositoryIDs {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-active', $1, $1 || '#caller-' || lpad(i::text, 6, '0'), $2, 1,
       'reachable', 0.95, 'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $3) AS i`, repositoryID, entityID, perRepositoryRows); err != nil {
			t.Fatalf("seed fan-in rows for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze fan-in rows: %v", err)
	}
}

// crossRepoDeadCodeProbeHasLivenessSeek reports whether the plan applies the
// liveness lookup as a four-column index condition rather than leaving any of
// its columns to a filter.
func crossRepoDeadCodeProbeHasLivenessSeek(plan string) bool {
	for _, line := range strings.Split(plan, "\n") {
		if !strings.Contains(line, "Index Cond:") && !strings.Contains(line, "Recheck Cond:") {
			continue
		}
		full := true
		for _, column := range []string{"entity_id =", "repository_id =", "scope_id =", "generation_id ="} {
			if !strings.Contains(line, column) {
				full = false
				break
			}
		}
		if full {
			return true
		}
	}
	return false
}

// crossRepoDeadCodeProbeHasRepositorySkipSeek reports whether the plan applies
// the granted-repository skip as an index condition rather than a filter.
//
// That step's bound is a bare repository_id range under the entity equality,
// not the pair's row comparison, so it is the index condition that carries
// `repository_id >` without a ROW(). Left as a filter it would read the granted
// repository's rows one scope at a time, which is the cost the branch exists to
// remove and which no answer assertion can see.
func crossRepoDeadCodeProbeHasRepositorySkipSeek(plan string) bool {
	for _, line := range strings.Split(plan, "\n") {
		if !strings.Contains(line, "Index Cond:") && !strings.Contains(line, "Recheck Cond:") {
			continue
		}
		if strings.Contains(line, "ROW(") {
			continue
		}
		if strings.Contains(line, "repository_id >") {
			return true
		}
	}
	return false
}

// crossRepoDeadCodeProbeScopesPerRepository is how many ingestion scopes cover
// one repository in the scope fan-out fixtures. A repository ingested by more
// than one scope is ordinary -- a monorepo path scope beside a whole-repository
// scope, or a re-onboard that left the old scope in place -- and 50 is a
// deliberately unremarkable point on that range, chosen to be far enough above
// the walk's real step count that a shape which pays per scope cannot hide
// inside the noise.
const crossRepoDeadCodeProbeScopesPerRepository = 50

// crossRepoDeadCodeProbeScopeFanOutWalkRowBudget is the most rows the recursive
// term may produce for a single entity whose one granted consumer repository is
// covered by crossRepoDeadCodeProbeScopesPerRepository scopes and which has one
// hidden consumer past it.
//
// Measured, not guessed, the way proof row 38 requires: the shipped walk
// produces 2 -- the granted repository's first pair, then the hidden one --
// and a walk that steps pair by pair produces 51. 8 sits far enough above the
// real count to survive fixture edits and far enough below the broken one to
// fail on it.
const crossRepoDeadCodeProbeScopeFanOutWalkRowBudget = 8

// seedCrossRepoDeadCodeProbeGrantedScopeFanOut gives one producer entity a
// consumer in a GRANTED repository covered by many ingestion scopes -- each
// with its own active generation and its own live row -- and one live consumer
// in an ungranted repository sorting after it.
//
// Every pair in the granted repository is granted, so none of them can ever be
// hidden and the entity's verdict does not depend on how many there are. What
// depends on it is the number of steps: a walk that leaves a granted repository
// by seeking the next repository takes one step for it, and one that seeks the
// next pair takes one per scope.
func seedCrossRepoDeadCodeProbeGrantedScopeFanOut(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	grantedRepositoryID string,
	hiddenRepositoryID string,
	scopes int,
) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT 'scope-fanout-g-' || lpad(i::text, 4, '0'), 'gen-fanout-g-' || lpad(i::text, 4, '0')
FROM generate_series(1, $1) AS i
ON CONFLICT (scope_id) DO NOTHING`, scopes); err != nil {
		t.Fatalf("seed granted fan-out scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT 'gen-fanout-g-' || lpad(i::text, 4, '0'), 'scope-fanout-g-' || lpad(i::text, 4, '0'), 'active'
FROM generate_series(1, $1) AS i
ON CONFLICT (generation_id) DO NOTHING`, scopes); err != nil {
		t.Fatalf("seed granted fan-out generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-fanout-g-' || lpad(i::text, 4, '0'), 'gen-fanout-g-' || lpad(i::text, 4, '0'),
       $1, $1 || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $3) AS i`, grantedRepositoryID, entityID, scopes); err != nil {
		t.Fatalf("seed granted fan-out rows for %s in %s: %v", entityID, grantedRepositoryID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', 'gen-active', $1, $1 || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
        '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
		hiddenRepositoryID, entityID); err != nil {
		t.Fatalf("seed hidden consumer for %s in %s: %v", entityID, hiddenRepositoryID, err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze granted fan-out rows: %v", err)
	}
}

// seedCrossRepoDeadCodeProbeUngrantedScopeFanOut gives one producer entity a
// consumer in a single repository covered by many ingestion scopes whose rows
// are all superseded, optionally followed by one scope whose row is live.
//
// This is the case the granted skip must NOT be applied to. Any one of an
// ungranted repository's scopes can hold the live row, and here the live one is
// last, so a walk that left the repository after its first pair would answer
// "nothing hidden" for an entity that has a hidden consumer.
func seedCrossRepoDeadCodeProbeUngrantedScopeFanOut(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryID string,
	scopePrefix string,
	staleScopes int,
	live bool,
) {
	t.Helper()

	scopeName := "scope-fanout-" + scopePrefix + "-"
	activeGeneration := "gen-fanout-" + scopePrefix + "-active-"
	staleGeneration := "gen-fanout-" + scopePrefix + "-stale-"
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
SELECT $1 || lpad(i::text, 4, '0'), $2 || lpad(i::text, 4, '0')
FROM generate_series(1, $3) AS i
ON CONFLICT (scope_id) DO NOTHING`, scopeName, activeGeneration, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out scopes: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT $2 || lpad(i::text, 4, '0'), $1 || lpad(i::text, 4, '0'), 'active'
FROM generate_series(1, $3) AS i
ON CONFLICT (generation_id) DO NOTHING`, scopeName, activeGeneration, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out active generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT $2 || lpad(i::text, 4, '0'), $1 || lpad(i::text, 4, '0'), 'superseded'
FROM generate_series(1, $3) AS i
ON CONFLICT (generation_id) DO NOTHING`, scopeName, staleGeneration, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out superseded generations: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT $1 || lpad(i::text, 4, '0'), $2 || lpad(i::text, 4, '0'),
       $3, $3 || '#caller', $4, 1, 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $5) AS i`,
		scopeName, staleGeneration, repositoryID, entityID, staleScopes); err != nil {
		t.Fatalf("seed ungranted fan-out superseded rows for %s in %s: %v", entityID, repositoryID, err)
	}
	if live {
		// 9999 so this scope sorts after every stale one: the walk has to reach
		// the LAST scope of the repository to find the live row.
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, active_generation_id)
VALUES ($1 || '9999', $2 || '9999')
ON CONFLICT (scope_id) DO NOTHING`, scopeName, activeGeneration); err != nil {
			t.Fatalf("seed ungranted fan-out live scope: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
VALUES ($2 || '9999', $1 || '9999', 'active')
ON CONFLICT (generation_id) DO NOTHING`, scopeName, activeGeneration); err != nil {
			t.Fatalf("seed ungranted fan-out live generation: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ($1 || '9999', $2 || '9999', $3, $3 || '#caller', $4, 1, 'reachable', 0.95,
        'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
			scopeName, activeGeneration, repositoryID, entityID); err != nil {
			t.Fatalf("seed ungranted fan-out live row for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze ungranted fan-out rows: %v", err)
	}
}

// crossRepoDeadCodeProbeRetainedGenerations is how many superseded generations
// the retained-generation fixture keeps per consumer repository. The default
// retention policy keeps the 24 most recent superseded generations per scope
// plus everything superseded inside the last seven days
// (postgres.DefaultGenerationRetentionPolicy), so a scope resynced every few
// minutes holds far more than 24; 200 is a deliberately ordinary point on that
// range rather than a worst case.
const crossRepoDeadCodeProbeRetainedGenerations = 200

// crossRepoDeadCodeProbeRetainedBufferBudget is the most buffers the probe may
// touch answering for one producer entity whose every consumer repository also
// holds a row from each retained generation.
//
// Measured, not guessed, the way proof row 38 requires: with this fixture the
// shipped walk touches 24 buffers and the walk it replaced -- which scanned the
// group for its active row -- touches 5,946. 200 sits far enough above the real
// count to survive fixture edits and far enough below the broken one to fail on
// it.
const crossRepoDeadCodeProbeRetainedBufferBudget = 200

// crossRepoDeadCodeProbeBuffers reads the buffer count off the plan's root node.
func crossRepoDeadCodeProbeBuffers(t *testing.T, plan string) int {
	t.Helper()

	for _, line := range strings.Split(plan, "\n") {
		match := crossRepoDeadCodeProbeBufferLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		total := 0
		for _, group := range match[1:] {
			if group == "" {
				continue
			}
			count, err := strconv.Atoi(group)
			if err != nil {
				t.Fatalf("parse buffer count from %q: %v", strings.TrimSpace(line), err)
			}
			total += count
		}
		return total
	}
	t.Fatalf("no Buffers line in the plan; EXPLAIN was not asked for them:\n%s", plan)
	return 0
}

var crossRepoDeadCodeProbeBufferLine = regexp.MustCompile(`Buffers: shared hit=(\d+)(?: read=(\d+))?`)

// seedCrossRepoDeadCodeProbeRetainedGenerations gives one producer entity a row
// in each named repository for each of retained superseded generations, and
// then its one active-generation row.
//
// The superseded rows go in first on purpose. That is the order a real install
// writes them -- the active generation is the newest -- so the active row is the
// last heap tuple in its (entity_id, repository_id) group and the worst case for
// a step that stops on the first row it can use.
func seedCrossRepoDeadCodeProbeRetainedGenerations(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	entityID string,
	repositoryIDs []string,
	retained int,
) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, status)
SELECT 'gen-retained-' || lpad(i::text, 4, '0'), 'scope-1', 'superseded'
FROM generate_series(1, $1) AS i
ON CONFLICT (generation_id) DO NOTHING`, retained); err != nil {
		t.Fatalf("seed retained generations: %v", err)
	}
	for _, repositoryID := range repositoryIDs {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-retained-' || lpad(i::text, 4, '0'), $1, $1 || '#caller', $2, 1,
       'reachable', 0.95, 'symbol_exact', '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, $3) AS i`, repositoryID, entityID, retained); err != nil {
			t.Fatalf("seed retained-generation rows for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	for _, repositoryID := range repositoryIDs {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', 'gen-active', $1, $1 || '#caller', $2, 1, 'reachable', 0.95, 'symbol_exact',
        '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`, repositoryID, entityID); err != nil {
			t.Fatalf("seed active row for %s in %s: %v", entityID, repositoryID, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze retained-generation rows: %v", err)
	}
}

// crossRepoDeadCodeProbeRow is one seeded reachability row.
type crossRepoDeadCodeProbeRow struct {
	entityID     string
	repositoryID string
	depth        int
	generationID string
}

// seedCrossRepoDeadCodeProbeSchema creates the three tables the probe joins and
// the index it depends on. The index DDL is read from the shipped migration so
// a test cannot pass against an index the deployment does not build;
// CONCURRENTLY is stripped because this schema has no concurrent writer and
// CREATE INDEX CONCURRENTLY cannot run inside the test's statement batch.
func seedCrossRepoDeadCodeProbeSchema(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
  scope_id text PRIMARY KEY,
  active_generation_id text NULL
);
CREATE TABLE scope_generations (
  generation_id text PRIMARY KEY,
  scope_id text NOT NULL,
  status text NOT NULL
);
CREATE TABLE code_reachability_rows (
  scope_id text NOT NULL,
  generation_id text NOT NULL,
  repository_id text NOT NULL,
  root_entity_id text NOT NULL,
  entity_id text NOT NULL,
  depth integer NOT NULL,
  state text NOT NULL,
  confidence double precision NOT NULL,
  min_resolution_method text NOT NULL,
  evidence jsonb NOT NULL,
  root_kinds jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (scope_id, generation_id, repository_id, root_entity_id, entity_id)
);
INSERT INTO ingestion_scopes VALUES ('scope-1', 'gen-active');
INSERT INTO scope_generations VALUES ('gen-active', 'scope-1', 'active'), ('gen-stale', 'scope-1', 'superseded');
`); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}

	// Both index migrations, in the order a deployment applies them, so the
	// fixture ends where a real install ends: 101's four-column key built, and
	// 102 dropping the two-column index an earlier release built. Reading the
	// DDL from the shipped files is what stops a proof passing against an index
	// no deployment builds.
	for _, name := range crossRepoDeadCodeProbeIndexMigrations {
		migration, err := os.ReadFile("../storage/postgres/migrations/" + name)
		if err != nil {
			t.Fatalf("read the shipped index migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, strings.ReplaceAll(string(migration), "CONCURRENTLY ", "")); err != nil {
			t.Fatalf("apply the shipped index migration %s: %v", name, err)
		}
	}
}

// crossRepoDeadCodeProbeIndexMigrations are the index migrations the probe
// depends on, in migration order.
var crossRepoDeadCodeProbeIndexMigrations = []string{
	"101_code_reachability_entity_repository_scope_generation_idx.sql",
	"102_drop_code_reachability_entity_repository_idx.sql",
}

func seedCrossRepoDeadCodeProbeRows(ctx context.Context, t *testing.T, db *sql.DB, rows []crossRepoDeadCodeProbeRow) {
	t.Helper()

	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
VALUES ('scope-1', $1, $2, $3, $4, $5, 'reachable', 0.95, 'symbol_exact',
        '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now())`,
			row.generationID,
			row.repositoryID,
			row.repositoryID+"#caller",
			row.entityID,
			row.depth,
		); err != nil {
			t.Fatalf("seed reachability row %+v: %v", row, err)
		}
	}
	if _, err := db.ExecContext(ctx, "ANALYZE code_reachability_rows"); err != nil {
		t.Fatalf("analyze proof rows: %v", err)
	}
}

// crossRepoDeadCodeProbeReference answers the probe's question with the
// unseekable predicate the probe replaced.
func crossRepoDeadCodeProbeReference(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT DISTINCT row.entity_id
FROM code_reachability_rows AS row
JOIN ingestion_scopes AS scope
  ON scope.scope_id = row.scope_id
 AND scope.active_generation_id = row.generation_id
JOIN scope_generations AS generation
  ON generation.generation_id = row.generation_id
 AND generation.status = 'active'
WHERE row.entity_id = ANY($2)
  AND row.repository_id <> $1
  AND row.depth > 0
  AND NOT (row.repository_id = ANY($3))
ORDER BY row.entity_id`,
		producerRepoID,
		crossRepoDeadCodeProbeTextArray(entityIDs),
		crossRepoDeadCodeProbeTextArray(grantRepositoryIDs),
	)
	if err != nil {
		t.Fatalf("reference query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entities := make([]string, 0, len(entityIDs))
	for rows.Next() {
		var entityID string
		if err := rows.Scan(&entityID); err != nil {
			t.Fatalf("scan reference row: %v", err)
		}
		entities = append(entities, entityID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reference rows: %v", err)
	}
	return entities
}

// crossRepoDeadCodeProbeWalkRowBudget is the most rows the probe's recursive
// term may produce for this fixture's page plus ent-fanout. The shipped walk
// produces 15; with the stop condition removed it produces 215, because
// ent-fanout's 200 distinct consumer repositories are then all walked. 60 sits
// far enough above the real count to survive fixture edits and far enough
// below the broken one to fail on it.
//
// Measured, not guessed: both numbers came from running this guard with the
// budget temporarily set to 1, once against the shipped statement and once
// against the mutation, which is also proof row 38 needed -- an earlier budget
// of 900 sat above BOTH and would have passed the mutation it exists to catch.
const crossRepoDeadCodeProbeWalkRowBudget = 60

// crossRepoDeadCodeProbeWalkRows reads the row count the recursive walk term
// actually produced out of an EXPLAIN ANALYZE plan.
func crossRepoDeadCodeProbeWalkRows(t *testing.T, plan string) int {
	t.Helper()

	for _, line := range strings.Split(plan, "\n") {
		if !strings.Contains(line, "Recursive Union") {
			continue
		}
		match := crossRepoDeadCodeProbeActualRows.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		rows, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse walk row count from %q: %v", strings.TrimSpace(line), err)
		}
		return rows
	}
	t.Fatalf("no Recursive Union node in the plan; the probe shape has drifted:\n%s", plan)
	return 0
}

var crossRepoDeadCodeProbeActualRows = regexp.MustCompile(`actual time=[0-9.]+\.\.[0-9.]+ rows=(\d+) loops=`)

// crossRepoDeadCodeProbePlanMode is one of the two ways the planner can be
// asked about the probe.
//
// custom passes the values with the statement, which is what a one-shot
// EXPLAIN does. generic prepares the statement and forces a plan built without
// them, which is where pgx's statement cache puts these reads in production.
// The two disagree: the shape this walk replaced planned identically under
// custom and lost its Index Cond under generic, so a plan assertion that only
// runs the first mode proves nothing about what production executes.
type crossRepoDeadCodeProbePlanMode struct {
	name    string
	generic bool
}

var crossRepoDeadCodeProbePlanModes = []crossRepoDeadCodeProbePlanMode{
	{name: "custom plan"},
	{name: "generic plan", generic: true},
}

// crossRepoDeadCodeProbePlan runs the shipped probe under the given EXPLAIN
// prefix and plan mode, and returns the plan as text.
func crossRepoDeadCodeProbePlan(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	mode crossRepoDeadCodeProbePlanMode,
	prefix string,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) string {
	t.Helper()

	statement := prefix + crossRepoDeadCodeUngrantedConsumerProbeQuery
	args := []any{
		producerRepoID,
		crossRepoDeadCodeProbeTextArray(entityIDs),
		crossRepoDeadCodeProbeTextArray(grantRepositoryIDs),
		len(entityIDs),
	}
	if mode.generic {
		statement, args = crossRepoDeadCodeProbeGenericStatement(
			ctx, t, db, prefix, producerRepoID, entityIDs, grantRepositoryIDs,
		)
	}

	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		t.Fatalf("explain probe (%s): %v", mode.name, err)
	}
	defer func() { _ = rows.Close() }()

	plan := strings.Builder{}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	// A generic plan is built without the parameter values, so the producer
	// repository stays a parameter marker in the plan where a custom plan
	// inlines it as a literal. Checking that is what keeps this mode honest: a
	// refactor that quietly stopped forcing the mode would otherwise leave two
	// subtests asking the planner the same question twice.
	if mode.generic && !strings.Contains(plan.String(), "repository_id <> $1") {
		t.Fatalf("plan was not built generically -- the producer repository is not a parameter in it:\n%s", plan.String())
	}
	if !mode.generic && !strings.Contains(plan.String(), "repository_id <> 'repo-producer'::text") {
		t.Fatalf("plan was not built with the values in hand:\n%s", plan.String())
	}
	return plan.String()
}

// crossRepoDeadCodeProbeGenericStatement prepares the probe on the connection
// and forces a generic plan for it, returning an EXPLAIN of the EXECUTE with no
// bind parameters left.
//
// The values are rendered into the EXECUTE rather than bound, because under
// force_generic_plan the plan is already built without them -- which is the
// point. The test pool is pinned to one connection, so the SET, the PREPARE and
// the EXPLAIN all land on the same session; the cleanup puts both back.
func crossRepoDeadCodeProbeGenericStatement(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	prefix string,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) (string, []any) {
	t.Helper()

	name := fmt.Sprintf("cross_repo_dead_code_probe_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force a generic plan: %v", err)
	}
	// Registered before the PREPARE, not after it. The pool is pinned to one
	// connection, so a PREPARE that fails would t.Fatalf with no cleanup
	// registered and leave force_generic_plan set on that session for the rest
	// of the run. The two cleanups are separate for the same reason: a
	// DEALLOCATE of a statement the PREPARE never created is an error of its
	// own, and cleanups run last-registered-first, so this still deallocates
	// before it resets.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "RESET plan_cache_mode"); err != nil {
			t.Errorf("reset plan_cache_mode: %v", err)
		}
	})
	if _, err := db.ExecContext(
		ctx,
		"PREPARE "+name+"(text, text[], text[], int) AS "+crossRepoDeadCodeUngrantedConsumerProbeQuery,
	); err != nil {
		t.Fatalf("prepare the probe: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "DEALLOCATE "+name); err != nil {
			t.Errorf("deallocate the probe: %v", err)
		}
	})
	return fmt.Sprintf(
		"%sEXECUTE %s(%s, %s, %s, %d)",
		prefix,
		name,
		crossRepoDeadCodeProbeQuoteLiteral(producerRepoID),
		crossRepoDeadCodeProbeQuoteLiteral(crossRepoDeadCodeProbeTextArray(entityIDs)),
		crossRepoDeadCodeProbeQuoteLiteral(crossRepoDeadCodeProbeTextArray(grantRepositoryIDs)),
		len(entityIDs),
	), nil
}

// crossRepoDeadCodeProbeQuoteLiteral renders a SQL string literal for the
// EXECUTE above. The values are test-owned entity and repository ids, never
// caller input.
//
// The name carries the probe's prefix rather than the obvious quoteLiteral
// because this file has no build tag, so it joins every tagged build of this
// package -- including `integration`, where
// cloud_resource_runtime_digest_starvation_live_test.go already declares a
// quoteLiteral. Sharing that one would couple two unrelated live proofs through
// a build tag only one of them carries.
func crossRepoDeadCodeProbeQuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// crossRepoDeadCodeProbeTextArray renders a Postgres text[] literal for the
// helper statements above. The probe itself binds pgarray.Array; this exists so
// the reference query and the EXPLAIN take the same values without depending on
// the encoder under test.
func crossRepoDeadCodeProbeTextArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, `"`+strings.ReplaceAll(value, `"`, `\"`)+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}
