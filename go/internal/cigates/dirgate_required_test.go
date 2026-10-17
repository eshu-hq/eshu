// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestDirgateIsRequiredOnEveryPullRequest pins that the dirgate job is a
// required check on a pull request that changes no go/** path. The job runs on
// every pull request, but required-gates-complete awaits only the gates that
// cigates.RequiredGates selects through triggers. Without an always-selected
// trigger on go-dir-gate, a docs-only change selects it zero times and a stray
// Dirgate-Growth trailer there is never awaited, yet still counts toward the
// absolute invariant's W. Seed a defect by dropping the "**" trigger from the
// go-dir-gate row and this test names the unselected path.
func TestDirgateIsRequiredOnEveryPullRequest(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}
	for _, path := range []string{
		"docs/a.md",
		"README.md",
		".agents/skills/x/SKILL.md",
		".github/workflows/x.yml",
		"specs/x.yaml",
		"scripts/other.sh",
		"go/internal/telemetry/x.go",
	} {
		required, err := reg.RequiredGates([]string{path})
		if err != nil {
			t.Fatalf("RequiredGates(%s): %v", path, err)
		}
		found := false
		for _, gate := range required {
			if gate.Workflow == "test.yml" && gate.Job == "dirgate" && slices.Contains(gate.GateIDs, "go-dir-gate") {
				found = true
			}
		}
		if !found {
			t.Errorf("a change to %s does not require go-dir-gate on the dirgate job of test.yml", path)
		}
	}
}
