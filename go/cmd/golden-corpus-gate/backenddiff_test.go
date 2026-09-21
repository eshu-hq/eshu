// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/graph/capture"
)

// writeBackendDiffDir records one backend's statements into dir using the
// real capture sink, so the gate phase test exercises the on-disk format.
func writeBackendDiffDir(t *testing.T, backend string, digests ...string) string {
	t.Helper()
	dir := t.TempDir()
	sink, err := capture.OpenDir(dir, backend, "testbin")
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	for _, digest := range digests {
		err := sink.Append(backendconformance.DifferentialRecord{
			Fingerprint: backendconformance.DifferentialFingerprint{
				Statement:  "MATCH (n) RETURN n",
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
