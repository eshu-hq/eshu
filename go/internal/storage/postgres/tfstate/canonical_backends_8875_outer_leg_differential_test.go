// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stable boundaries for the #8875 round-2 outer-leg differential probe. The
// probe derives from the shipped listTerraformBackendCanonicalRowsQuery
// constant (never a hand copy): truncate at the trailing ORDER BY, which is
// plan-shape-irrelevant. The hermetic guards below pin exactly three things:
// the query head (so the probe derives from the right constant), the ORDER BY
// boundary marker (so the truncation point is explicit), and the outer-leg
// arm presence. They do not pin the filter text; the predicate lockstep
// guard after them does that.
const (
	tf8875OuterLegQueryHead   = "\nWITH backend_generations AS ("
	tf8875OuterLegOrderMarker = "\nORDER BY backend.repo_id ASC"
	tf8875OuterLegArm         = "FROM backend_generations AS backend"
)

// normalizeSQLPredicate collapses every whitespace run to one blank so SQL
// that differs only in layout compares equal.
func normalizeSQLPredicate(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// outerLegCASESum extracts the outer-leg three-way array-length CASE sum
// from the derived probe: the parenthesized expression after the fact-kind
// and source-system conjuncts, up to the closing ") > 0". It strips the
// query's fact. table alias so the result compares equal to the alias-free
// migration and DDL predicates. It fails the test when the shape is absent
// instead of returning a vacuous string.
func outerLegCASESum(t *testing.T, derived string) string {
	t.Helper()
	armAt := strings.Index(derived, tf8875OuterLegArm)
	if armAt < 0 {
		t.Fatalf("derived probe no longer covers the outer leg (%q absent); re-derive, do not hand-copy it",
			tf8875OuterLegArm)
	}
	outer := derived[armAt:]
	open := strings.Index(outer, "AND (")
	if open < 0 {
		t.Fatalf("outer-leg CASE sum opener %q absent; the filter shape changed, re-derive the probe", "AND (")
	}
	rest := outer[open+len("AND ("):]
	closeAt := strings.Index(rest, ") > 0")
	if closeAt < 0 {
		t.Fatalf("outer-leg CASE sum closer %q absent; the filter shape changed, re-derive the probe", ") > 0")
	}
	sum := strings.ReplaceAll(rest[:closeAt], "fact.", "")
	if strings.Count(sum, "jsonb_array_length(payload") != 3 {
		t.Fatalf("outer-leg CASE sum holds %d array-length arms, want 3; the filter shape changed",
			strings.Count(sum, "jsonb_array_length(payload"))
	}
	return normalizeSQLPredicate(sum)
}

// migrationPredicate reads a migration file from the migrations directory
// and returns its normalized WHERE-clause predicate. The path is resolved
// relative to this package directory so the guard pins the shipped file,
// never a copy.
func migrationPredicate(t *testing.T, filename string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "migrations", filename))
	if err != nil {
		t.Fatalf("read migration %s: %v", filename, err)
	}
	text := string(raw)
	whereAt := strings.Index(text, "WHERE")
	semiAt := strings.LastIndex(text, ";")
	if whereAt < 0 || semiAt < 0 || semiAt <= whereAt {
		t.Fatalf("migration %s has no WHERE...; shape; refusing a vacuous comparison", filename)
	}
	return normalizeSQLPredicate(text[whereAt:semiAt])
}

// ddlPredicate extracts the normalized WHERE-clause predicate from a CREATE
// INDEX statement held in a Go source file in the parent postgres package,
// read as text (a Go import would cycle). It locates the statement by the
// index name so a rename fails loudly instead of comparing the wrong block.
func ddlPredicate(t *testing.T, sourceFile, indexName string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", sourceFile))
	if err != nil {
		t.Fatalf("read DDL source %s: %v", sourceFile, err)
	}
	text := string(raw)
	nameAt := strings.Index(text, indexName)
	if nameAt < 0 {
		t.Fatalf("index %s absent from %s; the DDL moved, re-derive this guard", indexName, sourceFile)
	}
	stmt := text[nameAt:]
	whereAt := strings.Index(stmt, "WHERE")
	semiAt := strings.Index(stmt, ";")
	if whereAt < 0 || semiAt < 0 || semiAt <= whereAt {
		t.Fatalf("DDL statement for %s has no WHERE...; shape; refusing a vacuous comparison", indexName)
	}
	return normalizeSQLPredicate(stmt[whereAt:semiAt])
}

// TestTF8875CanonicalOuterLegExactnessDifferential is the #8875 round-2
// outer-leg guard. It pins two things. Hermetically (runs everywhere): the
// outer-leg predicate extracted from the shipped query constant must equal,
// after whitespace/alias normalization, the predicate in migration 276 and
// in the Go DDL constant, so a key rename, a dropped arm, or a fourth arm
// in any one of the three fails loudly. DSN-backed (runs where
// ESHU_POSTGRES_DSN points at a migrated database): the derived probe must
// plan an Index Scan using the serving index on the shared fixture shape,
// so the seek claim is pinned in-tree instead of living only in prose.
func TestTF8875CanonicalOuterLegExactnessDifferential(t *testing.T) {
	prod := listTerraformBackendCanonicalRowsQuery
	if !strings.HasPrefix(prod, tf8875OuterLegQueryHead) {
		t.Fatalf("canonical query head drifted; re-derive the outer-leg probe, do not hand-copy it")
	}
	cut := strings.Index(prod, tf8875OuterLegOrderMarker)
	if cut < 0 {
		t.Fatalf("outer-leg boundary marker %q missing; re-derive the probe, do not hand-copy it",
			tf8875OuterLegOrderMarker)
	}
	derived := prod[:cut]
	armAt := strings.Index(derived, tf8875OuterLegArm)
	if armAt < 0 {
		t.Fatalf("derived probe no longer covers the outer leg (%q absent); re-derive, do not hand-copy it",
			tf8875OuterLegArm)
	}
	outer := derived[armAt:]
	for _, conjunct := range []string{"fact.fact_kind = 'file'", "fact.source_system = 'git'"} {
		if !strings.Contains(outer, conjunct) {
			t.Fatalf("outer-leg conjunct %q absent; the filter shape changed, re-derive the probe", conjunct)
		}
	}
	querySum := outerLegCASESum(t, derived)
	queryPred := normalizeSQLPredicate("WHERE fact_kind = 'file' AND source_system = 'git' AND ( " + querySum + " ) > 0")

	const indexName = "fact_records_terraform_backends_outer_leg_idx"
	migrationPred := migrationPredicate(t, "276_fact_records_terraform_backends_outer_leg_idx.sql")
	ddlPred := ddlPredicate(t, "schema_fact_records_terraform_backends.go", indexName)
	if queryPred != migrationPred {
		t.Fatalf("outer-leg predicate drifted from migration 276 predicate:\nquery:     %s\nmigration: %s",
			queryPred, migrationPred)
	}
	if ddlPred != migrationPred {
		t.Fatalf("Go DDL predicate drifted from migration 276 predicate:\nddl:       %s\nmigration: %s",
			ddlPred, migrationPred)
	}

	// Plan proof, DSN-backed: the derived probe must seek the serving
	// index on the shared seeded fixture shape. The hermetic lockstep
	// above runs everywhere, including hermetic CI.
	if os.Getenv(driftJSONBNullDSNEnv) == "" {
		t.Logf("DSN-backed plan assertion skipped: %s is empty", driftJSONBNullDSNEnv)
		return
	}
	db := openDriftJSONBNullDB(t)
	tf8875SeedBackendsFixture(t, db)
	rows, err := db.QueryContext(context.Background(), "EXPLAIN "+derived)
	if err != nil {
		t.Fatalf("EXPLAIN error = %v, want nil", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan error = %v, want nil", err)
		}
		plan.WriteString(line + "\n")
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows error = %v, want nil", err)
	}
	if !strings.Contains(plan.String(), "Index Scan using "+indexName) {
		t.Fatalf("derived probe does not seek the serving index %s:\n%s", indexName, plan.String())
	}
}
