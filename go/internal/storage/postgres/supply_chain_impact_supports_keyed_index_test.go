// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// DRAFT index set g (#10880 PR-C1): the keyed active_facts probe over the supports
// table. The QA replica plan (qa-prc-plans-2026-11-24) shows the probe as a
// Parallel Seq Scan on supports at estimated cost 3.05M against a 1.3 s index
// lookup on fact_records, because the supports table has no btree on the five
// payload expressions the probe equates. Migration 354 mirrors the legacy index's
// key expressions (migration 291) on the supports payload. The probe statement is
// unchanged by this: it stays the legacy statement with the row source swapped, so
// the index is usable only while its expressions are byte-equivalent to the probe's.
// The final set is decided after the post-roll QA measurement and must not be
// applied inside W1b.

const (
	keyedSupportsIndexMigration = "migrations/354_supply_chain_impact_supports_keyed_probe_idx.sql"
	keyedFactsIndexMigration    = "migrations/291_fact_records_supply_chain_impact_winners_keyed_idx.sql"
)

// migrationStatement returns the executable text of a migration with comment lines
// removed and whitespace collapsed.
func keyedIndexStatement(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "--") {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, " ")
}

// normalizeKeyExpr removes whitespace and one pair of wrapping parentheses, so the
// index's parenthesized expression and the probe's bare one compare equal.
func normalizeKeyExpr(expr string) string {
	expr = regexp.MustCompile(`\s+`).ReplaceAllString(expr, "")
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") && balancedOuter(expr) {
		expr = expr[1 : len(expr)-1]
	}
	return expr
}

// balancedOuter reports whether the first parenthesis closes at the last character.
func balancedOuter(expr string) bool {
	depth := 0
	for i, r := range expr {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(expr)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// splitTopLevel splits on commas outside parentheses and string literals.
func splitTopLevel(list string) []string {
	var parts []string
	depth, start, inString := 0, 0, false
	for i, r := range list {
		switch {
		case r == '\'':
			inString = !inString
		case inString:
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == ',' && depth == 0:
			parts = append(parts, list[start:i])
			start = i + 1
		}
	}
	return append(parts, list[start:])
}

// indexKeyExpressions returns the normalized key list of the single index column
// list in a CREATE INDEX statement, in order.
func indexKeyExpressions(ddl string) ([]string, error) {
	open := strings.Index(ddl, " ON ")
	if open < 0 {
		return nil, fmt.Errorf("no ON clause in %q", ddl)
	}
	rest := ddl[open:]
	lp := strings.Index(rest, "(")
	if lp < 0 {
		return nil, fmt.Errorf("no key list in %q", ddl)
	}
	depth, end := 0, -1
	for i := lp; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated key list in %q", ddl)
	}
	var keys []string
	for _, part := range splitTopLevel(rest[lp+1 : end]) {
		keys = append(keys, normalizeKeyExpr(part))
	}
	return keys, nil
}

var keyedProbeEquality = regexp.MustCompile(`(?m)^\s+(?:WHERE|AND) (.+?) = ident\.\w+\s*$`)

// keyedProbeExpressions returns, in order, the left-hand expressions the keyed
// probe equates to the identifier columns, normalized over the unaliased payload.
func keyedProbeExpressions(probe string) []string {
	var exprs []string
	for _, match := range keyedProbeEquality.FindAllStringSubmatch(probe, -1) {
		exprs = append(exprs, normalizeKeyExpr(strings.ReplaceAll(match[1], "f.payload", "payload")))
	}
	return exprs
}

// keyedProbeCoveredByIndex reports an error unless the index's leading key
// expressions equal the probe's equality expressions, in order.
func keyedProbeCoveredByIndex(probe, ddl string) error {
	want := keyedProbeExpressions(probe)
	if len(want) != 5 {
		return fmt.Errorf("probe has %d identifier equalities, want 5", len(want))
	}
	keys, err := indexKeyExpressions(ddl)
	if err != nil {
		return err
	}
	if len(keys) < len(want) {
		return fmt.Errorf("index has %d keys, fewer than the probe's %d equalities", len(keys), len(want))
	}
	for i, expr := range want {
		if keys[i] != expr {
			return fmt.Errorf("index key %d is %s, the probe equates %s", i+1, keys[i], expr)
		}
	}
	return nil
}

// TestKeyedSupportsIndexCoversTheKeyedProbe pins that the draft index's leading
// five keys are exactly the expressions the supports keyed probe equates, in the
// probe's order, so the planner can use every equality as an index condition.
func TestKeyedSupportsIndexCoversTheKeyedProbe(t *testing.T) {
	t.Parallel()

	ddl := keyedIndexStatement(t, keyedSupportsIndexMigration)
	if err := keyedProbeCoveredByIndex(buildWinnersKeyedActiveFactsFromSupportsSQL, ddl); err != nil {
		t.Fatalf("migration 354 does not cover the supports keyed probe: %v", err)
	}
	// The legacy index covers the legacy probe by the same rule, which proves the
	// matcher reads the real statements and is not vacuous.
	if err := keyedProbeCoveredByIndex(buildWinnersKeyedActiveFactsSQL, keyedIndexStatement(t, keyedFactsIndexMigration)); err != nil {
		t.Fatalf("the matcher rejects the shipped legacy index and probe: %v", err)
	}
}

// TestKeyedSupportsIndexMirrorsTheLegacyKeyExpressions derives the expected index
// from migration 291: the same five key expressions, then fact_id ASC, over the
// supports table, one CONCURRENTLY IF NOT EXISTS statement, and no predicate (a
// support exists only for a live finding fact).
func TestKeyedSupportsIndexMirrorsTheLegacyKeyExpressions(t *testing.T) {
	t.Parallel()

	ddl := keyedIndexStatement(t, keyedSupportsIndexMigration)
	if !strings.HasPrefix(ddl, "CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_finding_supports_keyed_probe_idx ON supply_chain_impact_finding_supports (") {
		t.Fatalf("migration 354 is not one CONCURRENTLY IF NOT EXISTS index on the supports table: %s", ddl)
	}
	if strings.Count(ddl, ";") != 1 || !strings.HasSuffix(ddl, ";") {
		t.Fatalf("migration 354 must hold exactly one statement: %s", ddl)
	}
	if strings.Contains(ddl, " WHERE ") {
		t.Fatalf("migration 354 carries a predicate: supports hold only live finding rows, so none is needed: %s", ddl)
	}
	got, err := indexKeyExpressions(ddl)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := indexKeyExpressions(keyedIndexStatement(t, keyedFactsIndexMigration))
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{}, legacy[:6]...) // five expressions, then fact_id ASC
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("migration 354 keys =\n%v\nwant the legacy five expressions then fact_id ASC =\n%v", got, want)
	}
}

// TestKeyedProbeCoverageRejectsSeededViolations proves the coverage rule fails on
// planted defects: a reordered pair, a dropped expression, a plain column in place
// of the c0 expression, and a probe that gained a sixth equality.
func TestKeyedProbeCoverageRejectsSeededViolations(t *testing.T) {
	t.Parallel()

	ddl := keyedIndexStatement(t, keyedSupportsIndexMigration)
	probe := buildWinnersKeyedActiveFactsFromSupportsSQL
	if err := keyedProbeCoveredByIndex(probe, ddl); err != nil {
		t.Fatalf("clean pair rejected: %v", err)
	}
	pkg := "(payload->>'package_id'),"
	repo := "(payload->>'repository_id'),"
	for name, mutated := range map[string]string{
		"reordered":       strings.Replace(strings.Replace(ddl, pkg, "@@", 1), repo, pkg+" "+repo, 1),
		"dropped":         strings.Replace(ddl, "(COALESCE(payload->>'advisory_id', '')),", "", 1),
		"plain c0 column": strings.Replace(ddl, "(COALESCE(NULLIF(payload->>'cve_id', ''), NULLIF(payload->>'advisory_id', ''), '')),", "cve_id,", 1),
	} {
		mutated = strings.Replace(mutated, "@@", repo, 1)
		if mutated == ddl {
			t.Fatalf("%s: the seeded mutation did not change the index", name)
		}
		if err := keyedProbeCoveredByIndex(probe, mutated); err == nil {
			t.Errorf("%s: a defective index was accepted", name)
		}
	}
	extra := strings.Replace(probe, "AND f.fact_kind", "AND (f.payload->>'workload_id') = ident.advisory_id\n          AND f.fact_kind", 1)
	if extra == probe {
		extra = strings.Replace(probe, "OFFSET 0", "AND (f.payload->>'workload_id') = ident.advisory_id\n        OFFSET 0", 1)
	}
	if extra == probe {
		t.Fatal("could not seed a sixth equality into the probe")
	}
	if err := keyedProbeCoveredByIndex(extra, ddl); err == nil {
		t.Error("a probe with a sixth equality was accepted")
	}
}
