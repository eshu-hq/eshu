// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"path/filepath"
	"testing"
)

// package cigates (not cigates_test) so this test can call the unexported
// scriptWorkflowSoundSubset directly, the same internal-test-package pattern
// required_workflow_test.go and trivyskipdirs_csv_test.go already use in this
// directory.

// TestScriptWorkflowSoundSubsetCount pins scriptWorkflowSoundSubsetCount
// against a live re-derivation from the committed registry and workflow
// files, using the same scriptWorkflowSoundSubset call
// checkVerifyScriptWorkflowMatch itself makes -- not a reimplementation of
// its loop, so the production check and this guard can never quietly
// disagree about what "the sound subset" means.
//
// This is the guard the "29 gates" figure in checkVerifyScriptWorkflowMatch's
// doc comment lacked: specs/ci-gates.v1.yaml grew past that count silently,
// and nothing caught the comment going stale. This test fails loudly the
// next time the registry moves the count instead of leaving a hard-coded
// number to rot silently in prose.
func TestScriptWorkflowSoundSubsetCount(t *testing.T) {
	t.Parallel()

	// go/internal/cigates -> internal -> go -> repo root, the same relative
	// path trivyskipdirs_csv_test.go uses in this same directory to reach the
	// committed specs/workflows tree without shelling out to git.
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))

	reg, err := Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}

	subset, err := scriptWorkflowSoundSubset(repoRoot, reg)
	if err != nil {
		t.Fatalf("scriptWorkflowSoundSubset: %v", err)
	}

	if got := len(subset); got != scriptWorkflowSoundSubsetCount {
		t.Errorf("scriptWorkflowSoundSubsetCount is %d, but the committed registry and "+
			"workflows currently derive %d gates in the sound subset (exactly one workflow "+
			"runs their verify script); update the scriptWorkflowSoundSubsetCount constant "+
			"and its doc comment in scriptworkflow.go to match",
			scriptWorkflowSoundSubsetCount, got)
	}
}
