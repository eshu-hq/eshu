// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
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

// A reproduced advisory scheduling-noise total within the ceiling passes:
// the existing advisory finding still prints, and no ceiling finding is
// added (#6941 scenario a).
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

// A reproduced advisory scheduling-noise total above the ceiling is a
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
	if !strings.Contains(out, "3 reproduced scheduling-noise divergence") {
		t.Fatalf("stdout = %q, want the observed count named", out)
	}
	if !strings.Contains(out, "ceiling of 2") {
		t.Fatalf("stdout = %q, want the ceiling value named", out)
	}
}

// Transient-read exclusions count toward the same ceiling tripwire: a
// systematic divergence on a registered statement must not hide behind
// timing noise indefinitely (#6971 P2). One reproduced advisory plus one
// reproduced transient is 2 against a ceiling of 1, so the gate fails.
// RED: transient is not counted, so the gate passes.
func TestRunBackendDiffQuorumCeilingCountsTransient(t *testing.T) {
	const orphan = "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"
	writePair := func() (string, string) {
		nornic := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1", "d1")
		neo := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d1", "d1", "d1")
		appendStmt(t, nornic, "nornicdb", orphan, "left")
		appendStmt(t, neo, "neo4j", orphan, "right")
		return nornic, neo
	}
	left, right := writePair()
	left2, right2 := writePair()
	allowlist := filepath.Join(t.TempDir(), "allowlist.yaml")
	raw := "transient_reads:\n- statement: \"" + orphan + "\"\n  reason: \"seeded transient read\"\n  upstream: \"https://github.com/eshu-hq/eshu/issues/6782\"\n  owner: \"graph\"\n"
	if err := os.WriteFile(allowlist, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := runBackendDiffQuorumPhaseWithCeiling(t, left, right, left2, right2, allowlist, 1)
	if err == nil {
		t.Fatalf("advisory-plus-transient total above the ceiling passed the gate\n%s", out)
	}
	if !strings.Contains(out, "[FAIL] nornicdb_vs_neo4j_executions_ceiling") {
		t.Fatalf("stdout = %q, want the required ceiling finding", out)
	}
	if !strings.Contains(out, "(1 scheduling-noise + 1 transient-read)") {
		t.Fatalf("stdout = %q, want the ceiling finding to break out both buckets", out)
	}
}

// The ceiling stays exclusive across buckets: one advisory plus one
// transient is exactly the ceiling of 2, so the gate passes with no
// ceiling finding (boundary pin for the mixed total).
func TestRunBackendDiffQuorumCeilingMixedTotalAtCeilingPasses(t *testing.T) {
	const orphan = "MATCH (n:Module) WHERE n.uid IS NULL RETURN n"
	nornic := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1", "d1")
	neo := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d1", "d1", "d1")
	appendStmt(t, nornic, "nornicdb", orphan, "left")
	appendStmt(t, neo, "neo4j", orphan, "right")
	nornic2 := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1", "d1")
	neo2 := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d1", "d1", "d1")
	appendStmt(t, nornic2, "nornicdb", orphan, "left")
	appendStmt(t, neo2, "neo4j", orphan, "right")
	allowlist := filepath.Join(t.TempDir(), "allowlist.yaml")
	raw := "transient_reads:\n- statement: \"" + orphan + "\"\n  reason: \"seeded transient read\"\n  upstream: \"https://github.com/eshu-hq/eshu/issues/6782\"\n  owner: \"graph\"\n"
	if err := os.WriteFile(allowlist, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	out, err := runBackendDiffQuorumPhaseWithCeiling(t, nornic, neo, nornic2, neo2, allowlist, 2)
	if err != nil {
		t.Fatalf("mixed total at the ceiling failed the gate: %v\n%s", err, out)
	}
	if strings.Contains(out, "nornicdb_vs_neo4j_executions_ceiling") {
		t.Fatalf("stdout = %q, want no ceiling finding at exactly the ceiling", out)
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

// A negative ceiling is refused at flag parsing instead of silently
// disabling the tripwire the way 0 does (#6941 review thread): a safety
// ceiling fails closed on invalid input.
func TestParseFlagsRejectsNegativeExecutionsAdvisoryMax(t *testing.T) {
	_, err := parseFlags([]string{"-phase=backend-diff", "-diff-executions-advisory-max=-1"})
	if err == nil {
		t.Fatal("parseFlags accepted -diff-executions-advisory-max=-1, want an error")
	}
	if !strings.Contains(err.Error(), "must be >= 0") {
		t.Fatalf("error = %q, want it to say the flag must be >= 0", err)
	}
	if _, err := parseFlags([]string{"-phase=backend-diff", "-diff-executions-advisory-max=0"}); err != nil {
		t.Fatalf("parseFlags rejected the documented disable value 0: %v", err)
	}
}

// appendStmtRows writes one record per row count, sharing one digest for
// executions that returned rows and using the empty digest for zero-row
// executions, mirroring how the capture sink records poll iterations that
// observe a converged row one more time on one leg.
func appendStmtRows(t *testing.T, dir, backend, statement string, rows ...int) {
	t.Helper()
	sink, err := capture.OpenDir(dir, backend, "testbin-rowcount")
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	for _, n := range rows {
		digest := "d1"
		if n == 0 {
			digest = ""
		}
		if err := sink.Append(backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{Statement: statement, Parameters: "{}"},
			Backend:     backend,
			RowCount:    n,
			Digest:      digest,
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// A row-total divergence with agreeing results that reproduces across both
// pairings is advisory like execution counts (#6782 option-2 slice): one leg
// observed the converged row in one more poll iteration, so the totals
// differ while the digest sets and execution counts agree. Nornicdb records
// two one-row plus two zero-row executions per pairing (total 2); neo4j
// records one one-row plus three zero-row executions (total 1).
func TestRunBackendDiffQuorumReproducedRowcountIsAdvisory(t *testing.T) {
	writePair := func() (string, string) {
		nornic := t.TempDir()
		neo := t.TempDir()
		appendStmtRows(t, nornic, "nornicdb", "MATCH (p) RETURN p", 1, 1, 0, 0)
		appendStmtRows(t, neo, "neo4j", "MATCH (p) RETURN p", 1, 0, 0, 0)
		return nornic, neo
	}
	left, right := writePair()
	left2, right2 := writePair()
	out, err := runBackendDiffQuorumPhaseOutput(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t))
	if err != nil {
		t.Fatalf("reproduced rowcount divergence failed quorum: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[WARN] nornicdb_vs_neo4j_executions") {
		t.Fatalf("stdout = %q, want an advisory finding", out)
	}
	if !strings.Contains(out, "MATCH (p) RETURN p") {
		t.Fatalf("stdout = %q, want the advisory finding to name the statement", out)
	}
}
