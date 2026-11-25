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

// TestCommittedRegistrySelectsIfaBlockingGate proves the blocking Ifa proof
// gates (ifa-determinism, ifa-dead-letter-matrix) are selected for Ifa-owned
// paths and remain blocking in the committed registry.
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
		if selected.ID == "ifa-determinism" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ifa-determinism not selected for Ifa paths; output:\n%s", out)
	}
	reg, err := cigates.Load(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}
	wantBlocking := map[string]bool{
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
			t.Errorf("gate %q must be blocking, got blocking=false", gate.ID)
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
