// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

var (
	dsRepoScanOnce       sync.Once
	dsRepoScanRoot       string
	dsRepoScanRead       map[string]bool
	dsRepoScanProbe      map[string]bool
	dsRepoScanUnresolved []string
	dsRepoScanErr        error
)

// dsRepoScan runs dsScanErr once per test binary over the read-only repository
// go/ tree so TestDocsTestInputsStayCode and TestLockstepEntriesStayRead share
// one scan. Scratch-root tests call dsScan directly without caching.
func dsRepoScan(t *testing.T) (string, map[string]bool, map[string]bool, []string) {
	t.Helper()
	dsRepoScanOnce.Do(func() {
		root := filepath.Join("..", "..", "..")
		dsRepoScanRoot = root
		dsRepoScanRead, dsRepoScanProbe, dsRepoScanUnresolved, dsRepoScanErr = dsScanErr(root, filepath.Join(root, "go"))
	})
	if dsRepoScanErr != nil {
		t.Fatal(dsRepoScanErr)
	}
	return dsRepoScanRoot, dsRepoScanRead, dsRepoScanProbe, dsRepoScanUnresolved
}

// TestDocsTestInputsStayCode is the durable guard behind the P2 on #9215: a
// Go test that reads a docs-set file makes that file code, because editing it
// alone must still run the asserting test. The scanner classifies every
// docs-set path the tests name as read (R1 direct reads, R2 identifier flow,
// R3 referenced path-named constants, R4 map tables in reading files,
// Join-chain resolution) or probe (named but never read). Read files must sit
// in the lockstep manifest; probes must sit in probePaths. Anything the rules
// cannot resolve fails closed as an R7 demand for human classification.
func TestDocsTestInputsStayCode(t *testing.T) {
	t.Parallel()
	_, read, probe, unresolved := dsRepoScan(t)
	var demands []string
	for rel := range read {
		if !IsDocsSetPath(rel) {
			continue
		}
		demands = append(demands, rel+": test input stays code, add to the lockstep manifest")
	}
	probes := map[string]struct{}{}
	for _, p := range probePaths {
		probes[p] = struct{}{}
	}
	for rel := range probe {
		if !IsDocsSetPath(rel) {
			continue
		}
		if _, ok := probes[rel]; !ok {
			demands = append(demands, rel+": named-only probe, add to probePaths with verification")
		}
	}
	demands = append(demands, unresolved...)
	slices.Sort(demands)
	for _, d := range demands {
		t.Error(d)
	}
}

// TestLockstepEntriesStayRead closes the ratchet in the other direction: a
// manifest entry with no detected test reader rots into unjustified CI cost
// on docs-only PRs, and a probePaths entry no test names is a stale claim.
// manuallyVerifiedLockstep covers entries whose read shape (multi-hop field
// flow, variable repo roots) no static rule can see.
func TestLockstepEntriesStayRead(t *testing.T) {
	t.Parallel()
	root, read, probe, _ := dsRepoScan(t)
	manifest := dsManifest(t, root)
	observed := map[string]bool{}
	for rel := range read {
		observed[rel] = true
	}
	for rel := range probe {
		observed[rel] = true
	}
	for _, p := range manifest {
		if !observed[p] && manuallyVerifiedLockstep[p] == "" {
			t.Errorf("lockstep entry %s has no detected test reader; remove it or justify it in manuallyVerifiedLockstep", p)
		}
	}
	for p := range manuallyVerifiedLockstep {
		if !slices.Contains(manifest, p) {
			t.Errorf("manuallyVerifiedLockstep entry %s is not in the manifest", p)
		}
	}
	for _, p := range probePaths {
		if !observed[p] {
			t.Errorf("probePaths entry %s is never named by any Go test; drop the stale entry", p)
		}
	}
}
