// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// referenceFindingSort is the pre-#10057 ordering, kept as the oracle: it
// marshals the stable key inside the comparator, which is slow but is the
// behavior the precomputed-key sort must reproduce byte for byte.
func referenceFindingSort(findings []SupplyChainImpactFinding, tieBreak bool) {
	sort.Slice(findings, func(i, j int) bool {
		left, right := supplyChainImpactStableKey(findings[i]), supplyChainImpactStableKey(findings[j])
		if left != right {
			return left < right
		}
		if !tieBreak {
			return false
		}
		if findings[i].MatchReason != findings[j].MatchReason {
			return findings[i].MatchReason < findings[j].MatchReason
		}
		return findings[i].ImageRef < findings[j].ImageRef
	})
}

// sortFixtureFindings builds findings that exercise every comparator level:
// distinct identities across every key field, identities shared by several
// findings that differ only in MatchReason or ImageRef, and findings tied on
// all three that differ only in a field the comparator ignores.
func sortFixtureFindings() []SupplyChainImpactFinding {
	statuses := []SupplyChainImpactStatus{
		SupplyChainImpactAffectedExact, SupplyChainImpactPossiblyAffected, SupplyChainImpactNotAffectedKnownFixed,
	}
	var out []SupplyChainImpactFinding
	for cve := 0; cve < 3; cve++ {
		for version := 0; version < 3; version++ {
			for digest := 0; digest < 4; digest++ {
				base := SupplyChainImpactFinding{
					CVEID:           fmt.Sprintf("CVE-2099-%04d", cve),
					AdvisoryID:      fmt.Sprintf("DSA-%d", cve%2),
					PackageID:       "pkg:deb/debian/curl",
					PURL:            fmt.Sprintf("pkg:deb/debian/curl@%d", cve%2),
					ProductCriteria: fmt.Sprintf("criteria-%d", cve%2),
					MatchCriteriaID: fmt.Sprintf("match-%d", cve%3),
					ObservedVersion: fmt.Sprintf("1:1.0-%d", version),
					RequestedRange:  "<1:1.0-2",
					Status:          statuses[(cve+version+digest/2)%len(statuses)],
					SubjectDigest:   fmt.Sprintf("sha256:%064d", digest),
					RepositoryID:    fmt.Sprintf("repo-%d", version%2),
				}
				if cve == 2 {
					base.ScanTargetID = "scan-target-a"
				}
				for _, reason := range []string{"reason_b", "reason_a"} {
					for _, ref := range []string{"registry/img:2", "registry/img:1"} {
						finding := base
						finding.MatchReason = reason
						finding.ImageRef = ref
						out = append(out, finding)
					}
				}
				// Tied on identity, reason, and image ref; only ignored fields differ.
				for _, fact := range []string{"fact-b", "fact-a"} {
					finding := base
					finding.MatchReason = "reason_a"
					finding.ImageRef = "registry/img:1"
					finding.EvidenceFactIDs = []string{fact}
					out = append(out, finding)
				}
			}
		}
	}
	// Whitespace around identity fields is trimmed by the key, not by the sort.
	padded := out[0]
	padded.CVEID = " " + padded.CVEID + " "
	padded.EvidenceFactIDs = []string{"padded"}
	return append(out, padded)
}

// findingOrderMatchesReference sorts shuffled copies of the fixture with sorter
// and with the reference, and reports whether the full JSON of every finding
// lands in the same position every time.
func findingOrderMatchesReference(t *testing.T, tieBreak bool, sorter func([]SupplyChainImpactFinding)) bool {
	t.Helper()
	fixture := sortFixtureFindings()
	rng := rand.New(rand.NewSource(10057))
	for permutation := 0; permutation < 40; permutation++ {
		rng.Shuffle(len(fixture), func(i, j int) { fixture[i], fixture[j] = fixture[j], fixture[i] })
		want := append([]SupplyChainImpactFinding(nil), fixture...)
		got := append([]SupplyChainImpactFinding(nil), fixture...)
		referenceFindingSort(want, tieBreak)
		sorter(got)
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal reference order: %v", err)
		}
		gotJSON, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal sorted order: %v", err)
		}
		if string(wantJSON) != string(gotJSON) {
			return false
		}
	}
	return true
}

// TestFindingSortMatchesMarshalInComparatorOrder pins that the precomputed-key
// sort orders findings exactly as the marshal-per-comparison sort did, ties
// included, for the tie-broken first pass and the key-only second pass.
func TestFindingSortMatchesMarshalInComparatorOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		tieBreak bool
	}{
		{"first pass breaks ties on reason and image ref", true},
		{"second pass sorts on the key alone", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !findingOrderMatchesReference(t, tc.tieBreak, func(findings []SupplyChainImpactFinding) {
				sortSupplyChainImpactFindings(findings, tc.tieBreak)
			}) {
				t.Fatal("precomputed-key sort ordered findings differently from the reference sort")
			}
		})
	}
}

// TestFindingSortFixtureDetectsAWrongKey proves the equivalence check can fail:
// sorters that build a deliberately wrong key, or drop a tie-break, must not
// match the reference, so a pass above is evidence and not an empty comparison.
func TestFindingSortFixtureDetectsAWrongKey(t *testing.T) {
	t.Parallel()
	wrongKeys := map[string]func(SupplyChainImpactFinding) string{
		"key ignores observed version": func(f SupplyChainImpactFinding) string {
			f.ObservedVersion = ""
			return supplyChainImpactStableKey(f)
		},
		"key ignores subject digest": func(f SupplyChainImpactFinding) string {
			f.SubjectDigest = ""
			return supplyChainImpactStableKey(f)
		},
		"key ignores status": func(f SupplyChainImpactFinding) string {
			f.Status = ""
			return supplyChainImpactStableKey(f)
		},
		"key is the untrimmed CVE": func(f SupplyChainImpactFinding) string {
			return f.CVEID + supplyChainImpactStableKey(f)
		},
	}
	for name, keyOf := range wrongKeys {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if findingOrderMatchesReference(t, true, func(findings []SupplyChainImpactFinding) {
				sort.Slice(findings, func(i, j int) bool {
					left, right := keyOf(findings[i]), keyOf(findings[j])
					if left != right {
						return left < right
					}
					if findings[i].MatchReason != findings[j].MatchReason {
						return findings[i].MatchReason < findings[j].MatchReason
					}
					return findings[i].ImageRef < findings[j].ImageRef
				})
			}) {
				t.Fatalf("%s matched the reference order, so the fixture cannot see this key defect", name)
			}
		})
	}
	t.Run("tie-break dropped", func(t *testing.T) {
		t.Parallel()
		if findingOrderMatchesReference(t, true, func(findings []SupplyChainImpactFinding) {
			sortSupplyChainImpactFindings(findings, false)
		}) {
			t.Fatal("key-only sort matched the tie-broken reference, so the fixture cannot see a dropped tie-break")
		}
	})
}

// TestCoalesceResortsAfterAMergeChangesAFindingKey pins the sort that follows
// the merge loop. Two findings share an identity and disagree on reason with an
// SBOM component read, so the merge turns affected_exact into
// possibly_affected, and the status is part of the sort key. The merged finding
// must therefore move behind its not_affected_known_fixed neighbor.
func TestCoalesceResortsAfterAMergeChangesAFindingKey(t *testing.T) {
	t.Parallel()
	build := func(version string, status SupplyChainImpactStatus, reason string) SupplyChainImpactFinding {
		return SupplyChainImpactFinding{
			CVEID: "CVE-2099-9946", PackageID: "pkg:deb/debian/curl", ObservedVersion: version,
			PURL: "pkg:deb/debian/curl@" + version, Status: status, Confidence: "exact", MatchReason: reason,
			SubjectDigest: "sha256:" + strings.Repeat("b", 64), EvidencePath: []string{chain.SBOMComponentFactKind},
		}
	}
	got := coalesceAndSortSupplyChainImpactFindings([]SupplyChainImpactFinding{
		build("1:1.0-3", SupplyChainImpactAffectedExact, "reason_a"),
		build("1:1.0-3", SupplyChainImpactAffectedExact, "reason_b"),
		build("1:1.0-4", SupplyChainImpactNotAffectedKnownFixed, "reason_a"),
	})
	var statuses []SupplyChainImpactStatus
	for _, finding := range got {
		statuses = append(statuses, finding.Status)
	}
	if len(got) != 2 || got[0].Status != SupplyChainImpactNotAffectedKnownFixed || got[1].Status != SupplyChainImpactPossiblyAffected {
		t.Fatalf("statuses = %v, want [not_affected_known_fixed possibly_affected]", statuses)
	}
}
