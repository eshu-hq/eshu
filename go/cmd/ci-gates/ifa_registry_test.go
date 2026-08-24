// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
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
// on: every Odù ref named by a materialized_edges:* row in the coverage ledger,
// resolved to the directory whose cassette file is named for that ref.
//
// BOTH halves of that ledger (#6181). It read only
// specs/ifa-materialized-edge-coverage.v1.yaml, the shared-projection half,
// while the reconciliation the gate performs accepts rows from the direct half
// (specs/ifa-materialized-edge-coverage-direct.v1.yaml) on identical terms. So
// when #6228 replaces a direct family's waiver with a real coverage row, that
// row created no obligation here: its cassette could sit in a directory the
// gate does not trigger on, and the compiled-Odù/cassette lockstep would be
// silently skipped for it -- the same hole this test closed for the shared half,
// reopened one ledger file over. Seeding a direct row whose cassette lives in
// testdata/cassettes/replaydelta passed this test before the merged read and
// fails after it.
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

	// Read the manifest through the same parser the gate itself uses, rather
	// than regex-matching YAML text. Two hand-tuned patterns had to agree with
	// each other, and they agreed wrongly: a ref written single-quoted or as a
	// block scalar matched NEITHER, so both counts dropped it equally, the
	// equality check still held, and the family was silently skipped anyway --
	// the exact failure this test exists to prevent, reached through the
	// manifest's YAML form. A structural read handles every YAML spelling
	// because the parser does, and a non-Odù ref is simply skipped instead of
	// producing a false failure.
	// LoadMaterializedEdgeLedger, not a per-file LoadManifest: it is the one
	// entry point the reconciliation itself comes through, and its doc comment
	// says every caller reading the whole surface must use it rather than
	// opening one half. Going through it also means a row duplicated across the
	// halves is rejected here instead of being counted twice.
	manifest, _, err := materializededges.LoadMaterializedEdgeLedger(filepath.Join(root, "specs"))
	if err != nil {
		t.Fatalf("materializededges.LoadMaterializedEdgeLedger: %v", err)
	}
	if len(manifest.Coverage) < 10 {
		t.Fatalf("coverage manifest parsed %d row(s), want >= 10; the load has collapsed and every assertion below would pass vacuously", len(manifest.Coverage))
	}

	wantDirs := make(map[string]string)
	materialized := 0
	for _, entry := range manifest.Coverage {
		if !strings.HasPrefix(entry.Surface, "materialized_edges:") {
			continue
		}
		materialized++
		ref := strings.TrimPrefix(entry.Ref, "odu:")
		if ref == "" || ref == entry.Ref {
			t.Errorf("materialized_edges row %q carries ref %q, which is not an odù reference; the cassette directory cannot be derived from it", entry.Surface, entry.Ref)
			continue
		}
		hits, globErr := filepath.Glob(filepath.Join(root, "testdata", "cassettes", "*", ref+".json"))
		if globErr != nil {
			t.Fatalf("glob cassette for %s: %v", ref, globErr)
		}
		if len(hits) != 1 {
			t.Fatalf("odù ref %q resolves to %d cassette file(s), want exactly 1; the ref-to-directory derivation this test rests on no longer holds", ref, len(hits))
		}
		wantDirs[filepath.Base(filepath.Dir(hits[0]))] = ref
	}
	if materialized < 10 {
		t.Fatalf("found %d materialized_edges:* row(s), want >= 10; the surface filter has collapsed and every assertion below would pass vacuously", materialized)
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
