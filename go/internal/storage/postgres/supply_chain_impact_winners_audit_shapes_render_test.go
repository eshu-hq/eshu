// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is test support, not a product command: it renders the audit statements that read the
// supports table (#10880 PR-C2 l) as psql-ready files, so a read-only run on a replica can show
// their plans against the supports indexes (T1: audit over supports against the fact baseline).
// Set ESHU_RENDER_EXPLAIN_DIR to a directory to write them; without it the test checks the
// shapes and skips the write. The statements are read-only and each file sets a 20 s
// statement_timeout. The pending select and the verify statements read session temp tables, so
// they are not rendered; they carry the same swapped fragments the derivation test pins.

type auditExplainShape struct {
	name      string
	statement string
	// args are the psql expressions bound to $1..$n, in order.
	args []string
}

func auditExplainShapes() []auditExplainShape {
	cursor := []string{"'pkg:npm/a'", "'repository:example'", "'sha256:0'", "'CVE-2026-0001'", "'GHSA-xxxx-xxxx-xxxx'"}
	return []auditExplainShape{
		{"audit_first_range", selectWinnersAuditFirstRangeFromSupportsSQL, []string{"5000"}},
		{"audit_next_range", selectWinnersAuditNextRangeFromSupportsSQL, append([]string{"5000"}, cursor...)},
		{"audit_null_probe", probeWinnersAuditNullKeysFromSupportsSQL, nil},
		{"staged_source_batch", stagedWinnersSourceBatchSQL, []string{":'scope'", ":'generation'"}},
		{"staged_operator_batch", stagedWinnersOperatorBatchSQL, []string{"'operator:vulnerability_suppressions'", ":'generation'"}},
		{"staged_ident_fetch", stagedWinnerFactIdentFromSupportsSQL, []string{":'fact_id'"}},
	}
}

func renderAuditExplainShape(shape auditExplainShape) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- Audit shape: %s, over the supports table (#10880 PR-C2 l). Read-only; override with psql -v name=value.\n", shape.name)
	b.WriteString("\\set ON_ERROR_STOP on\n")
	for _, d := range [][2]string{{"scope", "repository:example"}, {"generation", "generation:example"}, {"fact_id", "fact:example"}} {
		fmt.Fprintf(&b, "\\if :{?%s}\n\\else\n\\set %s '%s'\n\\endif\n", d[0], d[0], d[1])
	}
	b.WriteString("SET statement_timeout = '20s';\nPREPARE q AS\n")
	b.WriteString(strings.TrimSpace(shape.statement))
	b.WriteString(";\n-- for the plan without running the statement, drop ANALYZE from the line below\n")
	if len(shape.args) == 0 {
		b.WriteString("EXPLAIN (ANALYZE, BUFFERS) EXECUTE q;\n")
	} else {
		fmt.Fprintf(&b, "EXPLAIN (ANALYZE, BUFFERS) EXECUTE q(%s);\n", strings.Join(shape.args, ", "))
	}
	b.WriteString("DEALLOCATE q;\n")
	return b.String()
}

// TestRenderAuditExplainShapes writes the audit shape files to ESHU_RENDER_EXPLAIN_DIR. Without a
// directory it still checks that every shape renders over the supports table, with its binds
// matching the statement's highest parameter, and no fact_records read.
func TestRenderAuditExplainShapes(t *testing.T) {
	for _, shape := range auditExplainShapes() {
		text := renderAuditExplainShape(shape)
		if strings.Contains(shape.statement, "fact_records") || !strings.Contains(shape.statement, "supply_chain_impact_finding_supports") {
			t.Fatalf("shape %s does not read only the supports table", shape.name)
		}
		highest := 0
		for n := 1; n <= 9; n++ {
			if strings.Contains(shape.statement, fmt.Sprintf("$%d", n)) {
				highest = n
			}
		}
		if highest != len(shape.args) {
			t.Fatalf("shape %s binds %d values but the statement takes %d", shape.name, len(shape.args), highest)
		}
		if !strings.Contains(text, "PREPARE q AS") || strings.Count(text, "EXECUTE q") != 1 {
			t.Fatalf("shape %s did not render a PREPARE and one EXECUTE", shape.name)
		}
	}
	dir := os.Getenv("ESHU_RENDER_EXPLAIN_DIR")
	if dir == "" {
		t.Skip("set ESHU_RENDER_EXPLAIN_DIR to write the audit explain shape files")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	for _, shape := range auditExplainShapes() {
		if err := os.WriteFile(filepath.Join(dir, shape.name+".sql"), []byte(renderAuditExplainShape(shape)), 0o644); err != nil {
			t.Fatalf("write %s: %v", shape.name, err)
		}
	}
}
