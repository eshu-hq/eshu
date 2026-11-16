// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunCoverageHonorsRepoRoot: the cassette-backed Odùs resolve against the
// repository root, which defaults to the parent of -specs-dir, so every
// existing invocation keeps working, and an explicit -repo-root that holds no
// cassette fails the command in advisory mode too. A missing recording is a
// broken reconcile, not a coverage gap: an Odù read from nothing would pass
// every assertion made on it.
func TestRunCoverageHonorsRepoRoot(t *testing.T) {
	repoRoot := repoRootDir(t)
	base := []string{
		"coverage",
		"-specs-dir", filepath.Join(repoRoot, "specs"),
		"-snapshot", filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json"),
	}

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), base, &stdout, &stderr); err != nil {
		t.Fatalf("run(coverage) with the default repo root = %v, stderr=%s", err, stderr.String())
	}

	empty := t.TempDir()
	stdout.Reset()
	stderr.Reset()
	err := run(context.Background(), append(append([]string{}, base...), "-repo-root", empty), &stdout, &stderr)
	if err == nil {
		t.Fatal("run(coverage -repo-root <empty dir>) = nil, want an error: the cassette-backed Odùs cannot resolve there")
	}
	if !strings.Contains(err.Error(), "vuln-collector-nvd.json") {
		t.Errorf("error = %v, want it to name the missing cassette", err)
	}
}
