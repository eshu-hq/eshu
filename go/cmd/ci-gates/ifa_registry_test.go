// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// TestCoverageGateTriggersEveryFamilyCassette derives, rather than hard-codes,
// the set of cassette directories ifa-materialized-edge-coverage must trigger
// on: every Odù ref named by a materialized_edges:* row in
// specs/ifa-materialized-edge-coverage.v1.yaml, resolved to the directory whose
// cassette file is named for that ref.
//
// That gate is the one which RECONCILES the coverage manifest. A cassette edit
// that changes facts while leaving the edge set identical is invisible to the
// two live gates — their assertions compare edge sets — and is caught only by
// the compiled-Odù/cassette lockstep this gate runs. So a family whose cassette
// is not in its trigger list can have that lockstep silently skipped.
//
// The list was hand-maintained and carried three of twelve directories when
// #6212 measured it, with no stated rule for the other nine. Deriving the
// requirement here means adding a family cannot quietly omit its cassette: the
// manifest row is what creates the obligation, and this test is what enforces
// it. Pair it with scripts/test-verify-ci-gates-registry.sh, which separately
// asserts each declared glob is mirrored into the workflow filter — together
// they close both halves of the registry/workflow lockstep.
func TestCoverageGateTriggersEveryFamilyCassette(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	manifest, err := os.ReadFile(filepath.Join(root, "specs", "ifa-materialized-edge-coverage.v1.yaml"))
	if err != nil {
		t.Fatalf("read coverage manifest: %v", err)
	}

	refs := regexp.MustCompile(`ref:\s*"odu:([a-z0-9-]+)"`).FindAllStringSubmatch(string(manifest), -1)
	if len(refs) < 10 {
		t.Fatalf("found %d odù ref(s) in the coverage manifest, want >= 10; the parse has collapsed and every assertion below would pass vacuously", len(refs))
	}

	wantDirs := make(map[string]string)
	for _, match := range refs {
		ref := match[1]
		hits, globErr := filepath.Glob(filepath.Join(root, "testdata", "cassettes", "*", ref+".json"))
		if globErr != nil {
			t.Fatalf("glob cassette for %s: %v", ref, globErr)
		}
		if len(hits) != 1 {
			t.Fatalf("odù ref %q resolves to %d cassette file(s), want exactly 1; the ref-to-directory derivation this test rests on no longer holds", ref, len(hits))
		}
		wantDirs[filepath.Base(filepath.Dir(hits[0]))] = ref
	}
	if len(wantDirs) < 10 {
		t.Fatalf("derived only %d cassette director(ies), want >= 10", len(wantDirs))
	}

	// Assert against the LOADED gate's active Triggers, not the raw YAML text.
	// A substring match on the block would also match a commented-out trigger:
	// `# - "testdata/cassettes/codeowners/**"` still contains the path, so a
	// disabled trigger would read as present while the selector had stopped
	// scheduling the gate — recreating the exact silent skip this test exists to
	// prevent. Parsing is what distinguishes a live trigger from its corpse.
	gate := coverageGate(t, filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	active := make(map[string]struct{}, len(gate.Triggers))
	for _, trigger := range gate.Triggers {
		active[trigger] = struct{}{}
	}
	for dir, ref := range wantDirs {
		trigger := "testdata/cassettes/" + dir + "/**"
		if _, ok := active[trigger]; !ok {
			t.Errorf("ifa-materialized-edge-coverage does not trigger on %q (odù %s); a cassette edit that leaves the edge set identical would skip this gate's compiled-Odù lockstep", trigger, ref)
		}
	}
}

// coverageGate loads the registry and returns the ifa-materialized-edge-coverage
// entry, so callers assert on parsed triggers rather than on YAML text.
func coverageGate(t *testing.T, registryPath string) cigates.Gate {
	t.Helper()

	reg, err := cigates.Load(registryPath)
	if err != nil {
		t.Fatalf("cigates.Load(%s): %v", registryPath, err)
	}
	for _, gate := range reg.Gates {
		if gate.ID == "ifa-materialized-edge-coverage" {
			if len(gate.Triggers) == 0 {
				t.Fatal("ifa-materialized-edge-coverage parsed with zero triggers; every assertion below would pass vacuously")
			}
			return gate
		}
	}
	t.Fatal("ifa-materialized-edge-coverage not present in the loaded registry")
	return cigates.Gate{}
}
