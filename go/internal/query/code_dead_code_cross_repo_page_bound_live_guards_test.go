// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The guards TestCrossRepoDeadCodeConsumerEvidencePageBoundLive runs, and the
// EXPLAIN plumbing they share. They live beside the driver rather than in it so
// neither file approaches the 500-line cap.

// runCrossRepoDeadCodeConsumerPageIndexGuard fails when the shipped migrations
// did not leave the page's ordering index behind with its key columns in the
// order the statement asks for. Key order is the whole claim: the same five
// columns in any other order still answers correctly and still makes the read
// rank the group first.
func runCrossRepoDeadCodeConsumerPageIndexGuard(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	t.Run("the migrations leave the page's ordering index", func(t *testing.T) {
		var definition string
		err := db.QueryRowContext(
			ctx,
			"SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1",
			crossRepoDeadCodeConsumerPageRankIndex,
		).Scan(&definition)
		if err != nil {
			t.Fatalf("look up %s: %v", crossRepoDeadCodeConsumerPageRankIndex, err)
		}
		const wantKey = "(entity_id, confidence DESC, depth, repository_id, root_entity_id)"
		if !strings.Contains(definition, wantKey) {
			t.Fatalf("%s is defined as %q, want its key to be %s -- the page's ORDER BY with entity_id pinned by the IN list",
				crossRepoDeadCodeConsumerPageRankIndex, definition, wantKey)
		}
	})
}

// runCrossRepoDeadCodeConsumerPageAnswerGuard reads the page through the
// shipped reader and requires the rows and the per-entity truncation marker the
// route contracts for.
func runCrossRepoDeadCodeConsumerPageAnswerGuard(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
) {
	t.Helper()

	t.Run("the page's answer is unchanged", func(t *testing.T) {
		reader := NewContentReader(db)
		evidence, hidden, err := reader.CrossRepoDeadCodeConsumerEvidence(
			ctx, "repo-producer", page,
			crossRepoDeadCodeConsumerReads{PageRepositoryIDs: crossRepoDeadCodeConsumerPageHotRepositories},
		)
		if err != nil {
			t.Fatalf("read cross-repo consumer evidence: %v", err)
		}
		if len(hidden) != 0 {
			t.Fatalf("hidden consumers = %d, want 0; no probe was asked for", len(hidden))
		}
		// The ordinary entities sort before the busy one, so the read returns
		// every row they have and then spends the rest of its cap inside
		// ent-hot. Each has one consumer row per repository, and none of them
		// can be truncated.
		perOrdinary := len(crossRepoDeadCodeConsumerPageHotRepositories)
		rows := 0
		for _, entityID := range page {
			items := evidence[entityID]
			truncated := 0
			for _, item := range items {
				if item.Reason == "consumer_evidence_truncated" {
					truncated++
					continue
				}
				rows++
			}
			switch entityID {
			case "ent-hot":
				if truncated != 1 {
					t.Errorf("ent-hot carries %d truncation markers, want 1; its fan-in cannot fit the page", truncated)
				}
			default:
				if truncated != 0 {
					t.Errorf("%s carries %d truncation markers, want 0; the read moved past it", entityID, truncated)
				}
				if len(items) != perOrdinary {
					t.Errorf("%s has %d consumer rows, want %d", entityID, len(items), perOrdinary)
				}
			}
		}
		if rows != maxCrossRepoDeadCodeConsumerEvidenceRows {
			t.Errorf("the page returned %d consumer rows, want %d", rows, maxCrossRepoDeadCodeConsumerEvidenceRows)
		}
	})
}

// runCrossRepoDeadCodeConsumerPageWorkGuard counts the rows the plan's
// reachability scan read. It is the guard the issue exists for: without
// migration 103 the answer above is identical and the scan reads the busy
// entity's whole group to produce it.
func runCrossRepoDeadCodeConsumerPageWorkGuard(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
) {
	t.Helper()

	t.Run("the page stops at its own LIMIT", func(t *testing.T) {
		for _, mode := range crossRepoDeadCodeProbePlanModes {
			t.Run(mode.name, func(t *testing.T) {
				plan := crossRepoDeadCodeConsumerPagePlan(ctx, t, db, mode, page)
				// The work first, the reason second. The rows read are the
				// claim; which index produced them is the explanation, and a
				// failure that leads with the number says what went wrong
				// rather than what is missing.
				scanned := crossRepoDeadCodeConsumerPageScanRows(t, plan)
				if scanned > crossRepoDeadCodeConsumerPageScanRowBudget {
					t.Errorf("the page read scanned %d code_reachability_rows rows, want at most %d; it is bounded by the entity's fan-in rather than by its LIMIT",
						scanned, crossRepoDeadCodeConsumerPageScanRowBudget)
				}
				if !strings.Contains(plan, crossRepoDeadCodeConsumerPageRankIndex) {
					t.Fatalf("the page read does not use %s, so it is ranking the group before its LIMIT:\n%s",
						crossRepoDeadCodeConsumerPageRankIndex, plan)
				}
			})
		}
	})
}

// crossRepoDeadCodeConsumerPagePlan runs the shipped page statement under
// EXPLAIN (ANALYZE) in the given plan mode and returns the plan as text.
func crossRepoDeadCodeConsumerPagePlan(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	mode crossRepoDeadCodeProbePlanMode,
	page []string,
) string {
	t.Helper()

	const prefix = "EXPLAIN (ANALYZE) "
	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery(
		"repo-producer", page, crossRepoDeadCodeConsumerPageHotRepositories,
	)
	statement := prefix + query
	if mode.generic {
		statement, args = crossRepoDeadCodeConsumerPageGenericStatement(ctx, t, db, prefix, query, page)
	}

	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		t.Fatalf("explain the page read (%s): %v", mode.name, err)
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
	// The same honesty check the probe's plumbing makes: a generic plan leaves
	// the producer repository a parameter marker where a custom plan inlines
	// it, so a refactor that stopped forcing the mode cannot leave two subtests
	// asking the planner the same question twice.
	if mode.generic && !strings.Contains(plan.String(), "repository_id <> $1") {
		t.Fatalf("plan was not built generically -- the producer repository is not a parameter in it:\n%s", plan.String())
	}
	if !mode.generic && !strings.Contains(plan.String(), "repository_id <> 'repo-producer'::text") {
		t.Fatalf("plan was not built with the values in hand:\n%s", plan.String())
	}
	return plan.String()
}

// crossRepoDeadCodeConsumerPageGenericStatement prepares the page statement on
// the connection and forces a plan built without its values, returning an
// EXPLAIN of an EXECUTE with nothing left to bind.
//
// The statement takes one text parameter for the producer repository, one per
// entity id, and a trailing text[] for the consumer-repository list, so the
// PREPARE's parameter list is built from the page rather than written out.
func crossRepoDeadCodeConsumerPageGenericStatement(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	prefix string,
	query string,
	page []string,
) (string, []any) {
	t.Helper()

	name := fmt.Sprintf("cross_repo_dead_code_page_%d", time.Now().UnixNano())
	types := make([]string, 0, len(page)+2)
	values := make([]string, 0, len(page)+2)
	types = append(types, "text")
	values = append(values, crossRepoDeadCodeProbeQuoteLiteral("repo-producer"))
	for _, entityID := range page {
		types = append(types, "text")
		values = append(values, crossRepoDeadCodeProbeQuoteLiteral(entityID))
	}
	types = append(types, "text[]")
	values = append(values, crossRepoDeadCodeProbeQuoteLiteral(
		crossRepoDeadCodeProbeTextArray(crossRepoDeadCodeConsumerPageHotRepositories)))

	if _, err := db.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force a generic plan: %v", err)
	}
	// Registered before the PREPARE, and separately from it, for the reason
	// the probe's plumbing records: the pool is pinned to one connection, so a
	// failed PREPARE must still leave plan_cache_mode reset, and a DEALLOCATE
	// of a statement that was never created is an error of its own.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "RESET plan_cache_mode"); err != nil {
			t.Errorf("reset plan_cache_mode: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, "PREPARE "+name+"("+strings.Join(types, ", ")+") AS "+query); err != nil {
		t.Fatalf("prepare the page read: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "DEALLOCATE "+name); err != nil {
			t.Errorf("deallocate the page read: %v", err)
		}
	})
	return prefix + "EXECUTE " + name + "(" + strings.Join(values, ", ") + ")", nil
}

// crossRepoDeadCodeConsumerPageScanRowPattern reads the rows and loop count off
// the plan node that scans code_reachability_rows.
var crossRepoDeadCodeConsumerPageScanRowPattern = regexp.MustCompile(
	`on code_reachability_rows[^\n]*actual time=[0-9.]+\.\.[0-9.]+ rows=(\d+) loops=(\d+)`)

// crossRepoDeadCodeConsumerPageScanRows totals the rows every scan of
// code_reachability_rows in the plan actually read.
func crossRepoDeadCodeConsumerPageScanRows(t *testing.T, plan string) int {
	t.Helper()

	matches := crossRepoDeadCodeConsumerPageScanRowPattern.FindAllStringSubmatch(plan, -1)
	if len(matches) == 0 {
		t.Fatalf("no plan node scans code_reachability_rows; the page statement's shape has drifted:\n%s", plan)
	}
	total := 0
	for _, match := range matches {
		rows, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse scanned rows from %q: %v", match[0], err)
		}
		loops, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatalf("parse scan loops from %q: %v", match[0], err)
		}
		total += rows * loops
	}
	return total
}
