// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The drain breakdown names a dead-lettered row's failure CLASS
// ("projection_bug") and its count. A class is a triage bucket, not the error.
// The gate tears down Postgres and its temp dir on exit, and the service log
// dumps are a 30-line tail that a real failure spent entirely on INFO chatter,
// so once the run ends the actual error text is gone. A genuine reducer bug and
// a machine-contention timeout print the same line, and telling them apart
// afterwards costs an hour (#6306).
//
// fact_work_items already stores failure_message. These tests pin that the
// breakdown carries it, bounded, and that nothing about the drain's verdict
// moves as a result.

func TestResidualBreakdownPrintsFailureMessage(t *testing.T) {
	t.Parallel()

	rows := []residualRow{{
		Domain:         "code_function_summary",
		Status:         "dead_letter",
		FailureClass:   "projection_bug",
		Count:          1,
		FailureMessage: "reduce intent: value-flow fixpoint: node not found",
	}}
	got := formatResidualBreakdown(rows)

	if !strings.Contains(got, "reduce intent: value-flow fixpoint: node not found") {
		t.Errorf("breakdown does not carry the failure message: %s", got)
	}
}

// The residual can be in the hundreds. Printing one message per group with a
// group cap is what keeps a 624-row residual from dumping 624 messages into a
// CI log, so the cap is the property, not an implementation detail.
func TestResidualBreakdownCapsTheNumberOfMessageGroups(t *testing.T) {
	t.Parallel()

	rows := make([]residualRow, 0, maxResidualMessageGroups+2)
	for _, d := range []string{"d1", "d2", "d3", "d4", "d5", "d6"} {
		rows = append(rows, residualRow{
			Domain: d, Status: "dead_letter", FailureClass: "projection_bug", Count: 1,
			FailureMessage: "unique failure for " + d,
		})
	}
	got := formatResidualBreakdown(rows)

	printed := 0
	for _, d := range []string{"d1", "d2", "d3", "d4", "d5", "d6"} {
		if strings.Contains(got, "unique failure for "+d) {
			printed++
		}
	}
	if printed > maxResidualMessageGroups {
		t.Errorf("printed %d messages, cap is %d: %s", printed, maxResidualMessageGroups, got)
	}
	if printed != maxResidualMessageGroups {
		t.Errorf("printed %d messages, want the full cap of %d: %s", printed, maxResidualMessageGroups, got)
	}
	// Silently dropping the rest reads as "those groups had no message".
	if !strings.Contains(got, "more group") {
		t.Errorf("dropped message groups without saying so: %s", got)
	}
	// Every group's COUNT still appears — the cap bounds messages, not counts.
	for _, d := range []string{"d1", "d2", "d3", "d4", "d5", "d6"} {
		if !strings.Contains(got, d+"/dead_letter/projection_bug=1") {
			t.Errorf("group %s lost its count: %s", d, got)
		}
	}
}

// A Go error chain can run to kilobytes (a wrapped Cypher statement, a whole
// SQL string). One of those per group would bury the counts it sits next to.
func TestResidualBreakdownTruncatesALongFailureMessage(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 5000)
	rows := []residualRow{{
		Domain: "repo_dependency", Status: "dead_letter", FailureClass: "projection_bug",
		Count: 1, FailureMessage: long,
	}}
	got := formatResidualBreakdown(rows)

	if strings.Contains(got, strings.Repeat("x", residualMessageMaxLen+1)) {
		t.Errorf("message not truncated to %d chars: %d chars of output", residualMessageMaxLen, len(got))
	}
	if !strings.Contains(got, strings.Repeat("x", residualMessageMaxLen)) {
		t.Errorf("truncated below the %d-char budget: %s", residualMessageMaxLen, got)
	}
	if !strings.Contains(got, residualMessageTruncationMarker) {
		t.Errorf("truncated without marking it, so the reader cannot tell: %s", got)
	}
}

// failure_message is err.Error() from a reducer handler, and Go errors carry
// newlines. Two separate things keep that from wrecking the gate's own
// line-oriented output, and each needs its own assertion or one can be removed
// while the other keeps the test green:
//
//   - printing through %q means a raw newline can never reach the log, so error
//     text cannot emit what looks like another gate line ("[FAIL] ...") and make
//     the run misreport itself;
//   - flattening first means the message does not arrive as a wall of "\n"
//     escapes, which %q alone would happily print and which spend the 200-rune
//     budget on punctuation instead of on the cause.
func TestResidualBreakdownFlattensNewlinesInFailureMessage(t *testing.T) {
	t.Parallel()

	rows := []residualRow{{
		Domain: "repo_dependency", Status: "dead_letter", FailureClass: "projection_bug",
		Count: 1, FailureMessage: "outer failure\n[FAIL] forged: not a real check\r\ntail",
	}}
	got := formatResidualBreakdown(rows)

	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("breakdown emitted a raw line break from error text: %q", got)
	}
	// The two-character escape sequence, not a line break: this is the half only
	// flattening removes.
	if strings.Contains(got, `\n`) || strings.Contains(got, `\r`) {
		t.Errorf("breakdown printed escaped line breaks instead of flattening them: %s", got)
	}
	if !strings.Contains(got, "outer failure") || !strings.Contains(got, "tail") {
		t.Errorf("flattening dropped message content: %s", got)
	}
}

// This change is diagnostics only. The drain's verdict comes from DrainCounts,
// which never sees a residualRow -- but the summary and per-group counts this
// function renders are the same text a reader compares across runs, so pin that
// adding messages moves none of it.
func TestResidualBreakdownCountsAreUnchangedByFailureMessages(t *testing.T) {
	t.Parallel()

	without := []residualRow{
		{Domain: "repo_dependency", Status: "pending", Count: 2},
		{Domain: "aws_cloud_runtime_drift", Status: "retrying", FailureClass: "aws_cloud_runtime_drift_state_pending", Count: 3},
		{Domain: "code_function_summary", Status: "dead_letter", FailureClass: "projection_bug", Count: 1},
	}
	with := make([]residualRow, len(without))
	copy(with, without)
	for i := range with {
		with[i].FailureMessage = "some error text for row " + with[i].Domain
	}

	base := formatResidualBreakdown(without)
	enriched := formatResidualBreakdown(with)

	if !strings.HasPrefix(enriched, base) {
		t.Fatalf("messages rewrote the existing breakdown:\n base = %s\n with = %s", base, enriched)
	}
}

// The gate reads these rows from Postgres. The formatter can only print a
// message the query actually selected, so the SELECT is half the fix and needs
// its own guard: without this, reverting the column leaves every formatter test
// green and the live gate silently back to printing counts alone.
func TestResidualBreakdownSQLSelectsTheFailureMessage(t *testing.T) {
	t.Parallel()

	if !strings.Contains(residualBreakdownSQL, "failure_message") {
		t.Errorf("residual breakdown query does not select failure_message:\n%s", residualBreakdownSQL)
	}
	// Bounded in the database too, so a pathological group never crosses the
	// wire in full.
	if !strings.Contains(residualBreakdownSQL, "string_agg(DISTINCT") {
		t.Errorf("residual breakdown query does not collapse messages per group:\n%s", residualBreakdownSQL)
	}
	if !strings.Contains(residualBreakdownSQL, "left(") {
		t.Errorf("residual breakdown query does not bound the message length:\n%s", residualBreakdownSQL)
	}
}

// A group can hold several distinct causes — a contention timeout and a real
// reducer defect can share a domain, status, and failure class. Without a sort
// inside the aggregate, which of them survives the printed budget is
// unspecified, so two runs over identical data can blame different things. That
// is worse than blaming nothing, and no formatter test can catch it.
func TestResidualBreakdownSQLOrdersMessagesDeterministically(t *testing.T) {
	t.Parallel()

	aggregate := residualMessageAggregateSQL()
	if !strings.Contains(aggregate, " ORDER BY ") {
		t.Fatalf("message aggregate has no sort, so its concatenation order is unspecified:\n%s", aggregate)
	}
	// Postgres rejects an ordinal here ("in an aggregate with DISTINCT, ORDER BY
	// expressions must appear in argument list"), so the sort must repeat the
	// column expression verbatim. Counting the occurrences pins that: one as the
	// aggregate's argument, one as its sort key.
	if got := strings.Count(aggregate, residualMessageColumnSQL); got != 2 {
		t.Errorf("column expression appears %d times, want 2 (argument and sort key):\n%s", got, aggregate)
	}
}

// The formatter can only mark a message as cut if it can SEE that it was cut.
// Fetching exactly the printed budget hands it a message that is
// indistinguishable from one that ended there, and every long error then prints
// as a complete short one.
func TestResidualBreakdownSQLFetchesMoreThanItPrints(t *testing.T) {
	t.Parallel()

	if residualMessageFetchLen <= residualMessageMaxLen {
		t.Fatalf("query fetches %d runes but prints %d: a database-side cut is undetectable",
			residualMessageFetchLen, residualMessageMaxLen)
	}
	if !strings.Contains(residualBreakdownSQL, fmt.Sprintf("%d", residualMessageFetchLen)) {
		t.Errorf("query does not carry the %d-rune fetch bound:\n%s", residualMessageFetchLen, residualBreakdownSQL)
	}
	// The cut happens after the whitespace collapse. The other order lets a Go
	// flatten shrink an already-cut message back under the budget, where it
	// prints unmarked.
	if !strings.Contains(residualMessageColumnSQL, "regexp_replace") {
		t.Errorf("query does not normalize whitespace before cutting:\n%s", residualMessageColumnSQL)
	}
	if strings.Index(residualMessageColumnSQL, "left(") > strings.Index(residualMessageColumnSQL, "regexp_replace") {
		t.Errorf("query cuts before it normalizes, so a cut can be hidden by flattening:\n%s", residualMessageColumnSQL)
	}
}

// The live differential rebuilds the pre-message query from
// residualBreakdownCountsSQL. That only proves anything while the shipped query
// really starts with those same four columns — a hand-frozen copy that drifted
// would pass a differential against itself.
func TestResidualBreakdownCountsSQLIsAPrefixOfTheShippedQuery(t *testing.T) {
	t.Parallel()

	if !strings.HasPrefix(residualBreakdownSQL, residualBreakdownColumnsSQL+",") {
		t.Errorf("shipped query does not begin with the four pre-message columns:\n%s", residualBreakdownSQL)
	}
	if !strings.HasSuffix(residualBreakdownSQL, residualBreakdownScopeSQL) {
		t.Errorf("shipped query does not end with the shared predicate/grouping/order:\n%s", residualBreakdownSQL)
	}
	if !strings.HasPrefix(residualBreakdownCountsSQL(), residualBreakdownColumnsSQL) ||
		!strings.HasSuffix(residualBreakdownCountsSQL(), residualBreakdownScopeSQL) {
		t.Errorf("reference query is not derived from the shipped halves:\n%s", residualBreakdownCountsSQL())
	}
}

// The residual predicate and grouping are the drain contract (AGENTS.md), and
// the count column is what EvaluateDrains's number is reconciled against.
// Adding a message column must not touch either.
func TestResidualBreakdownSQLKeepsTheDrainPredicateAndGrouping(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WHERE status NOT IN ('succeeded', 'superseded')",
		"GROUP BY domain, status, COALESCE(failure_class, '')",
		"ORDER BY count(*) DESC, domain, status",
	} {
		if !strings.Contains(residualBreakdownSQL, want) {
			t.Errorf("residual breakdown query lost %q:\n%s", want, residualBreakdownSQL)
		}
	}
}

// ResidualWorkItems hands these same rows to the zero-correlation diagnosis,
// which explains a missing edge. That message has its own budget and its own
// reader; it must not grow error text because the drain wanted some.
func TestZeroCorrelationPipelineIgnoresFailureMessages(t *testing.T) {
	t.Parallel()

	without := []residualRow{
		{Domain: "repo_dependency", Status: "pending", Count: 2},
		{Domain: "code_function_summary", Status: "dead_letter", FailureClass: "projection_bug", Count: 1},
	}
	with := make([]residualRow, len(without))
	copy(with, without)
	for i := range with {
		with[i].FailureMessage = "some error text that must not appear here"
	}

	base := diagnoseZeroCorrelationPipeline(context.Background(), &fakePipelineQuerier{rows: without}, "RUNS_IMAGE")
	enriched := diagnoseZeroCorrelationPipeline(context.Background(), &fakePipelineQuerier{rows: with}, "RUNS_IMAGE")

	if base != enriched {
		t.Errorf("pipeline diagnosis changed when messages were added:\n base = %s\n with = %s", base, enriched)
	}
}
