// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// advisoryGroupResolverForTest builds the production resolver over a
// catalog holding exactly the given Odù under the family Odù's name.
func advisoryGroupResolverForTest(t *testing.T, odu ifa.Odu) MaterializedEdgeOduResolver {
	t.Helper()
	return MaterializedEdgeOduResolver{
		Catalog:  map[string]ifa.Odu{ifa.AdvisoryGroupFamilyOduName: odu},
		RepoRoot: repoRootDir(t),
	}
}

func advisoryGroupEntryForTest() replaycoverage.CoverageEntry {
	return replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + advisoryGroupFamily,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          ifa.AdvisoryGroupFamilyOduName,
	}
}

// TestAdvisoryGroupGuardResolvesThroughResolver proves the guard is
// reachable by surface name through MaterializedEdgeOduResolver, not
// only by calling it directly. A guard nothing dispatches to is not
// coverage.
func TestAdvisoryGroupGuardResolvesThroughResolver(t *testing.T) {
	t.Parallel()

	catalog := ifa.CatalogByName()
	odu, ok := catalog[ifa.AdvisoryGroupFamilyOduName]
	if !ok {
		t.Fatalf("Odù %q is not in ifa.Catalog(); a coverage row naming it could never resolve", ifa.AdvisoryGroupFamilyOduName)
	}
	covered, detail := advisoryGroupResolverForTest(t, odu).Resolve(advisoryGroupEntryForTest())
	if !covered {
		t.Fatalf("resolver reported advisory_group uncovered: %s", detail)
	}
	t.Logf("advisory_group: %s", detail)
}

// TestAdvisoryGroupGuardRejectsMembersLessOdu proves the #10466
// fail-closed conditions beyond factlessness: stripping the provenance
// members off every finding, or reducing them to each row's own ids,
// must red even though BuildGroup would still produce the same two
// edges — the member union on the node is part of the claim, and a
// guard that let it go vacuous would certify the #10475 failure shape.
func TestAdvisoryGroupGuardRejectsMembersLessOdu(t *testing.T) {
	t.Parallel()

	base := ifa.CatalogByName()[ifa.AdvisoryGroupFamilyOduName]
	mutateMembers := func(members any) ifa.Odu {
		out := ifa.Odu{Name: base.Name, Facts: make([]facts.Envelope, 0, len(base.Facts))}
		for _, env := range base.Facts {
			clone := env.Clone()
			if clone.FactKind == facts.ReducerSupplyChainImpactFindingFactKind {
				provenance, _ := clone.Payload["provenance"].(map[string]any)
				next := make(map[string]any, len(provenance))
				for k, v := range provenance {
					next[k] = v
				}
				if members == nil {
					delete(next, "advisory_group_members")
				} else {
					next["advisory_group_members"] = members
				}
				clone.Payload["provenance"] = next
			}
			out.Facts = append(out.Facts, clone)
		}
		return out
	}

	cases := []struct {
		name    string
		odu     ifa.Odu
		wantSub string
	}{
		{name: "no members key", odu: mutateMembers(nil), wantSub: "carries no provenance advisory_group_members"},
		{name: "own ids only", odu: mutateMembers([]string{"CVE-2026-10466", "GHSA-10466-aaaa-bbbb"}), wantSub: "no member distinct from its own CVE/advisory ids"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			covered, detail := advisoryGroupResolverForTest(t, tc.odu).Resolve(advisoryGroupEntryForTest())
			if covered {
				t.Fatalf("guard reported covered: %s", detail)
			}
			if !strings.Contains(detail, tc.wantSub) {
				t.Fatalf("guard detail = %q, want substring %q", detail, tc.wantSub)
			}
		})
	}
}

// TestAdvisoryGroupIDParity pins the shared group-identity derivation
// against the reader's grouping rule: CVE when present, advisory
// otherwise, empty when neither — the id the writer MERGEs and the
// live assert-edges scan reads back off the node.
func TestAdvisoryGroupIDParity(t *testing.T) {
	t.Parallel()

	cases := []struct{ cve, advisory, want string }{
		{"CVE-2026-10466", "GHSA-10466-aaaa-bbbb", "CVE-2026-10466"},
		{"", "GHSA-10466-aaaa-bbbb", "GHSA-10466-aaaa-bbbb"},
		{"  ", "GHSA-10466-aaaa-bbbb", "GHSA-10466-aaaa-bbbb"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := AdvisoryGroupID(tc.cve, tc.advisory); got != tc.want {
			t.Errorf("AdvisoryGroupID(%q, %q) = %q, want %q", tc.cve, tc.advisory, got, tc.want)
		}
	}
}
