// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// migrationIndexStatement returns the CREATE INDEX statement that creates
// name in the embedded migrations, whitespace-collapsed.
func migrationIndexStatement(t *testing.T, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?IF\s+NOT\s+EXISTS\s+` + name + `\b[^;]*;`)
	var found []string
	for _, definition := range migrations.BootstrapDefinitions() {
		found = append(found, pattern.FindAllString(definition.SQL, -1)...)
	}
	if len(found) != 1 {
		t.Fatalf("migrations create %s %d times, want 1", name, len(found))
	}
	return strings.Join(strings.Fields(found[0]), " ")
}

// indexPredicateKinds parses the fact_kind list of an index predicate.
func indexPredicateKinds(t *testing.T, statement string) []string {
	t.Helper()
	at := strings.Index(statement, " WHERE ")
	if at < 0 {
		t.Fatalf("index statement has no predicate: %s", statement)
	}
	where := statement[at:]
	if match := regexp.MustCompile(`fact_kind IN \(([^)]*)\)`).FindStringSubmatch(where); match != nil {
		return regexp.MustCompile(`'([^']+)'`).FindAllString(match[1], -1)
	}
	if match := regexp.MustCompile(`fact_kind = ('[^']+')`).FindStringSubmatch(where); match != nil {
		return []string{match[1]}
	}
	return nil
}

// TestBranchKindsAreProvableByAnchors pins ruling Addendum A: a partial index
// serves a branch only when the branch's literal kind list equals or is a
// subset of the index predicate's list, and every other conjunct of the
// predicate appears in the branch.
func TestBranchKindsAreProvableByAnchors(t *testing.T) {
	t.Parallel()

	for _, branch := range Branches() {
		statement := migrationIndexStatement(t, branch.Anchor)
		if branch.Anchor == IndexScopeGeneration {
			continue // unpartial (scope_id, generation_id, fact_kind, ...)
		}
		if !strings.Contains(statement, "is_tombstone = FALSE") || !strings.Contains(branch.SQL, "fact.is_tombstone = FALSE") {
			t.Errorf("%s: anchor %s or branch lacks the tombstone conjunct", branch.Name, branch.Anchor)
		}
		if branch.Shape == ShapeGuard {
			continue // proven by TestGuardPredicateMatchesIndex
		}
		indexKinds := indexPredicateKinds(t, statement)
		for _, kind := range branch.Kinds {
			if !slices.Contains(indexKinds, "'"+kind+"'") {
				t.Errorf("%s: kind %s is not in anchor %s predicate %v", branch.Name, kind, branch.Anchor, indexKinds)
			}
		}
		leading := regexp.MustCompile(`ON fact_records \(\s*\(payload->>'` + branch.ProbeKey + `'\)`)
		if branch.ProbeKey != "" && !leading.MatchString(statement) {
			t.Errorf("%s: anchor %s does not lead on payload->>'%s': %s", branch.Name, branch.Anchor, branch.ProbeKey, statement)
		}
	}
}

// TestNewIndexStatementsMatchMigrations pins every generated #8880 index
// statement to its migration file, byte for byte, and each migration to one
// sole CONCURRENTLY statement.
func TestNewIndexStatementsMatchMigrations(t *testing.T) {
	t.Parallel()

	byPath := map[string]string{}
	for _, definition := range migrations.BootstrapDefinitions() {
		byPath[definition.Path] = definition.SQL
	}
	for _, index := range NewIndexes() {
		sql, ok := byPath["go/internal/storage/postgres/migrations/"+index.Migration]
		if !ok {
			t.Fatalf("migration %s for %s is missing", index.Migration, index.Name)
		}
		var body []string
		for _, line := range strings.Split(sql, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				body = append(body, line)
			}
		}
		if got := strings.TrimSpace(strings.Join(body, "\n")); got != index.Statement {
			t.Errorf("%s statement drifted from factimpact.NewIndexes\nmigration:\n%s\ngenerated:\n%s", index.Migration, got, index.Statement)
		}
		if strings.Count(index.Statement, ";") != 1 || !strings.HasPrefix(index.Statement, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+index.Name+"\n") {
			t.Errorf("%s must hold one CREATE INDEX CONCURRENTLY statement", index.Migration)
		}
	}
}

// TestGuardPredicateMatchesIndex pins predicate implication by identical
// expression: the guard statement repeats the guard index predicate, and the
// JS/TS file branch repeats the JS/TS index language predicate.
func TestGuardPredicateMatchesIndex(t *testing.T) {
	t.Parallel()

	guard := migrationIndexStatement(t, IndexSupplyChainImpactKeyGuard)
	if !strings.Contains(guard, strings.Join(strings.Fields(unqualified(GuardPredicate())), " ")) {
		t.Fatalf("guard index predicate drifted from GuardPredicate()")
	}
	jsts := migrationIndexStatement(t, IndexJSTSReachabilityRepoFile)
	if !strings.Contains(jsts, unqualified(JSTSLanguagePredicate)) {
		t.Fatalf("JS/TS index predicate drifted from JSTSLanguagePredicate")
	}
	for _, branch := range Branches() {
		switch branch.Name {
		case "foreign_key_guard":
			if !strings.Contains(branch.SQL, GuardPredicate()) {
				t.Errorf("guard statement does not repeat GuardPredicate() verbatim")
			}
		case "file_repo_id", "file_scope":
			if !strings.Contains(branch.SQL, JSTSLanguagePredicate) {
				t.Errorf("%s does not repeat JSTSLanguagePredicate verbatim", branch.Name)
			}
		}
	}
}

// TestEveryRetiredCellIsServed is the exactness table behind the live
// differential: for every scanned kind and every retired arm that applies to
// it, some statement can return the row. Bounded kinds evaluate every arm;
// growth and file kinds serve each payload key by a key probe or the guard,
// and the scope-row arms by a resolved-scope probe.
func TestEveryRetiredCellIsServed(t *testing.T) {
	t.Parallel()

	for _, info := range Kinds() {
		probed := map[string]bool{}
		scoped, full := false, false
		for _, branch := range Branches() {
			if !slices.Contains(branch.Kinds, info.Kind) {
				continue
			}
			switch branch.Shape {
			case ShapeFullFilter:
				full = true
			case ShapeScope:
				scoped = true
			case ShapeKeyProbe:
				probed[branch.ProbeKey] = true
			}
		}
		if info.Class == ClassBounded {
			if !full {
				t.Errorf("bounded kind %s has no full-filter statement", info.Kind)
			}
			continue
		}
		omitted := OmittedKeys(info.Kind)
		for _, candidate := range retiredArms() {
			if !candidate.applies(info.Kind) {
				continue
			}
			switch {
			case candidate.key == "":
				if !scoped {
					t.Errorf("%s: scope arm has no resolved-scope probe", info.Kind)
				}
			case !probed[candidate.key] && !slices.Contains(omitted, candidate.key):
				t.Errorf("%s: arm on %s is neither probed nor guarded", info.Kind, candidate.key)
			}
		}
	}
	if got := KindsInRetiredOrder(); len(got) != 21 {
		t.Fatalf("scanned kinds = %d, want the retired 21", len(got))
	}
}

// TestExactlyOneBranchReturnsSuppressions pins the suppression-tail bound:
// one statement returns that kind, ordered and limited; no other statement
// carries a LIMIT, so core evidence is never bounded.
func TestExactlyOneBranchReturnsSuppressions(t *testing.T) {
	t.Parallel()

	var suppressionBranches []string
	for _, branch := range Branches() {
		returns := slices.Contains(branch.Kinds, KindSuppression) || strings.Contains(GuardPredicate(), KindSuppression) && branch.Shape == ShapeGuard
		if returns {
			suppressionBranches = append(suppressionBranches, branch.Name)
		}
		hasLimit := strings.Contains(branch.SQL, "\nLIMIT ")
		if hasLimit != branch.ReturnsSuppressions {
			t.Errorf("%s: LIMIT present = %v, ReturnsSuppressions = %v", branch.Name, hasLimit, branch.ReturnsSuppressions)
		}
	}
	if !slices.Equal(suppressionBranches, []string{"vulnerability_suppression"}) {
		t.Fatalf("branches that can return suppressions = %v, want only vulnerability_suppression", suppressionBranches)
	}
	for _, branch := range Branches() {
		if branch.ReturnsSuppressions && !strings.Contains(branch.SQL, "ORDER BY convert_to(fact.fact_id, 'UTF8')") {
			t.Fatalf("suppression branch must order by byte order before its LIMIT")
		}
	}
}

// TestBranchNamesAreClosedAndUnique pins the telemetry label set.
func TestBranchNamesAreClosedAndUnique(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, branch := range Branches() {
		if seen[branch.Name] || !regexp.MustCompile(`^[a-z][a-z0-9_]*$`).MatchString(branch.Name) {
			t.Fatalf("branch name %q is duplicated or not a closed label", branch.Name)
		}
		seen[branch.Name] = true
		if len(branch.RunWhen) == 0 {
			t.Fatalf("branch %s has no inputs gating it", branch.Name)
		}
	}
}
