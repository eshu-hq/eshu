// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// The plan half of TestCrossRepoDeadCodeUngrantedConsumerProbeLive: the
// assertions that read the plan rather than the answer, because a walk whose
// per-step lookup lands in a filter still returns the right entities and reads
// the producer entity's whole fan-in to do it.

// runCrossRepoDeadCodeProbeSeekGuard requires every seek the walk depends on to
// reach an index condition.
func runCrossRepoDeadCodeProbeSeekGuard(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
) {
	t.Helper()

	t.Run("the walk seeks and never scans a group", func(t *testing.T) {
		// This is the property the shape rests on, and no behavioural
		// assertion above can see it: a walk whose per-step lookup lands in a
		// filter instead of an index condition still returns the right
		// entities, and reads the producer entity's whole fan-in to do it.
		//
		// Which index the planner picks is left alone deliberately; the index
		// the migrations must leave behind is asserted separately, and the plan
		// it produces at corpus scale is measured in
		// docs/internal/evidence/5167-cross-repo-hidden-consumer-walk.md.
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
