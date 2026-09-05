// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// POST /api/v0/code/dead-code/cross-repo bounds its consumer reads by what the
// request asked for, and that is what this file pins.
//
// A scoped caller that names no consumers gets two statements. The evidence
// page carries the grant, so its 1001-row cap falls on consumers the caller may
// see. The ungranted-consumer probe answers the other half -- is there a
// consumer this caller cannot see? -- and answers it per producer entity,
// bounded by that entity's own index seeks rather than by a shared row budget.
//
// A request that names consumers in consumer_repo_ids gets one statement, bound
// to those consumers. The row cap then falls where the question is: bound to the
// whole grant instead, a thousand rows from a repository the caller did not ask
// about filled the page and pushed the requested consumer off it, and the
// candidate came back unknown_needs_evidence for a symbol that consumer proves
// live. The probe is skipped for the same request, because every selector entry
// the grant admits is inside the grant -- there is nothing left for it to say,
// and not running it is what makes that structural.
//
// The count the probe contributes is one per producer entity that has an
// out-of-grant consumer, not one per such consumer. The classification only
// depends on whether there is one, and stopping at the first HIDDEN pair --
// ungranted AND live -- is what keeps the probe from reading a producer
// entity's whole fan-in group.
//
// This file holds the statement-shape pins. The probe's behavioural guards --
// the empty grant, the consumer selector, and per-entity coverage -- are in
// code_dead_code_cross_repo_probe_guards_test.go, and the classification order
// the two reads feed is in code_dead_code_cross_repo_classification_test.go.

// TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe pins both
// statements a scoped request sends. The page must carry the grant ahead of its
// LIMIT; the second must be the ungranted-consumer probe, with the grant bound
// as its own argument rather than left off the statement entirely.
func TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id"}},
	})
	reader := NewContentReader(db)
	if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"entity-1", "entity-2"},
		crossRepoDeadCodeConsumerReads{
			PageRepositoryIDs: []string{codeGrantConsumerRepo},
			SignalGrant:       []string{codeGrantConsumerRepo, codeGrantGrantedRepo},
		},
	); err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if len(recorder.queries) != 2 {
		t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted-consumer probe)", len(recorder.queries))
	}

	page := recorder.queries[0]
	grant := "AND row.repository_id = ANY($4)"
	if !strings.Contains(page, grant) {
		t.Fatalf("evidence page is missing %q, so its LIMIT is drawn from every tenant's rows:\n%s", grant, page)
	}
	if strings.Index(page, grant) > strings.Index(page, "LIMIT") {
		t.Fatalf("the grant sits after the LIMIT, so the page is still cut from a mixed set:\n%s", page)
	}

	probe := recorder.queries[1]
	if probe != crossRepoDeadCodeUngrantedConsumerProbeQuery {
		t.Fatalf("second statement is not the ungranted-consumer probe:\n%s", probe)
	}
	// The whole point of the probe is that it stops early, and it stops early
	// because it walks one producer entity's distinct (repository_id, scope_id)
	// PAIRS in index order and quits at the first pair that is both outside the
	// grant and live. Each piece of that is pinned: the recursive walk, the two
	// per-step seeks and the gate that picks between them, the one-row limits
	// that keep each seek a seek, and the continue-condition that ends the
	// walk.
	for _, want := range []string{
		"WITH RECURSIVE page AS (",
		"AND (row.repository_id, row.scope_id) > (walk.repository_id, walk.scope_id)",
		"ORDER BY row.repository_id, row.scope_id\n        LIMIT 1) AS first_pair) AS pair) AS seed",
		"WHERE granted.repository_id = first_pair.repository_id) AS is_granted",
		"WHERE granted.repository_id = next_pair.repository_id) AS is_granted",
		"NOT pair.is_granted",
		"WHERE NOT walk.hidden",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe is missing %q, so it can no longer stop at the first hidden pair:\n%s", want, probe)
		}
	}
	// A granted repository costs ONE step however many ingestion scopes cover
	// it, and that is only true while the step from a granted pair seeks the
	// next REPOSITORY rather than the next pair. Both gated branches are
	// pinned: without them a repository with fifty scopes costs fifty steps, so
	// the walk passes more granted PAIRS than the grant has repositories and the
	// min(d, N) half of its bound stops holding, with every answer unchanged.
	for _, want := range []string{
		"AND walk.is_granted\n           AND row.repository_id > walk.repository_id",
		"AND NOT walk.is_granted\n           AND (row.repository_id, row.scope_id) > (walk.repository_id, walk.scope_id)",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe is missing %q, so a granted repository costs one step per ingestion scope instead of one:\n%s", want, probe)
		}
	}
	if got, want := strings.Count(probe, "ORDER BY row.repository_id, row.scope_id"), 3; got != want {
		t.Fatalf("probe has %d ordered seeks, want %d (the walk's seed and its two gated steps)", got, want)
	}
	// The liveness test is what makes a step independent of how many
	// superseded generations the retention runner still keeps, and it only is
	// that if all four key columns are equalities against the pair the walk
	// just found. Losing any one of them leaves the generation a filter over
	// the pair's retained rows and the answer unchanged, so nothing else here
	// can see it.
	//
	// The generation has to arrive as a SCALAR SUBQUERY rather than as a join
	// to ingestion_scopes on the outer row, and that is the load-bearing half.
	// Written as a join the planner may reorder it, and on a corpus with one
	// ingestion scope per consumer repository it does: it drops generation_id
	// out of the Index Cond, seeks three columns, and probes scope_generations
	// once per retained row. The plan a small fixture gets is not the plan a
	// corpus gets -- the liveness lookup took three different shapes across the
	// corpora measured in the walk note -- so this pin, not a plan assertion,
	// is what holds the seek.
	for _, want := range []string{
		"AND live_row.repository_id = pair.repository_id",
		"AND live_row.scope_id = pair.scope_id",
		"AND live_row.generation_id = (",
		"SELECT scope.active_generation_id",
		"WHERE scope.scope_id = pair.scope_id)",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe is missing %q, so a step scans a pair's retained generations instead of seeking its active row:\n%s", want, probe)
		}
	}
	// The join form is what this replaced. Its absence is the assertion: with
	// it back, the equality above is satisfied by text the planner is free to
	// reorder into the scan this seek exists to remove.
	if strings.Contains(probe, "JOIN code_reachability_rows AS live_row") {
		t.Fatalf("probe joins the liveness row on the outer pair; the planner may then reorder the generation out of the Index Cond:\n%s", probe)
	}
	// A bound rendered per granted repository is what this shape replaced: it
	// cost one index probe per granted repository per producer entity, so a
	// broad grant scaled the read linearly. Nothing here may reintroduce one.
	for _, forbidden := range []string{"gap.lo", "gap.hi", "grant_bounds", "lag(repository_id)"} {
		if strings.Contains(probe, forbidden) {
			t.Fatalf("probe carries %q, a per-granted-repository bound; its cost then grows with the caller's grant:\n%s", forbidden, probe)
		}
	}
	if strings.Contains(probe, "row.confidence") || strings.Contains(probe, "row.evidence") {
		t.Fatalf("probe selects consumer evidence columns; it must answer whether, never which:\n%s", probe)
	}
	if got, want := len(recorder.args[1]), 4; got != want {
		t.Fatalf("len(args) = %d, want %d (producer repo, entity array, grant array, page size)", got, want)
	}
	if got, want := fmt.Sprintf("%v", recorder.args[1][3]), "2"; got != want {
		t.Fatalf("probe LIMIT argument = %v, want %v (one row per producer entity at most)", got, want)
	}
}

// TestCrossRepoDeadCodeProbeStatementIsSizeIndependent is why the probe binds
// arrays instead of rendering one placeholder per entity: the statement text is
// the same for every page and every grant, so a page of 250 candidates and a
// page of 2 plan as one statement rather than two.
func TestCrossRepoDeadCodeProbeStatementIsSizeIndependent(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id"}},
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id"}},
	})
	reader := NewContentReader(db)
	read := func(entityIDs []string, grant []string) {
		t.Helper()
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			entityIDs,
			crossRepoDeadCodeConsumerReads{PageRepositoryIDs: grant, SignalGrant: grant},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
	}
	read([]string{"entity-1"}, []string{codeGrantConsumerRepo})
	read(
		[]string{"entity-1", "entity-2", "entity-3"},
		[]string{codeGrantConsumerRepo, codeGrantGrantedRepo, codeGrantOtherRepo},
	)
	if recorder.queries[1] != recorder.queries[3] {
		t.Fatalf("probe text changed with the page and grant size:\nfirst:\n%s\nsecond:\n%s", recorder.queries[1], recorder.queries[3])
	}
}

// crossRepoDeadCodeConsumerPageRankMigration is the shipped migration whose
// index the evidence page's ORDER BY depends on.
const crossRepoDeadCodeConsumerPageRankMigration = "../storage/postgres/migrations/103_code_reachability_entity_confidence_rank_idx.sql"

// TestCrossRepoDeadCodeConsumerPageOrderMatchesItsIndexKey pins the evidence
// page's ORDER BY and migration 103's index key to each other, by reading both
// (#6527).
//
// They are one decision written in two places. With entity_id pinned by the
// statement's IN list the index's key columns ARE the ordering, so the scan is
// already in output order and the LIMIT stops it. Edit either alone and the
// LIMIT silently goes back to bounding only what comes back: Postgres has to
// rank a producer entity's whole consumer fan-in before it can emit that
// entity's first row, which is 1,000,497 rows read for a 1,001-row answer on
// the corpus in docs/internal/evidence/5167-cross-repo-consumer-page-bound.md.
//
// Nothing else can catch that drift. The answer is identical either way, so no
// behavioural assertion moves; the live proof sees it, but only against a real
// Postgres, and this runs in the unit lane on every change to either file.
func TestCrossRepoDeadCodeConsumerPageOrderMatchesItsIndexKey(t *testing.T) {
	t.Parallel()

	query, _ := buildCrossRepoDeadCodeConsumerEvidenceQuery(
		codeGrantGrantedRepo, []string{"entity-1"}, []string{codeGrantConsumerRepo},
	)
	order := crossRepoDeadCodeConsumerPageOrderColumns(t, query)
	key := crossRepoDeadCodeConsumerPageIndexKeyColumns(t)
	// Two parsers that both returned nothing would agree, and this test would
	// pass for the wrong reason. The ranking column is what the whole change is
	// about, so require the parse to have found it and the four columns that
	// break its ties.
	if strings.Count(order, ",") != 4 || !strings.Contains(order, "confidence DESC") {
		t.Fatalf("read the page's ORDER BY as (%s), want five columns including confidence DESC; the parse has drifted from the statement", order)
	}
	if order != key {
		t.Fatalf("the evidence page orders by (%s) and migration 103's index key is (%s); they have to be the same columns in the same order, or the page ranks a producer entity's whole fan-in before its LIMIT",
			order, key)
	}
}

// crossRepoDeadCodeConsumerPageOrderColumns renders the page statement's ORDER
// BY as a comparable column list: the table alias dropped, ASC dropped as the
// default, DESC kept because it is part of the index key.
func crossRepoDeadCodeConsumerPageOrderColumns(t *testing.T, query string) string {
	t.Helper()

	_, after, found := strings.Cut(query, "\nORDER BY ")
	if !found {
		t.Fatalf("the evidence page statement has no ORDER BY:\n%s", query)
	}
	clause, _, found := strings.Cut(after, "\nLIMIT ")
	if !found {
		t.Fatalf("the evidence page statement has no LIMIT after its ORDER BY:\n%s", query)
	}
	columns := make([]string, 0, 5)
	for _, column := range strings.Split(clause, ",") {
		column = strings.Join(strings.Fields(column), " ")
		column = strings.TrimPrefix(column, "row.")
		column = strings.TrimSuffix(column, " ASC")
		columns = append(columns, column)
	}
	return strings.Join(columns, ", ")
}

// crossRepoDeadCodeConsumerPageIndexKeyColumns reads migration 103's key column
// list off the shipped file. Reading the file rather than repeating the columns
// here is the point: a pin that restated them would agree with itself while the
// deployment built something else.
func crossRepoDeadCodeConsumerPageIndexKeyColumns(t *testing.T) string {
	t.Helper()

	migration, err := os.ReadFile(crossRepoDeadCodeConsumerPageRankMigration)
	if err != nil {
		t.Fatalf("read the shipped page-rank index migration: %v", err)
	}
	statement := ""
	for _, line := range strings.Split(string(migration), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		statement += " " + line
	}
	_, after, found := strings.Cut(statement, "ON code_reachability_rows (")
	if !found {
		t.Fatalf("migration 103 does not index code_reachability_rows:\n%s", statement)
	}
	key, _, found := strings.Cut(after, ")")
	if !found {
		t.Fatalf("migration 103's index key list is unterminated:\n%s", statement)
	}
	columns := make([]string, 0, 5)
	for _, column := range strings.Split(key, ",") {
		columns = append(columns, strings.Join(strings.Fields(column), " "))
	}
	return strings.Join(columns, ", ")
}
