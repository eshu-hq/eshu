// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package migrations

import (
	"strings"
	"testing"
)

// TestCVEIDExpressionStatisticsMigrationExists pins #10050's build: the
// bootstrap must carry a statistics object on the CVE key expression
// (payload->>'cve_id'), the single-expression shape the local theory proof
// showed removes the advisory-index walk. The multivariate shape is
// deliberately excluded: it bought nothing over the single expression in
// the proof (its MCV list stays empty at ANALYZE sample sizes).
func TestCVEIDExpressionStatisticsMigrationExists(t *testing.T) {
	t.Parallel()

	const wantFile = "283_fact_records_vulnerability_cve_id_stats.sql"
	defs := BootstrapDefinitions()
	var found *Definition
	for i := range defs {
		if strings.HasSuffix(defs[i].Path, "/"+wantFile) {
			found = &defs[i]
		}
	}
	if found == nil {
		t.Fatalf("bootstrap has no %s: #10050 needs an extended-statistics object on (payload->>'cve_id')", wantFile)
	}
	for _, want := range []string{
		"CREATE STATISTICS",
		"((payload->>'cve_id'))",
		"FROM fact_records",
		"ANALYZE fact_records",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Errorf("%s does not contain %q", wantFile, want)
		}
	}
	// The no-multivariate rule applies to the CREATE STATISTICS statement,
	// not the file's comments (which name fact_kind to explain the
	// exclusion): the ON list must hold exactly the one expression.
	// Statement lines start at column 0; comment lines start with "--".
	var statement []string
	inStatement := false
	for line := range strings.Lines(found.SQL) {
		code := strings.TrimLeft(strings.TrimRight(line, "\n"), " \t")
		switch {
		case strings.HasPrefix(code, "CREATE STATISTICS"):
			inStatement = true
			statement = append(statement, code)
		case inStatement && (strings.HasPrefix(code, "ON ") || strings.HasPrefix(code, "FROM ")):
			statement = append(statement, code)
			if strings.HasPrefix(code, "FROM ") {
				inStatement = false
			}
		case inStatement && strings.HasPrefix(code, "--"):
			t.Fatalf("%s: comment inside the CREATE STATISTICS statement: %q", wantFile, code)
		}
	}
	if len(statement) != 3 {
		t.Fatalf("%s: CREATE STATISTICS statement has %d lines, want CREATE/ON/FROM: %q", wantFile, len(statement), statement)
	}
	if strings.Contains(strings.Join(statement, "\n"), ",") {
		t.Errorf("%s must be a single-expression object: the multivariate shape bought nothing in the #10050 proof", wantFile)
	}
}
