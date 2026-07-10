// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestCommittedRegistrySelectsIfaBlockingGate proves the P4 (#4397)
// advisory->blocking flip landed for all three Ifa proof gates
// (ifa-contract-layer, ifa-determinism, ifa-dead-letter-matrix): each must
// still be selected for Ifa-owned paths at the pre-pr tier, and each must now
// report blocking=true in the committed registry. Before the flip this test
// asserted the opposite (blocking=false, "must start advisory") — see git
// history for TestCommittedRegistrySelectsIfaAdvisoryGate, the P1-era name
// this test replaces.
func TestCommittedRegistrySelectsIfaBlockingGate(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	root := repoRoot(t)
	paths := writePathsFile(t, t.TempDir(), []string{
		"go/internal/ifa/odu.go",
		"go/cmd/ifa/main.go",
	})
	cmd := exec.Command(
		bin, "select",
		"--registry", filepath.Join(root, "specs", "ci-gates.v1.yaml"),
		"--tier", "pre-pr",
		"--paths-from", paths,
		"--json",
	)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("select Ifa paths failed: %v\n%s", err, out)
	}
	var result selectJSONOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	var found bool
	for _, selected := range result.Selected {
		if selected.ID == "ifa-contract-layer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ifa-contract-layer not selected for Ifa paths; output:\n%s", out)
	}
	reg, err := cigates.Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}
	wantBlocking := map[string]bool{
		"ifa-contract-layer":     false,
		"ifa-determinism":        false,
		"ifa-dead-letter-matrix": false,
	}
	seen := map[string]bool{}
	for _, gate := range reg.Gates {
		if _, tracked := wantBlocking[gate.ID]; !tracked {
			continue
		}
		seen[gate.ID] = true
		if !gate.Blocking {
			t.Errorf("gate %q must be blocking after the P4 flip, got blocking=false", gate.ID)
		}
	}
	for id := range wantBlocking {
		if !seen[id] {
			t.Errorf("gate %q not found in ci-gates registry", id)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "go", "cmd", "ifa")); err != nil {
		t.Fatalf("cmd/ifa package missing: %v", err)
	}
}
