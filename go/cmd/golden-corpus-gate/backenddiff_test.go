// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
)

// writeBackendDiffDir records one backend's statements into dir using the
// real capture sink, so the gate phase test exercises the on-disk format.
func writeBackendDiffDir(t *testing.T, backend string, digests ...string) string {
	t.Helper()
	return writeBackendDiffDirStmt(t, backend, "MATCH (n) RETURN n", digests...)
}

// writeBackendDiffDirStmt is writeBackendDiffDir for an explicit statement,
// so quorum tests can diverge different statements per pairing.
func writeBackendDiffDirStmt(t *testing.T, backend, statement string, digests ...string) string {
	t.Helper()
	dir := t.TempDir()
	sink, err := capture.OpenDir(dir, backend, "testbin")
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	for _, digest := range digests {
		err := sink.Append(backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{
				Statement:  statement,
				Parameters: "{}",
			},
			Backend:  backend,
			RowCount: 1,
			Digest:   digest,
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

func writeEmptyBackendDiffAllowlist(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "allowlist.yaml")
	if err := os.WriteFile(path, []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func runBackendDiffPhase(t *testing.T, left, right, allowlist string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return run(context.Background(), []string{
		"-phase=backend-diff",
		"-diff-left=" + left,
		"-diff-right=" + right,
		"-diff-allowlist=" + allowlist,
	}, os.Getenv, &stdout, &stderr)
}

// The backend-diff phase is opt-in: an explicit -phase=backend-diff runs
// it, but -phase=all (every existing B-7 invocation) must not.
func TestBackendDiffPhaseSetOptIn(t *testing.T) {
	if !phaseSet("backend-diff")["backend-diff"] {
		t.Errorf("phaseSet(backend-diff) does not include backend-diff")
	}
	if phaseSet("all")["backend-diff"] {
		t.Errorf("phaseSet(all) must not include the opt-in backend-diff phase")
	}
	if !phaseSet("graph,backend-diff")["graph"] {
		t.Errorf("phaseSet(graph,backend-diff) lost the graph phase")
	}
	// An explicit request alongside "all" is honored: "all" expands without
	// swallowing the tokens after it.
	got := phaseSet("all,backend-diff")
	for _, p := range []string{"drains", "graph", "query", "timing", "demo-answers", "backend-diff"} {
		if !got[p] {
			t.Errorf("phaseSet(all,backend-diff) lost phase %q", p)
		}
	}
}

func TestRunBackendDiffClean(t *testing.T) {
	left := writeBackendDiffDir(t, "nornicdb", "abc123")
	right := writeBackendDiffDir(t, "neo4j", "abc123")
	if err := runBackendDiffPhase(t, left, right, writeEmptyBackendDiffAllowlist(t)); err != nil {
		t.Errorf("clean comparison failed the gate: %v", err)
	}
}

func TestRunBackendDiffDivergent(t *testing.T) {
	left := writeBackendDiffDir(t, "nornicdb", "abc123")
	right := writeBackendDiffDir(t, "neo4j", "def456")
	if err := runBackendDiffPhase(t, left, right, writeEmptyBackendDiffAllowlist(t)); err == nil {
		t.Errorf("divergent comparison passed the gate")
	}
}

// A seeded divergence the allowlist names passes: the excuse chain from
// flag to parse to Excuse is part of the gate's contract.
func TestRunBackendDiffAllowlisted(t *testing.T) {
	left := writeBackendDiffDir(t, "nornicdb", "abc123")
	right := writeBackendDiffDir(t, "neo4j", "def456")
	allowlist := filepath.Join(t.TempDir(), "allowlist.yaml")
	raw := "entries:\n- statement: \"MATCH (n) RETURN n\"\n  tier: \"statement\"\n  reason: \"seeded test divergence\"\n  upstream: \"https://github.com/eshu-hq/eshu/issues/6782\"\n"
	if err := os.WriteFile(allowlist, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := runBackendDiffPhase(t, left, right, allowlist); err != nil {
		t.Errorf("allowlisted divergence failed the gate: %v", err)
	}
}

// A missing backend side fails closed: comparing one backend against
// itself is not a differential proof.
// Flag values tolerate surrounding whitespace like every other path flag.
func TestRunBackendDiffPaddedFlags(t *testing.T) {
	left := writeBackendDiffDir(t, "nornicdb", "abc123")
	right := writeBackendDiffDir(t, "neo4j", "abc123")
	allowlist := writeEmptyBackendDiffAllowlist(t)
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"-phase=backend-diff",
		"-diff-left= " + left + " ",
		"-diff-right= " + right + " ",
		"-diff-allowlist= " + allowlist + " ",
	}, os.Getenv, &stdout, &stderr)
	if err != nil {
		t.Errorf("padded flags failed the gate: %v", err)
	}
}

func TestRunBackendDiffMissingBackend(t *testing.T) {
	left := writeBackendDiffDir(t, "nornicdb", "abc123")
	right := writeBackendDiffDir(t, "nornicdb", "abc123")
	if err := runBackendDiffPhase(t, left, right, writeEmptyBackendDiffAllowlist(t)); err == nil {
		t.Errorf("single-backend comparison passed the gate")
	}
}

// runBackendDiffQuorumPhase runs the backend-diff phase with two pairings,
// exercising the multi-leg quorum path end to end through flag parsing.
func runBackendDiffQuorumPhase(t *testing.T, left, right, left2, right2, allowlist string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return run(context.Background(), []string{
		"-phase=backend-diff",
		"-diff-left=" + left,
		"-diff-right=" + right,
		"-diff-left2=" + left2,
		"-diff-right2=" + right2,
		"-diff-allowlist=" + allowlist,
	}, os.Getenv, &stdout, &stderr)
}

// A divergence reproducing across both pairings fails quorum: the same
// statement answers differently on each backend in both pairings.
func TestRunBackendDiffQuorumReproducedFails(t *testing.T) {
	left := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1")
	right := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d2")
	left2 := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1")
	right2 := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d2")
	if err := runBackendDiffQuorumPhase(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t)); err == nil {
		t.Errorf("reproduced divergence passed quorum")
	}
}

// Pairing-local noise passes quorum: each pairing is red on its own
// divergence, but nothing reproduces, so the gate stays green.
func TestRunBackendDiffQuorumDisjointPasses(t *testing.T) {
	left := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (a) RETURN a", "d1")
	right := writeBackendDiffDirStmt(t, "neo4j", "MATCH (a) RETURN a", "d2")
	left2 := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (b) RETURN b", "d1")
	right2 := writeBackendDiffDirStmt(t, "neo4j", "MATCH (b) RETURN b", "d2")
	if err := runBackendDiffQuorumPhase(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t)); err != nil {
		t.Errorf("disjoint pairings failed quorum: %v", err)
	}
}

// Half a second pairing is a flag error, not a silent single-pair run.
func TestRunBackendDiffQuorumHalfFlagsFails(t *testing.T) {
	left := writeBackendDiffDir(t, "nornicdb", "abc123")
	right := writeBackendDiffDir(t, "neo4j", "abc123")
	left2 := writeBackendDiffDir(t, "nornicdb", "abc123")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"-phase=backend-diff",
		"-diff-left=" + left,
		"-diff-right=" + right,
		"-diff-left2=" + left2,
		"-diff-allowlist=" + writeEmptyBackendDiffAllowlist(t),
	}, os.Getenv, &stdout, &stderr)
	if err == nil {
		t.Errorf("half quorum flags passed the gate")
	}
}

// Repeating the first pairing as the second degrades quorum to single-pair:
// fail closed instead of silently passing noise through.
func TestRunBackendDiffQuorumIdenticalDirsFails(t *testing.T) {
	left := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (a) RETURN a", "d1")
	right := writeBackendDiffDirStmt(t, "neo4j", "MATCH (a) RETURN a", "d2")
	if err := runBackendDiffQuorumPhase(t, left, right, left, right, writeEmptyBackendDiffAllowlist(t)); err == nil {
		t.Errorf("identical quorum pairings passed the gate")
	}
}

// Stale-allowlist enforcement is per pairing, not weakened by quorum: an
// entry matching pairing 1 but nothing in pairing 2 fails the gate.
func TestRunBackendDiffQuorumStalePerPairingFails(t *testing.T) {
	left := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1")
	right := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d2")
	left2 := writeBackendDiffDir(t, "nornicdb", "abc123")
	right2 := writeBackendDiffDir(t, "neo4j", "abc123")
	allowlist := filepath.Join(t.TempDir(), "allowlist.yaml")
	raw := "entries:\n- statement: \"MATCH (s) RETURN s\"\n  tier: \"statement\"\n  reason: \"seeded test divergence\"\n  upstream: \"https://github.com/eshu-hq/eshu/issues/6782\"\n"
	if err := os.WriteFile(allowlist, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := runBackendDiffQuorumPhase(t, left, right, left2, right2, allowlist); err == nil {
		t.Errorf("pairing-local stale entry passed quorum")
	}
}

// runBackendDiffQuorumPhaseOutput is runBackendDiffQuorumPhase keeping the
// gate's stdout, so a test can assert which finding carried a verdict.
func runBackendDiffQuorumPhaseOutput(t *testing.T, left, right, left2, right2, allowlist string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"-phase=backend-diff",
		"-diff-left=" + left,
		"-diff-right=" + right,
		"-diff-left2=" + left2,
		"-diff-right2=" + right2,
		"-diff-allowlist=" + allowlist,
	}, os.Getenv, &stdout, &stderr)
	return stdout.String(), err
}

// An execution-count divergence that reproduces across both pairings is
// still not a backend divergence: NornicDB and Neo4j drain at different
// speeds, so pass counts differ systematically and quorum cannot filter
// them. The gate reports it as an advisory finding and stays green (#6782
// permanent disposition).
func TestRunBackendDiffQuorumReproducedExecutionsIsAdvisory(t *testing.T) {
	left := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1", "d1")
	right := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d1", "d1", "d1")
	left2 := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1", "d1")
	right2 := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d1", "d1", "d1")
	out, err := runBackendDiffQuorumPhaseOutput(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t))
	if err != nil {
		t.Fatalf("reproduced executions-only divergence failed quorum: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[WARN] nornicdb_vs_neo4j_executions") {
		t.Fatalf("stdout = %q, want an advisory executions finding", out)
	}
	if !strings.Contains(out, "MATCH (s) RETURN s") {
		t.Fatalf("stdout = %q, want the advisory finding to name the statement", out)
	}
}

// The advisory rule is kind-scoped: a reproduced results divergence beside
// reproduced execution noise still fails the gate.
func TestRunBackendDiffQuorumResultsBesideExecutionsFails(t *testing.T) {
	writePair := func() (string, string) {
		nornic := writeBackendDiffDirStmt(t, "nornicdb", "MATCH (s) RETURN s", "d1", "d1")
		neo := writeBackendDiffDirStmt(t, "neo4j", "MATCH (s) RETURN s", "d1", "d1", "d1")
		appendStmt(t, nornic, "nornicdb", "MATCH (r) RETURN r", "left")
		appendStmt(t, neo, "neo4j", "MATCH (r) RETURN r", "right")
		return nornic, neo
	}
	left, right := writePair()
	left2, right2 := writePair()
	out, err := runBackendDiffQuorumPhaseOutput(t, left, right, left2, right2, writeEmptyBackendDiffAllowlist(t))
	if err == nil {
		t.Fatalf("reproduced results divergence passed quorum beside execution noise\n%s", out)
	}
	if !strings.Contains(out, "found 1 reproduced divergence(s), first: MATCH (r) RETURN r") {
		t.Fatalf("stdout = %q, want the required finding to count only the results divergence", out)
	}
	// The executions divergence was routed to advisory, not dropped.
	if !strings.Contains(out, "[WARN] nornicdb_vs_neo4j_executions: 1 reproduced execution-count divergence(s)") {
		t.Fatalf("stdout = %q, want the executions divergence routed to the advisory finding", out)
	}
	// Both divergences reproduced in both pairings, so nothing was
	// pairing-local: 2+2 unexcused minus 2*2 reproduced. Pins the dropped
	// arithmetic against a regression to counting only the required subset.
	if !strings.Contains(out, "0 pairing-local divergence(s) did not reproduce") {
		t.Fatalf("stdout = %q, want zero pairing-local divergences", out)
	}
}

// appendStmt adds one more recorded statement to an existing capture dir.
func appendStmt(t *testing.T, dir, backend, statement, digest string) {
	t.Helper()
	sink, err := capture.OpenDir(dir, backend, "testbin-extra")
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	err = sink.Append(backendconformance.DifferentialRecord{
		Fingerprint: backendconformance.DifferentialFingerprint{Statement: statement, Parameters: "{}"},
		Backend:     backend,
		RowCount:    1,
		Digest:      digest,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
