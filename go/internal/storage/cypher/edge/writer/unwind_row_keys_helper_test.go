// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// unwindMapBinding matches `UNWIND $<param> AS <var>` so the row-key guard can
// find which parameter list feeds which bound variable.
var unwindMapBinding = regexp.MustCompile(`UNWIND\s+\$([A-Za-z_][A-Za-z0-9_]*)\s+AS\s+([A-Za-z_][A-Za-z0-9_]*)`)

// unwindRowKeyViolations reports every row map in params that lacks a key the
// statement reads through its UNWIND variable (`<var>.<key>`).
//
// The pinned NornicDB v1.3.3 does not read a missing row-map key as null the
// way Neo4j does: `SET x.p = row.key` stores the literal text "row.key" when
// the key is absent (#6782). A writer must therefore send every key its
// statement references, with nil when there is no value. The check only
// applies to UNWIND parameters that are lists of maps; scalar lists (for
// example `UNWIND $uids AS uid`) are skipped because they have no keys.
//
// It returns one human-readable violation per offending row, and the number
// of rows it inspected so callers can fail a test that checked nothing.
func unwindRowKeyViolations(cypher string, params map[string]any) (violations []string, checked int) {
	for _, binding := range unwindMapBinding.FindAllStringSubmatchIndex(cypher, -1) {
		param := cypher[binding[2]:binding[3]]
		variable := cypher[binding[4]:binding[5]]
		rows, ok := unwindParamRows(params[param])
		if !ok {
			continue
		}
		reference := regexp.MustCompile(`\b` + regexp.QuoteMeta(variable) + `\.([A-Za-z_][A-Za-z0-9_]*)`)
		referenced := map[string]struct{}{}
		for _, match := range reference.FindAllStringSubmatch(cypher[binding[1]:], -1) {
			referenced[match[1]] = struct{}{}
		}
		for index, row := range rows {
			checked++
			var missing []string
			for key := range referenced {
				if _, present := row[key]; !present {
					missing = append(missing, key)
				}
			}
			if len(missing) == 0 {
				continue
			}
			sort.Strings(missing)
			violations = append(violations, fmt.Sprintf(
				"$%s[%d] omits %s, which the statement reads as %s.<key>",
				param, index, strings.Join(missing, ", "), variable,
			))
		}
	}
	return violations, checked
}

// unwindParamRows returns the row maps of an UNWIND parameter, or false when
// the parameter is not a list of maps.
func unwindParamRows(value any) ([]map[string]any, bool) {
	switch typed := value.(type) {
	case []map[string]any:
		return typed, true
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			rows = append(rows, row)
		}
		return rows, true
	default:
		return nil, false
	}
}

// assertUnwindRowsCarryReferencedKeys fails the test when any statement sends
// a row map without a key its UNWIND statement reads. It returns the number of
// rows checked so a caller can assert the guard actually saw its rows.
//
// Any writer test that records its statements can call it:
//
//	checked := assertUnwindRowsCarryReferencedKeys(t, executor.calls)
func assertUnwindRowsCarryReferencedKeys(t testing.TB, statements []sourcecypher.Statement) int {
	t.Helper()
	total := 0
	for _, statement := range statements {
		violations, checked := unwindRowKeyViolations(statement.Cypher, statement.Parameters)
		total += checked
		if len(violations) > 0 {
			t.Errorf("the pinned NornicDB stores the literal expression text for a missing UNWIND row key:\n%s\nstatement:\n%s",
				strings.Join(violations, "\n"), statement.Cypher)
		}
	}
	return total
}

// TestUnwindRowKeyViolationsSeeded is the seeded-violation RED/GREEN pair for
// the guard itself: a planted row without a referenced key must be reported,
// and the same statement with every key present (nil allowed) must pass.
func TestUnwindRowKeyViolationsSeeded(t *testing.T) {
	t.Parallel()

	const statement = `UNWIND $rows AS row
MATCH (a:Repository {id: row.repo_id})
MATCH (b:Repository {id: row.target_repo_id})
MERGE (a)-[rel:DEPENDS_ON]->(b)
SET rel.source_tool = row.source_tool`

	t.Run("planted missing key is reported", func(t *testing.T) {
		t.Parallel()
		violations, checked := unwindRowKeyViolations(statement, map[string]any{
			"rows": []map[string]any{
				{"repo_id": "a", "target_repo_id": "b", "source_tool": "helm"},
				{"repo_id": "a", "target_repo_id": "c"},
			},
		})
		if checked != 2 {
			t.Fatalf("checked = %d, want 2", checked)
		}
		if len(violations) != 1 || !strings.Contains(violations[0], "$rows[1] omits source_tool") {
			t.Fatalf("violations = %q, want one naming $rows[1] and source_tool", violations)
		}
	})

	t.Run("explicit nil passes", func(t *testing.T) {
		t.Parallel()
		violations, checked := unwindRowKeyViolations(statement, map[string]any{
			"rows": []any{
				map[string]any{"repo_id": "a", "target_repo_id": "c", "source_tool": nil},
			},
		})
		if checked != 1 || len(violations) != 0 {
			t.Fatalf("checked = %d, violations = %q; want 1 and none", checked, violations)
		}
	})

	t.Run("scalar unwind list is skipped", func(t *testing.T) {
		t.Parallel()
		violations, checked := unwindRowKeyViolations(
			"UNWIND $uids AS uid MATCH (n {uid: uid}) RETURN n.uid",
			map[string]any{"uids": []string{"u1"}},
		)
		if checked != 0 || len(violations) != 0 {
			t.Fatalf("checked = %d, violations = %q; want 0 and none", checked, violations)
		}
	})

	t.Run("variable other than row is honoured", func(t *testing.T) {
		t.Parallel()
		violations, _ := unwindRowKeyViolations(
			"UNWIND $pairs AS pair MATCH (a {id: pair.left}) MATCH (b {id: pair.right}) MERGE (a)-[:R]->(b)",
			map[string]any{"pairs": []map[string]any{{"left": "a"}}},
		)
		if len(violations) != 1 || !strings.Contains(violations[0], "right") {
			t.Fatalf("violations = %q, want one naming right", violations)
		}
	})
}
