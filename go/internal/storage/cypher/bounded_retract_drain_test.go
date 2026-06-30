// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
	"testing"
)

// TestBuildBoundedRetractDrainCypherRewritesFilesStatement verifies the rewrite
// for canonicalNodeRetractFilesCypher (full-refresh, unbounded).
func TestBuildBoundedRetractDrainCypherRewritesFilesStatement(t *testing.T) {
	t.Parallel()

	got, err := BuildBoundedRetractDrainCypher(canonicalNodeRetractFilesCypher, "f", "__retract_batch")
	if err != nil {
		t.Fatalf("BuildBoundedRetractDrainCypher() error = %v, want nil", err)
	}
	// Must end with bounded drain + RETURN clause.
	if !strings.Contains(got, "WITH f LIMIT $__retract_batch") {
		t.Fatalf("rewritten Cypher missing LIMIT clause:\n%s", got)
	}
	if !strings.Contains(got, "DETACH DELETE f") {
		t.Fatalf("rewritten Cypher missing DETACH DELETE f:\n%s", got)
	}
	if !strings.Contains(got, "RETURN count(f) AS __drained") {
		t.Fatalf("rewritten Cypher missing RETURN count(f):\n%s", got)
	}
	// Original WHERE clause must be preserved.
	if !strings.Contains(got, "f.generation_id <> $generation_id") {
		t.Fatalf("rewritten Cypher dropped WHERE clause:\n%s", got)
	}
}

// TestBuildBoundedRetractDrainCypherRewritesRemovedFilesStatement verifies the
// rewrite for canonicalNodeRetractRemovedFilesCypher (full-refresh with file-list guard).
func TestBuildBoundedRetractDrainCypherRewritesRemovedFilesStatement(t *testing.T) {
	t.Parallel()

	got, err := BuildBoundedRetractDrainCypher(canonicalNodeRetractRemovedFilesCypher, "f", "__retract_batch")
	if err != nil {
		t.Fatalf("BuildBoundedRetractDrainCypher() error = %v, want nil", err)
	}
	if !strings.Contains(got, "WITH f LIMIT $__retract_batch") {
		t.Fatalf("rewritten Cypher missing LIMIT clause:\n%s", got)
	}
	if !strings.Contains(got, "DETACH DELETE f") {
		t.Fatalf("rewritten Cypher missing DETACH DELETE f:\n%s", got)
	}
	if !strings.Contains(got, "RETURN count(f) AS __drained") {
		t.Fatalf("rewritten Cypher missing RETURN:\n%s", got)
	}
	// NOT IN guard from original must be preserved.
	if !strings.Contains(got, "NOT (f.path IN $file_paths)") {
		t.Fatalf("rewritten Cypher dropped NOT IN guard:\n%s", got)
	}
}

// TestBuildBoundedRetractDrainCypherRewritesDirectoriesStatement verifies the
// rewrite for canonicalNodeRetractDirectoriesCypher (var "d", bare-label shape).
// NornicDB v1.1.9 requires ORDER BY elementId() before LIMIT for bare-label queries.
func TestBuildBoundedRetractDrainCypherRewritesDirectoriesStatement(t *testing.T) {
	t.Parallel()

	got, err := BuildBoundedRetractDrainCypher(canonicalNodeRetractDirectoriesCypher, "d", "__retract_batch")
	if err != nil {
		t.Fatalf("BuildBoundedRetractDrainCypher() error = %v, want nil", err)
	}
	// Bare-label shape: must use ORDER BY elementId() before LIMIT.
	if !strings.Contains(got, "WITH d ORDER BY elementId(d) LIMIT $__retract_batch") {
		t.Fatalf("rewritten Cypher missing ORDER BY elementId(d) LIMIT clause:\n%s", got)
	}
	if !strings.Contains(got, "DETACH DELETE d") {
		t.Fatalf("rewritten Cypher missing DETACH DELETE d:\n%s", got)
	}
	if !strings.Contains(got, "RETURN count(d) AS __drained") {
		t.Fatalf("rewritten Cypher missing RETURN:\n%s", got)
	}
}

// TestBuildBoundedRetractDrainCypherRewritesEntityStatement verifies the
// rewrite for canonicalNodeRetractEntityTemplate (var "n", bare-label shape).
// NornicDB v1.1.9 requires ORDER BY elementId() before LIMIT for bare-label queries.
func TestBuildBoundedRetractDrainCypherRewritesEntityStatement(t *testing.T) {
	t.Parallel()

	cypher := "MATCH (n:Function)\nWHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id\nDETACH DELETE n"
	got, err := BuildBoundedRetractDrainCypher(cypher, "n", "__retract_batch")
	if err != nil {
		t.Fatalf("BuildBoundedRetractDrainCypher() error = %v, want nil", err)
	}
	// Bare-label shape: must use ORDER BY elementId() before LIMIT.
	if !strings.Contains(got, "WITH n ORDER BY elementId(n) LIMIT $__retract_batch") {
		t.Fatalf("rewritten Cypher missing ORDER BY elementId(n) LIMIT clause:\n%s", got)
	}
	if !strings.Contains(got, "DETACH DELETE n") {
		t.Fatalf("rewritten Cypher missing DETACH DELETE n:\n%s", got)
	}
	if !strings.Contains(got, "RETURN count(n) AS __drained") {
		t.Fatalf("rewritten Cypher missing RETURN:\n%s", got)
	}
	if !strings.Contains(got, "n.generation_id <> $generation_id") {
		t.Fatalf("rewritten Cypher dropped WHERE clause:\n%s", got)
	}
}

// TestBuildBoundedRetractDrainCypherErrorsOnWrongTrailingVerb verifies that
// passing a cypher that does NOT end with "DETACH DELETE <drainVar>" returns
// an error rather than silently producing wrong Cypher.
func TestBuildBoundedRetractDrainCypherErrorsOnWrongTrailingVerb(t *testing.T) {
	t.Parallel()

	_, err := BuildBoundedRetractDrainCypher("MATCH (f:File)\nDELETE f", "f", "__retract_batch")
	if err == nil {
		t.Fatal("BuildBoundedRetractDrainCypher() error = nil, want non-nil for cypher without DETACH DELETE f")
	}
	if !strings.Contains(err.Error(), "DETACH DELETE f") {
		t.Fatalf("error = %v, want message mentioning DETACH DELETE f", err)
	}
}

// TestBuildBoundedRetractDrainCypherShapeClassification is a table-driven guard
// that runs all four production retract statements through BuildBoundedRetractDrainCypher
// and asserts the exact WITH clause emitted per statement. If a future edit changes
// the MATCH shape of any statement (e.g. makes a bare-label template anchored), this
// test will fail rather than silently draining zero nodes on NornicDB v1.1.9.
func TestBuildBoundedRetractDrainCypherShapeClassification(t *testing.T) {
	t.Parallel()

	const bp = "__retract_batch"
	cases := []struct {
		name       string
		cypher     string
		drainVar   string
		wantWith   string
		wantNoWith string
	}{
		{
			name:       "canonicalNodeRetractFilesCypher relationship-anchored",
			cypher:     canonicalNodeRetractFilesCypher,
			drainVar:   "f",
			wantWith:   "WITH f LIMIT $" + bp,
			wantNoWith: "ORDER BY",
		},
		{
			name:       "canonicalNodeRetractRemovedFilesCypher relationship-anchored",
			cypher:     canonicalNodeRetractRemovedFilesCypher,
			drainVar:   "f",
			wantWith:   "WITH f LIMIT $" + bp,
			wantNoWith: "ORDER BY",
		},
		{
			name:       "canonicalNodeRetractDirectoriesCypher bare-label",
			cypher:     canonicalNodeRetractDirectoriesCypher,
			drainVar:   "d",
			wantWith:   "WITH d ORDER BY elementId(d) LIMIT $" + bp,
			wantNoWith: "",
		},
		{
			name:       "canonicalNodeRetractEntityTemplate bare-label (Function)",
			cypher:     fmt.Sprintf(canonicalNodeRetractEntityTemplate, "Function"),
			drainVar:   "n",
			wantWith:   "WITH n ORDER BY elementId(n) LIMIT $" + bp,
			wantNoWith: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := BuildBoundedRetractDrainCypher(tc.cypher, tc.drainVar, bp)
			if err != nil {
				t.Fatalf("BuildBoundedRetractDrainCypher() error = %v, want nil", err)
			}
			if !strings.Contains(got, tc.wantWith) {
				t.Fatalf("rewritten Cypher missing expected WITH clause %q:\n%s", tc.wantWith, got)
			}
			if tc.wantNoWith != "" && strings.Contains(got, tc.wantNoWith) {
				t.Fatalf("rewritten Cypher must not contain %q for anchored shape:\n%s", tc.wantNoWith, got)
			}
		})
	}
}

// TestBuildBoundedRetractDrainCypherErrorsOnWrongVar verifies that a mismatch
// between drainVar and the actual trailing DETACH DELETE target is rejected.
func TestBuildBoundedRetractDrainCypherErrorsOnWrongVar(t *testing.T) {
	t.Parallel()

	_, err := BuildBoundedRetractDrainCypher(canonicalNodeRetractFilesCypher, "x", "__retract_batch")
	if err == nil {
		t.Fatal("BuildBoundedRetractDrainCypher() error = nil, want non-nil for wrong drainVar")
	}
}
