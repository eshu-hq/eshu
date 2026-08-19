// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
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

// materializedEdgeCountClaimFile is a file that states a family count in prose,
// with the exact number of claims it is expected to make.
//
// Claims is pinned rather than checked as "at least one": two of these files
// carry two claims each, so rephrasing one past the regex leaves the other
// matching and the file still looks covered. The escaped claim then goes stale
// unnoticed — the same false green this gate exists to prevent, one level down.
//
// Changing a count claim's wording is therefore a deliberate edit here, not a
// silent drop.
type materializedEdgeCountClaimFile struct {
	Path   string
	Claims int
}

// materializedEdgeCountClaimFiles lists the files whose prose a reader will
// believe. A file is here because it makes a claim, not because it is Go or
// YAML. Adding a family, or proving one, moves these numbers, and nothing in
// the build forces the prose to follow — which is how four of the five claims
// drifted at once.
var materializedEdgeCountClaimFiles = []materializedEdgeCountClaimFile{
	{filepath.Join("specs", MaterializedEdgeManifestFileName), 2},
	{filepath.Join("go", "internal", "ifa", "materializededges", "materialized_edges_lockstep_test.go"), 1},
	{filepath.Join("go", "internal", "reducer", "materialized_edge_families.go"), 2},
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

	// Count the families that still carry a waiver, rather than subtracting the
	// families that have any coverage row.
	//
	// Coverage and waivers are both per (surface, proof_gate): a family can have
	// a proven baseline while its fault dimension is still waived, and that
	// partial state is not merely allowed, it is what incremental #5543 work
	// looks like on the way through. Counting "has any coverage row" as done
	// would drop such a family out of the waived total the moment its first
	// dimension lands, so the gate would demand a prose number that is wrong —
	// rejecting accurate prose during exactly the work it is meant to track.
	//
	// "Families with at least one remaining waiver" is what the sentences mean
	// by "the N other allProjectionDomains families".
	waivers, err := LoadMaterializedEdgeWaivers(filepath.Join(repoRoot, "specs", MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}
	waivedFamilies := map[string]struct{}{}
	for _, w := range waivers {
		name, ok := strings.CutPrefix(w.Surface, MaterializedEdgeSurfacePrefix)
		if !ok {
			continue
		}
		waivedFamilies[name] = struct{}{}
	}
	waived := len(waivedFamilies)

	if total == 0 || waived > total {
		t.Fatalf("derived counts are nonsense: total=%d waived=%d", total, waived)
	}
	for name := range waivedFamilies {
		if !slices.Contains(families, name) {
			t.Errorf("waiver names %q, which is not an allProjectionDomains family; the ledger and reducer.MaterializedEdgeFamilies() disagree", name)
		}
	}

	checked := 0
	perFile := map[string]int{}
	for _, cf := range materializedEdgeCountClaimFiles {
		rel := cf.Path
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
	for _, cf := range materializedEdgeCountClaimFiles {
		if got := perFile[cf.Path]; got != cf.Claims {
			t.Errorf("%s makes %d family-count claim(s) matching %q, want exactly %d; a claim was added or reworded past this gate, and a claim this gate cannot see is a claim that goes stale silently",
				cf.Path, got, familyCountClaim.String(), cf.Claims)
		}
	}
	if checked == 0 {
		t.Fatalf("no family-count claims matched %q in %v; the wording changed and this gate went vacuous",
			familyCountClaim.String(), materializedEdgeCountClaimFiles)
	}
	t.Logf("checked %d family-count claim(s): total=%d waived=%d", checked, total, waived)
}
