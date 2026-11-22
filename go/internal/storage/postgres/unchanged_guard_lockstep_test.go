// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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
	factGuardSetPattern   = regexp.MustCompile(`(?is)\bDO\s+UPDATE\s+SET\s+(.*?)\s+WHERE\s`)
	factGuardGroupPattern = regexp.MustCompile(`(?is)\bAND\s*\((.*)\)\s*$`)
	factGuardLegPattern   = regexp.MustCompile(`(?i)^fact_records\.(\w+)\s+IS\s+DISTINCT\s+FROM\s+EXCLUDED\.(\w+)$`)
	// factGuardDiscriminatorLegPattern matches one leg of the
	// selectUnchangedFactIDs WHERE clause in unchanged_guard.go, which joins
	// fact_records AS f against the incoming VALUES AS v and keeps rows
	// equal on every leg (IS NOT DISTINCT FROM). Parsed from the production
	// source (never a hand copy) so a guard-side column addition without the
	// matching discriminator leg fails loudly.
	factGuardDiscriminatorLegPattern = regexp.MustCompile(`(?i)\bf\.(\w+)\s+IS\s+NOT\s+DISTINCT\s+FROM\s+v\.(\w+)`)
	factGuardSetItemPattern          = regexp.MustCompile(`(?i)^(\w+)\s*=\s*EXCLUDED\.(\w+)$`)
	factGuardInsertPattern           = regexp.MustCompile(`(?is)INSERT\s+INTO\s+fact_records\s*\((.*?)\)`)
)

const factGuardFencingPredicate = "fact_records.fencing_token <= EXCLUDED.fencing_token"

// unchangedGuardSource reads the production selectUnchangedFactIDs source so
// the lockstep check parses the discriminator legs it ships, never a copy.
func unchangedGuardSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(file), "unchanged_guard.go"))
	if err != nil {
		t.Fatalf("read unchanged_guard.go: %v", err)
	}
	return string(src)
}

// factGuardDiscriminatorLegs returns the ordered f.X/v.Y leg columns parsed
// from a selectUnchangedFactIDs source, plus every malformed leg.
func factGuardDiscriminatorLegs(source string) (legs []string, malformed []string) {
	for _, match := range factGuardDiscriminatorLegPattern.FindAllStringSubmatch(source, -1) {
		if match[1] != match[2] {
			malformed = append(malformed, match[0])
			continue
		}
		legs = append(legs, match[1])
	}
	return legs, malformed
}

// factGuardClauseDrift returns every way suffix's SET list, guard legs, and
// prefix's INSERT columns disagree. discriminator is the production
// unchanged_guard.go source: its WHERE legs must equal the suffix guard legs
// in order, or a fencing-false row that differs only in a guard-side-only
// column would match the discriminator and leak a stale derivation (#4444).
func factGuardClauseDrift(suffix, prefix, discriminator string) []string {
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
	discLegs, discMalformed := factGuardDiscriminatorLegs(discriminator)
	for _, leg := range discMalformed {
		drift = append(drift, fmt.Sprintf("discriminator leg %q is not `f.X IS NOT DISTINCT FROM v.X`", leg))
	}
	if len(discLegs) == 0 && len(discMalformed) == 0 {
		drift = append(drift, "no discriminator `f.X IS NOT DISTINCT FROM v.X` legs found")
	} else if !slices.Equal(discLegs, gotGuard) {
		drift = append(drift, fmt.Sprintf("discriminator legs %v != suffix guard legs %v in order", discLegs, gotGuard))
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

// factGuardOwnerLegs are the two owner legs (#11203) the conflict WHERE must
// carry, right after the fencing predicate: an upsert never rewrites a row held
// by another scope or generation. They are deliberately NOT in
// factwrite.BatchInsertConflict, whose reducer ids may be stable across
// generations; a generation leg there would freeze such a row at its first
// generation (issue #11213 owns that census).
var factGuardOwnerLegs = []string{
	"fact_records.scope_id = EXCLUDED.scope_id",
	"fact_records.generation_id = EXCLUDED.generation_id",
}

// factGuardOwnerDrift reports every way the owner guard drifted: a missing or
// reordered leg in the suffix, an owner leg placed before the fencing predicate,
// or a classification query (the unchanged-guard source) that no longer reads the
// stored scope and generation, which would let a foreign row with the owner's
// exact columns be admitted as unchanged.
func factGuardOwnerDrift(suffix, discriminator string) []string {
	var drift []string
	fencing := strings.Index(suffix, factGuardFencingPredicate)
	prev := fencing
	for _, leg := range factGuardOwnerLegs {
		at := strings.Index(suffix, leg)
		switch {
		case at < 0:
			drift = append(drift, "owner leg `"+leg+"` missing from the conflict WHERE (#11203)")
		case at < prev:
			drift = append(drift, "owner leg `"+leg+"` is not after the fencing predicate and the previous owner leg")
		default:
			prev = at
		}
	}
	for _, want := range []string{
		"f.scope_id, f.generation_id", "storedScope != incoming.ScopeID", "storedGeneration != incoming.GenerationID",
	} {
		if !strings.Contains(discriminator, want) {
			drift = append(drift, "classification query no longer reads "+want+": a foreign row could read as unchanged (#11203)")
		}
	}
	return drift
}

// TestUpsertFactBatchGuardLockstep fails when the shipped suffix's SET list,
// guard legs, or INSERT columns drift apart.
func TestUpsertFactBatchGuardLockstep(t *testing.T) {
	t.Parallel()
	if drift := factGuardClauseDrift(upsertFactBatchSuffix, upsertFactBatchPrefix, unchangedGuardSource(t)); len(drift) > 0 {
		t.Fatalf("fact upsert guard drifted:\n%s", strings.Join(drift, "\n"))
	}
	if drift := factGuardOwnerDrift(upsertFactBatchSuffix, unchangedGuardSource(t)); len(drift) > 0 {
		t.Fatalf("fact upsert owner guard drifted:\n%s", strings.Join(drift, "\n"))
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
	const cleanDiscriminator = `
JOIN fact_records AS f ON f.fact_id = v.fact_id
WHERE f.fact_kind IS NOT DISTINCT FROM v.fact_kind
  AND f.payload IS NOT DISTINCT FROM v.payload`
	if drift := factGuardClauseDrift(clean, insertBase, cleanDiscriminator); len(drift) != 0 {
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
		if drift := factGuardClauseDrift(clause, insertBase, cleanDiscriminator); len(drift) == 0 {
			t.Fatalf("seeded violation %q not reported", name)
		}
	}
	discriminatorCases := map[string]string{
		"discriminator leg dropped": strings.Replace(
			cleanDiscriminator, "\n  AND f.payload IS NOT DISTINCT FROM v.payload", "", 1),
		"discriminator legs reordered": `
JOIN fact_records AS f ON f.fact_id = v.fact_id
WHERE f.payload IS NOT DISTINCT FROM v.payload
  AND f.fact_kind IS NOT DISTINCT FROM v.fact_kind`,
		"discriminator alias mismatch": strings.Replace(
			cleanDiscriminator, "f.payload IS NOT DISTINCT FROM v.payload", "f.payload IS NOT DISTINCT FROM v.fact_kind", 1),
	}
	for name, discriminator := range discriminatorCases {
		if drift := factGuardClauseDrift(clean, insertBase, discriminator); len(drift) == 0 {
			t.Fatalf("seeded violation %q not reported", name)
		}
	}
}

// TestFactOwnerGuardDriftSeededViolations is the RED/GREEN pair for the owner
// lockstep: the shipped statement and classification query are clean, and each
// planted corruption (a deleted leg, a reordered leg, an owner leg before the
// fencing predicate, a classification query without the owner columns) is
// reported.
func TestFactOwnerGuardDriftSeededViolations(t *testing.T) {
	t.Parallel()
	source := unchangedGuardSource(t)
	if drift := factGuardOwnerDrift(upsertFactBatchSuffix, source); len(drift) != 0 {
		t.Fatalf("shipped owner guard reported drift: %v", drift)
	}
	cases := map[string]struct{ suffix, source string }{
		"scope leg deleted":      {strings.Replace(upsertFactBatchSuffix, factOwnerGuardClauseScopeLeg, "", 1), source},
		"generation leg deleted": {strings.Replace(upsertFactBatchSuffix, factOwnerGuardClauseGenerationLeg, "", 1), source},
		"both legs deleted":      {strings.Replace(upsertFactBatchSuffix, factOwnerGuardClause, "", 1), source},
		"legs before fencing": {
			strings.Replace(strings.Replace(upsertFactBatchSuffix, factOwnerGuardClause, "", 1),
				"WHERE "+factGuardFencingPredicate, "WHERE fact_records.scope_id = EXCLUDED.scope_id AND fact_records.generation_id = EXCLUDED.generation_id AND "+factGuardFencingPredicate, 1),
			source,
		},
		"classification without the stored scope column": {upsertFactBatchSuffix, strings.Replace(source, "f.scope_id, f.generation_id", "f.x, f.generation_id", 1)},
		"classification ignoring a scope mismatch":       {upsertFactBatchSuffix, strings.Replace(source, "storedScope != incoming.ScopeID", "false", 1)},
		"classification ignoring a generation mismatch":  {upsertFactBatchSuffix, strings.Replace(source, "storedGeneration != incoming.GenerationID", "false", 1)},
	}
	for name, tc := range cases {
		if drift := factGuardOwnerDrift(tc.suffix, tc.source); len(drift) == 0 {
			t.Fatalf("seeded violation %q not reported", name)
		}
	}
}
