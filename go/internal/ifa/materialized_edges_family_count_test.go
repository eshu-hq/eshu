// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// familyCountClaim matches the prose counts this repo writes about the
// allProjectionDomains family set, in both Go comments and the YAML ledger.
//
// Two shapes carry different meanings and must not be conflated:
//
//	"the 13 other allProjectionDomains families"  -> the WAIVED subset
//	"the 14 allProjectionDomains families"        -> the WHOLE set
//
// The optional "other"/"not-yet-covered" qualifier is what distinguishes them,
// and it appears on BOTH sides of the number in committed prose ("the other 13
// allProjectionDomains members" as well as "the 13 other ..."). Matching only
// one order silently reclassifies a waived-subset claim as a whole-set claim
// and reports a correct sentence as wrong, which is how this gate first failed.
var familyCountClaim = regexp.MustCompile(`(?:(other|not-yet-covered)\s+)?(\d+)\s+(?:(other|not-yet-covered)\s+)?allProjectionDomains`)

// materializedEdgeCountClaimFiles are the files that state a family count in
// prose. A file is listed here because it makes a claim a reader will believe,
// not because it is Go or YAML.
//
// Adding a family, or proving one, changes these numbers. Nothing in the build
// forces the prose to follow, which is exactly how four of the five claims in
// this list drifted at once.
var materializedEdgeCountClaimFiles = []string{
	filepath.Join("specs", MaterializedEdgeManifestFileName),
	filepath.Join("go", "internal", "ifa", "materialized_edges_lockstep_test.go"),
	filepath.Join("go", "internal", "reducer", "materialized_edge_families.go"),
}

// TestMaterializedEdgeFamilyCountClaimsMatchTheCode is a #5543 anti-drift gate.
//
// The materialized-edge ledger is read as a statement of how much work is left:
// "the N other allProjectionDomains families" is how a maintainer sizes the
// remaining per-domain child issues. When that number is stale the ledger
// understates the backlog, and no existing gate catches it — the coverage gate
// checks rows, not the sentences describing them.
//
// It had drifted in four places at once, spanning 11, 12, and 13 against a real
// total of 14, because each family added and each dimension proven moves the
// numbers and the prose was updated by hand or not at all.
//
// This derives both counts from reducer.MaterializedEdgeFamilies() and the
// committed manifest, then holds every prose claim to them.
func TestMaterializedEdgeFamilyCountClaimsMatchTheCode(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	families := reducer.MaterializedEdgeFamilies()
	total := len(families)

	manifest, err := replaycoverage.LoadManifest(filepath.Join(repoRoot, "specs", MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	covered := map[string]struct{}{}
	for _, sc := range manifest.Coverage {
		name, ok := strings.CutPrefix(sc.Surface, MaterializedEdgeSurfacePrefix)
		if !ok {
			continue
		}
		covered[name] = struct{}{}
	}
	waived := total - len(covered)

	if total == 0 || waived < 0 || waived > total {
		t.Fatalf("derived counts are nonsense: total=%d covered=%d waived=%d", total, len(covered), waived)
	}

	checked := 0
	perFile := map[string]int{}
	for _, rel := range materializedEdgeCountClaimFiles {
		path := filepath.Join(repoRoot, rel)
		raw, err := os.ReadFile(path) // #nosec G304 -- repo-relative path from a fixed list.
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, m := range familyCountClaim.FindAllStringSubmatch(line, -1) {
				claimed, convErr := strconv.Atoi(m[2])
				if convErr != nil {
					continue
				}
				checked++
				perFile[rel]++
				want, label := total, "every allProjectionDomains family"
				if strings.TrimSpace(m[1]) != "" || strings.TrimSpace(m[3]) != "" {
					want, label = waived, "the waived (not-yet-covered) allProjectionDomains families"
				}
				if claimed != want {
					t.Errorf("%s:%d claims %d for %s, but the code says %d\n  line: %s\n  fix the prose, not this test — the count comes from reducer.MaterializedEdgeFamilies() and the committed coverage rows",
						rel, i+1, claimed, label, want, strings.TrimSpace(line))
				}
			}
		}
	}

	// A gate that matches nothing passes for the wrong reason, and the check has
	// to be PER FILE. A single total lets one file be reworded past the regex
	// while the others keep the count non-zero — the file silently leaves
	// coverage and its next stale number ships unnoticed. That hole was real
	// here: rewording one claim left the suite green.
	//
	// If a file legitimately stops making a count claim, drop it from
	// materializedEdgeCountClaimFiles deliberately rather than letting it fall
	// out by accident.
	for _, rel := range materializedEdgeCountClaimFiles {
		if perFile[rel] == 0 {
			t.Errorf("%s makes no family-count claim matching %q; either its wording drifted past this gate or it should be removed from materializedEdgeCountClaimFiles on purpose",
				rel, familyCountClaim.String())
		}
	}
	if checked == 0 {
		t.Fatalf("no family-count claims matched %q in %v; the wording changed and this gate went vacuous",
			familyCountClaim.String(), materializedEdgeCountClaimFiles)
	}
	t.Logf("checked %d family-count claim(s): total=%d covered=%d waived=%d", checked, total, len(covered), waived)
}
