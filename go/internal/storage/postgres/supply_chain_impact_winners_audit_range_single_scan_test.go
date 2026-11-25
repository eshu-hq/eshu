// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// The #10972 single-scan range statement keeps the shipped range contract
// (one budgeted read per range, whole identifiers, oversize alone and whole,
// open last range) while reading the fact heap once per row instead of four
// times: range_raw carries only the narrow key columns, one range_bounds
// CTE folds the tail/below-tail/count CTEs with single-pass window
// functions, and range_facts re-attaches payload and source_confidence by
// primary-key join in the same statement and snapshot. These are the
// hermetic shape pins; the live differential, cost, and index proofs live in
// supply_chain_impact_winners_audit_range_single_scan_live_test.go.

// singleScanSQLs renders both single-scan variants under test.
func singleScanSQLs(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"first": renderWinnersAuditRangeSingleScanSQL(true),
		"next":  renderWinnersAuditRangeSingleScanSQL(false),
	}
}

// TestSingleScanRangeRawProjectsOnlyNarrowColumnsAndIdents pins the read-path
// narrowing: range_raw projects fact_id, scope_id, generation_id and the
// five 291 identifier expressions, and carries no payload or
// source_confidence. The heap is read once, narrow; the wide columns rejoin
// later by primary key.
func TestSingleScanRangeRawProjectsOnlyNarrowColumnsAndIdents(t *testing.T) {
	t.Parallel()
	for name, sql := range singleScanSQLs(t) {
		arms := cteArms(t, sql, "range_raw")
		if len(arms) != 1 {
			t.Fatalf("%s range_raw has %d arms, want 1", name, len(arms))
		}
		arm := arms[0]
		projected := projectedColumns(arm)
		for _, want := range []string{"fact_id", "scope_id", "generation_id"} {
			if !projected[want] {
				t.Errorf("%s range_raw does not project narrow column %s", name, want)
			}
		}
		for _, expr := range winnersAuditRangeFactIdentExprs {
			if !strings.Contains(arm, expr+" AS ") {
				t.Errorf("%s range_raw does not project identifier expression %s", name, expr)
			}
		}
		// The identifier expressions read payload->> fields; only a bare
		// wide-column projection counts as carrying the heap row.
		if projected["payload"] {
			t.Errorf("%s range_raw carries payload: the wide column must rejoin by primary key instead", name)
		}
		if projected["source_confidence"] {
			t.Errorf("%s range_raw carries source_confidence: the wide column must rejoin by primary key instead", name)
		}
	}
}

// TestSingleScanIdentExprsMatchMigration291 pins the handoff's byte-identical
// claim: stripping the `f.` qualifier from each range identifier expression
// yields exactly the five frozen key expressions of migration 291 (immutable
// under #7002; the bootstrap fragment repeats them in
// schema_fact_records_supply_chain_impact_indexes.go), so the range
// predicate and ORDER BY stay index conditions and index order.
func TestSingleScanIdentExprsMatchMigration291(t *testing.T) {
	t.Parallel()
	want := []string{
		`(payload->>'package_id')`,
		`(payload->>'repository_id')`,
		`(payload->>'subject_digest')`,
		`COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')`,
		`COALESCE(payload->>'advisory_id', '')`,
	}
	if len(winnersAuditRangeFactIdentExprs) != len(want) {
		t.Fatalf("identifier expressions = %d, want %d", len(winnersAuditRangeFactIdentExprs), len(want))
	}
	for i, expr := range winnersAuditRangeFactIdentExprs {
		if got := strings.ReplaceAll(expr, "f.", ""); got != want[i] {
			t.Errorf("identifier expression %d = %s, want migration 291 bytes %s", i, got, want[i])
		}
	}
	for name, sql := range singleScanSQLs(t) {
		for _, expr := range winnersAuditRangeFactIdentExprs {
			if !strings.Contains(sql, expr) {
				t.Errorf("%s single-scan statement lost identifier expression %s", name, expr)
			}
		}
	}
}

// TestSingleScanRangeBoundsIsOneMaterializedCTE pins the fold: range_tail
// and range_below_tail are gone, range_bounds is one MATERIALIZED CTE fed
// by single-pass window functions over a single scan of range_raw, and it
// still emits the identical bound columns.
func TestSingleScanRangeBoundsIsOneMaterializedCTE(t *testing.T) {
	t.Parallel()
	for name, sql := range singleScanSQLs(t) {
		if strings.Contains(sql, "range_tail AS") {
			t.Errorf("%s still renders range_tail: the fold must absorb it", name)
		}
		if strings.Contains(sql, "range_below_tail AS") {
			t.Errorf("%s still renders range_below_tail: the fold must absorb it", name)
		}
		if got := strings.Count(sql, "range_bounds AS MATERIALIZED ("); got != 1 {
			t.Errorf("%s renders range_bounds %d times, want exactly one MATERIALIZED CTE", name, got)
		}
		for _, window := range []string{"count(*) OVER ()", "rank() OVER (", "min(CASE WHEN"} {
			if !strings.Contains(sql, window) {
				t.Errorf("%s range_bounds does not use single-pass window %q", name, window)
			}
		}
		for _, column := range winnersAuditRangeBoundColumns {
			if !strings.Contains(sql, "AS "+column) {
				t.Errorf("%s range_bounds does not emit bound column %s", name, column)
			}
		}
	}
}

// oversizeArmBlock slices the oversize UNION ALL arm (the re-scan of
// fact_records for an oversize identifier) out of a range statement, from
// its WHERE marker to the arm's closing parenthesis.
func oversizeArmBlock(t *testing.T, sql string) string {
	t.Helper()
	const marker = "WHERE (SELECT oversize FROM range_bounds)"
	start := strings.Index(sql, marker)
	if start < 0 {
		t.Fatalf("range statement has no oversize arm marker %q", marker)
	}
	end := strings.Index(sql[start:], "),\nwinners_now")
	if end < 0 {
		t.Fatalf("oversize arm has no closing boundary before winners_now")
	}
	return sql[start : start+end]
}

// TestSingleScanOversizeArmTextuallyUnchanged pins the handoff's refusal
// boundary: the oversize re-scan keeps the shipped predicate byte for byte,
// so an oversize identifier still reads every one of its facts by the same
// index equalities.
func TestSingleScanOversizeArmTextuallyUnchanged(t *testing.T) {
	t.Parallel()
	for name, pair := range map[string][2]string{
		"first": {selectWinnersAuditFirstRangeSQL, renderWinnersAuditRangeSingleScanSQL(true)},
		"next":  {selectWinnersAuditNextRangeSQL, renderWinnersAuditRangeSingleScanSQL(false)},
	} {
		want, got := oversizeArmBlock(t, pair[0]), oversizeArmBlock(t, pair[1])
		if got != want {
			t.Errorf("%s oversize arm differs from shipped:\n--- shipped ---\n%s\n--- single-scan ---\n%s", name, want, got)
		}
	}
}

// TestSingleScanRangeFactsReattachesBodyColumns pins the re-attachment
// contract through the shared column guard: every column the winner body
// reads off its fact alias is projected by both range_facts arms of the new
// statement, and range_raw stays narrow (its two missing legs are exactly
// the re-attached wide columns).
func TestSingleScanRangeFactsReattachesBodyColumns(t *testing.T) {
	t.Parallel()
	columns := winnerBodyFactColumns()
	next := renderWinnersAuditRangeSingleScanSQL(false)
	missing := missingRangeColumns(t, next, columns)
	want := []string{"range_raw arm 1: payload", "range_raw arm 1: source_confidence"}
	if len(missing) != len(want) {
		t.Fatalf("single-scan missing columns = %v, want exactly %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Fatalf("single-scan missing columns = %v, want exactly %v", missing, want)
		}
	}
}

// TestStagedAuditRangeObservationOverFetchRatio pins the staged-audit
// telemetry: the observation carries the budgeted raw index rows beside the
// recomputed facts, the over-fetch ratio is raw over facts, a range that
// recomputed nothing reports zero instead of dividing by zero, and the
// report line carries the ratio ObserveRange emits.
func TestStagedAuditRangeObservationOverFetchRatio(t *testing.T) {
	t.Parallel()
	obs := StagedAuditRangeObservation{Budget: 200, RawRows: 100, Facts: 80}
	if got := obs.OverFetchRatio(); got != 1.25 {
		t.Errorf("OverFetchRatio() = %v, want 1.25", got)
	}
	if report := obs.report(); !strings.Contains(report, "over_fetch=1.25") {
		t.Errorf("report() = %q, want it to carry over_fetch=1.25", report)
	}
	empty := StagedAuditRangeObservation{Budget: 200, RawRows: 5, Facts: 0}
	if got := empty.OverFetchRatio(); got != 0 {
		t.Errorf("empty OverFetchRatio() = %v, want 0 (no division by zero)", got)
	}
	rng := WinnersAuditRange{Budget: 50, RawRows: 50, Facts: 40, Oversize: true, Last: false}
	got, line := ObserveRange(rng)
	if got.RawRows != 50 || got.Facts != 40 || got.Budget != 50 || !got.Oversize || got.Last {
		t.Errorf("ObserveRange() = %+v, want the range's raw/facts/budget/oversize/last", got)
	}
	if !strings.Contains(line, "over_fetch=1.25") {
		t.Errorf("ObserveRange() line = %q, want it to emit over_fetch=1.25", line)
	}
}
