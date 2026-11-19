// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Lockstep check for the issue #10873 unchanged-row guard on
// upsertFactBatchSuffix: the SET list, the guard legs, and the INSERT columns
// must agree, or a written-but-uncompared column keeps a stale value silently
// (or a compared-but-unwritten column skips rows it should write). Mirrors
// TestUpsertContentFileConflictClauseLockstep (#8915) for the fact shape:
// OR-chained legs with the payload compared last so cheap scalars
// short-circuit, and the fencing predicate kept verbatim (#4444).

var (
	factGuardSetPattern     = regexp.MustCompile(`(?is)\bDO\s+UPDATE\s+SET\s+(.*?)\s+WHERE\s`)
	factGuardGroupPattern   = regexp.MustCompile(`(?is)\bAND\s*\((.*)\)\s*$`)
	factGuardLegPattern     = regexp.MustCompile(`(?i)^fact_records\.(\w+)\s+IS\s+DISTINCT\s+FROM\s+EXCLUDED\.(\w+)$`)
	factGuardSetItemPattern = regexp.MustCompile(`(?i)^(\w+)\s*=\s*EXCLUDED\.(\w+)$`)
	factGuardInsertPattern  = regexp.MustCompile(`(?is)INSERT\s+INTO\s+fact_records\s*\((.*?)\)`)
)

const factGuardFencingPredicate = "fact_records.fencing_token <= EXCLUDED.fencing_token"

// factGuardClauseDrift returns every way suffix's SET list, guard legs, and
// prefix's INSERT columns disagree.
func factGuardClauseDrift(suffix, prefix string) []string {
	var drift []string
	setMatch := factGuardSetPattern.FindStringSubmatch(suffix)
	if setMatch == nil {
		return []string{"no DO UPDATE SET ... WHERE clause found"}
	}
	var setColumns []string
	for _, item := range splitColumns(setMatch[1]) {
		parts := factGuardSetItemPattern.FindStringSubmatch(item)
		if parts == nil || parts[1] != parts[2] {
			drift = append(drift, fmt.Sprintf("SET item %q is not `col = EXCLUDED.col`", item))
			continue
		}
		setColumns = append(setColumns, parts[1])
	}
	if !strings.Contains(suffix, factGuardFencingPredicate) {
		drift = append(drift, "fencing predicate `"+factGuardFencingPredicate+"` missing or reworded (#4444)")
	}

	var wantGuard []string
	for _, column := range setColumns {
		if !slices.Contains(factGuardSkippedColumns, column) {
			wantGuard = append(wantGuard, column)
		}
	}
	group := factGuardGroupPattern.FindStringSubmatch(strings.TrimSpace(suffix))
	if group == nil {
		return append(drift, "guard `AND (...)` group not found")
	}
	legs := regexp.MustCompile(`(?i)\s+OR\s+`).Split(group[1], -1)
	var gotGuard []string
	for _, leg := range legs {
		parts := factGuardLegPattern.FindStringSubmatch(strings.TrimSpace(leg))
		if parts == nil || parts[1] != parts[2] {
			drift = append(drift, fmt.Sprintf("guard leg %q is not `fact_records.X IS DISTINCT FROM EXCLUDED.X`", leg))
			continue
		}
		gotGuard = append(gotGuard, parts[1])
	}
	if !slices.Equal(gotGuard, wantGuard) {
		drift = append(drift, fmt.Sprintf("guard legs %v != SET columns minus %v = %v", gotGuard, factGuardSkippedColumns, wantGuard))
	}
	if len(gotGuard) > 0 && gotGuard[len(gotGuard)-1] != "payload" {
		drift = append(drift, fmt.Sprintf("payload is not the last guard leg (got %q): cheap scalars must short-circuit first", gotGuard[len(gotGuard)-1]))
	}

	insertMatch := factGuardInsertPattern.FindStringSubmatch(prefix)
	if insertMatch == nil {
		return append(drift, "no INSERT INTO fact_records column list found")
	}
	var wantSet []string
	for _, column := range splitColumns(insertMatch[1]) {
		if !slices.Contains(factGuardKeyColumns, column) {
			wantSet = append(wantSet, column)
		}
	}
	gotSet, wantSorted := slices.Clone(setColumns), slices.Clone(wantSet)
	slices.Sort(gotSet)
	slices.Sort(wantSorted)
	if !slices.Equal(gotSet, wantSorted) {
		drift = append(drift, fmt.Sprintf("SET columns %v != INSERT columns minus key %v", gotSet, wantSorted))
	}
	return drift
}

// TestUpsertFactBatchGuardLockstep fails when the shipped suffix's SET list,
// guard legs, or INSERT columns drift apart.
func TestUpsertFactBatchGuardLockstep(t *testing.T) {
	t.Parallel()
	if drift := factGuardClauseDrift(upsertFactBatchSuffix, upsertFactBatchPrefix); len(drift) > 0 {
		t.Fatalf("fact upsert guard drifted:\n%s", strings.Join(drift, "\n"))
	}
	if !strings.HasSuffix(upsertFactBatchSuffixReturningFactID, upsertFactBatchSuffix) &&
		!strings.HasPrefix(upsertFactBatchSuffixReturningFactID, upsertFactBatchSuffix) {
		t.Fatal("RETURNING suffix is not the guarded suffix plus RETURNING")
	}
}

// TestFactGuardClauseDriftSeededViolations is the RED/GREEN pair for the
// lockstep check: each corruption class is reported, and the clean clause is
// not.
func TestFactGuardClauseDriftSeededViolations(t *testing.T) {
	t.Parallel()
	const insertBase = "INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, payload, observed_at) VALUES "
	const clean = `
ON CONFLICT (fact_id) DO UPDATE SET
fact_kind = EXCLUDED.fact_kind, payload = EXCLUDED.payload, observed_at = EXCLUDED.observed_at
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
  AND (fact_records.fact_kind IS DISTINCT FROM EXCLUDED.fact_kind
    OR fact_records.payload IS DISTINCT FROM EXCLUDED.payload)`
	if drift := factGuardClauseDrift(clean, insertBase); len(drift) != 0 {
		t.Fatalf("clean clause reported drift: %v", drift)
	}
	cases := map[string]string{
		"missing leg": strings.Replace(
			clean, "\n    OR fact_records.payload IS DISTINCT FROM EXCLUDED.payload", "", 1),
		"timestamp compared": strings.Replace(
			clean, "OR fact_records.payload", "OR fact_records.observed_at IS DISTINCT FROM EXCLUDED.observed_at OR fact_records.payload", 1),
		"fencing reworded": strings.Replace(
			clean, "fact_records.fencing_token <= EXCLUDED.fencing_token", "fact_records.fencing_token < EXCLUDED.fencing_token", 1),
		"set without guard": strings.Replace(
			clean, "payload = EXCLUDED.payload,", "payload = EXCLUDED.payload, fact_kind = EXCLUDED.fact_kind,", 1),
	}
	for name, clause := range cases {
		if drift := factGuardClauseDrift(clause, insertBase); len(drift) == 0 {
			t.Fatalf("seeded violation %q not reported", name)
		}
	}
}
