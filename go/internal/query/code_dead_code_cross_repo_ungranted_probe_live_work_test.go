// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The work half of TestCrossRepoDeadCodeUngrantedConsumerProbeLive: the guards
// that measure what the walk did rather than what it answered. Each of the
// mutations they exist for leaves every entity's verdict correct, so no
// assertion in the sibling _answer_test.go file can see them.

// runCrossRepoDeadCodeProbeStopCondition bounds the rows the recursive term
// produces for a page carrying a wide fan-out entity.
func runCrossRepoDeadCodeProbeStopCondition(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
) {
	t.Helper()

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
}

// runCrossRepoDeadCodeProbeRetainedGenerationCost bounds the buffers one entity
// costs when every consumer repository also holds each retained generation.
func runCrossRepoDeadCodeProbeRetainedGenerationCost(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

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
}

// runCrossRepoDeadCodeProbeGrantedScopeCost bounds the steps a granted
// repository costs however many ingestion scopes cover it.
func runCrossRepoDeadCodeProbeGrantedScopeCost(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

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
}

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
