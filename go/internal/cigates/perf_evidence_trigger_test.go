// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestPerfEvidenceSelectsSingleFileReducerCommandChange proves the
// perf-evidence gate selects a PR that touches only one hot-path file under
// go/cmd/reducer. The verifier already classifies that tree as hot by
// location (is_hot_path_by_location in scripts/verify-performance-evidence.sh),
// so a registry without the trigger would print "no trigger matched changed
// paths" and run nothing locally while CI's verify-contracts step still
// flags the change. Seed a defect by deleting the go/cmd/reducer/** trigger
// from the perf-evidence row and this test names the unselected file (#8009).
func TestPerfEvidenceSelectsSingleFileReducerCommandChange(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	reg, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}

	for _, path := range []string{
		"go/cmd/reducer/run.go",
		"go/cmd/reducer/main.go",
		"go/cmd/reducer/observed_service_wiring.go",
	} {
		sels := reg.Select([]string{path}, cigates.TierPrePush)
		found := false
		for _, s := range sels {
			if s.Gate.ID != "perf-evidence" {
				continue
			}
			found = true
			if !s.Selected {
				t.Errorf("single-file change %s does not select perf-evidence: %s", path, s.Reason)
			}
		}
		if !found {
			t.Fatal("perf-evidence gate missing from the committed registry")
		}
	}
}
