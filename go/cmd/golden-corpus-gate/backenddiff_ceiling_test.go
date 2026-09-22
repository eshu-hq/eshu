// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// writeExecutionsAdvisoryDirs builds two leg pairings with n distinct
// statements, each reproducing an executions-kind divergence in both
// pairings (nornicdb records 2 executions, neo4j records 3, same digest set,
// so the results agree and only the execution count differs). The #6941
// ceiling counts len(advisory), so this fixture is what grows past a ceiling
// under test.
func writeExecutionsAdvisoryDirs(t *testing.T, n int) (left, right, left2, right2 string) {
	t.Helper()
	left, right, left2, right2 = t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	for i := range n {
		stmt := fmt.Sprintf("MATCH (s%d) RETURN s%d", i, i)
		for _, dir := range []string{left, left2} {
			appendStmt(t, dir, "nornicdb", stmt, "d1")
			appendStmt(t, dir, "nornicdb", stmt, "d1")
		}
		for _, dir := range []string{right, right2} {
			appendStmt(t, dir, "neo4j", stmt, "d1")
			appendStmt(t, dir, "neo4j", stmt, "d1")
			appendStmt(t, dir, "neo4j", stmt, "d1")
		}
	}
	return left, right, left2, right2
}

// runBackendDiffQuorumPhaseWithCeiling is runBackendDiffQuorumPhaseOutput
// plus the #6941 -diff-executions-advisory-max flag under test.
func runBackendDiffQuorumPhaseWithCeiling(t *testing.T, left, right, left2, right2, allowlist string, ceiling int) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"-phase=backend-diff",
		"-diff-left=" + left,
		"-diff-right=" + right,
		"-diff-left2=" + left2,
		"-diff-right2=" + right2,
		"-diff-allowlist=" + allowlist,
		fmt.Sprintf("-diff-executions-advisory-max=%d", ceiling),
	}, os.Getenv, &stdout, &stderr)
	return stdout.String(), err
}

// A reproduced advisory execution-count total within the ceiling passes: the
// existing advisory finding still prints, and no ceiling finding is added
// (#6941 scenario a).
func TestRunBackendDiffQuorumExecutionsWithinCeilingPasses(t *testing.T) {
	left, right, left2, right2 := writeExecutionsAdvisoryDirs(t, 3)
	out, err := runBackendDiffQuorumPhaseWithCeiling(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t), 5)
	if err != nil {
		t.Fatalf("within-ceiling run failed the gate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[WARN] nornicdb_vs_neo4j_executions") {
		t.Fatalf("stdout = %q, want the advisory executions finding", out)
	}
	if strings.Contains(out, "nornicdb_vs_neo4j_executions_ceiling") {
		t.Fatalf("stdout = %q, want no ceiling finding while within the ceiling", out)
	}
}

// A total exactly at the ceiling passes: the ceiling is exclusive ("above
// it" adds the finding), so len(advisory) == max adds no ceiling finding
// (#6941 boundary; a > to >= slip in the phase fails here).
func TestRunBackendDiffQuorumExecutionsAtCeilingPasses(t *testing.T) {
	left, right, left2, right2 := writeExecutionsAdvisoryDirs(t, 3)
	out, err := runBackendDiffQuorumPhaseWithCeiling(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t), 3)
	if err != nil {
		t.Fatalf("at-ceiling run failed the gate: %v\n%s", err, out)
	}
	if strings.Contains(out, "nornicdb_vs_neo4j_executions_ceiling") {
		t.Fatalf("stdout = %q, want no ceiling finding at exactly the ceiling", out)
	}
}

// A reproduced advisory execution-count total above the ceiling is a
// required, failing finding naming the observed count and the ceiling
// (#6941 scenario b).
func TestRunBackendDiffQuorumExecutionsAboveCeilingFails(t *testing.T) {
	left, right, left2, right2 := writeExecutionsAdvisoryDirs(t, 3)
	out, err := runBackendDiffQuorumPhaseWithCeiling(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t), 2)
	if err == nil {
		t.Fatalf("above-ceiling run passed the gate\n%s", out)
	}
	if !strings.Contains(out, "[FAIL] nornicdb_vs_neo4j_executions_ceiling") {
		t.Fatalf("stdout = %q, want the required ceiling finding", out)
	}
	if !strings.Contains(out, "3 reproduced execution-count divergence") {
		t.Fatalf("stdout = %q, want the observed count named", out)
	}
	if !strings.Contains(out, "ceiling of 2") {
		t.Fatalf("stdout = %q, want the ceiling value named", out)
	}
}

// A ceiling of 0 disables the check regardless of the reproduced advisory
// count (#6941 scenario c).
func TestRunBackendDiffQuorumExecutionsCeilingDisabledNeverFails(t *testing.T) {
	left, right, left2, right2 := writeExecutionsAdvisoryDirs(t, 3)
	out, err := runBackendDiffQuorumPhaseWithCeiling(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t), 0)
	if err != nil {
		t.Fatalf("disabled ceiling (0) failed the gate: %v\n%s", err, out)
	}
	if strings.Contains(out, "nornicdb_vs_neo4j_executions_ceiling") {
		t.Fatalf("stdout = %q, want no ceiling finding when disabled", out)
	}
}

// The advisory finding's detail names the top reproduced statements by
// count instead of only the first one recorded (#6941 scenario d). Four
// statements each contribute exactly one advisory divergence, so the top-3
// tie-break (statement text ascending) keeps s0-s2 and drops s3.
func TestRunBackendDiffQuorumAdvisoryDetailNamesTopStatements(t *testing.T) {
	left, right, left2, right2 := writeExecutionsAdvisoryDirs(t, 4)
	out, err := runBackendDiffQuorumPhaseOutput(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t))
	if err != nil {
		t.Fatalf("executions-only divergence failed quorum: %v\n%s", err, out)
	}
	idx := strings.Index(out, "[WARN] nornicdb_vs_neo4j_executions:")
	if idx < 0 {
		t.Fatalf("stdout = %q, want the advisory executions finding", out)
	}
	advisoryLine := out[idx:]
	if nl := strings.IndexByte(advisoryLine, '\n'); nl >= 0 {
		advisoryLine = advisoryLine[:nl]
	}
	for _, want := range []string{"MATCH (s0) RETURN s0 (1)", "MATCH (s1) RETURN s1 (1)", "MATCH (s2) RETURN s2 (1)"} {
		if !strings.Contains(advisoryLine, want) {
			t.Fatalf("advisory line = %q, want it to name %q", advisoryLine, want)
		}
	}
	// s3 loses the tie-break (statement text descends after s2), so the
	// advisory line names only the top 3 -- it still appears in the raw
	// per-pairing dump above, which is not what this assertion scopes.
	if strings.Contains(advisoryLine, "MATCH (s3) RETURN s3") {
		t.Fatalf("advisory line = %q, want only the top 3 statements named, not s3", advisoryLine)
	}
}
