// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// guardedDirectFamily is one #6228 direct-materialization family that has been
// taken past the "waived and unregistered" state.
type guardedDirectFamily struct {
	// Family is the materialized_edges:<family> surface suffix.
	Family string
	// OduName is the catalog name of the Odù this family's guard resolves.
	OduName string
	// EdgeTypes is what the family's WRITER can emit, hand-read off the Cypher
	// that writer executes rather than off the port name -- the #6181 rule.
	EdgeTypes []string
}

// guardedDirectMaterializedEdgeFamilies is the roster of #6228 direct families
// that now have a registered edge-type set, a named vacuity guard dispatched
// from MaterializedEdgeOduResolver, a cataloged Odù, and a hand-derived
// expected-edge-set fixture.
//
// It is written down rather than derived from the registry for the same reason
// materializedEdgeFamiliesUnderUmbrella is: deriving it would make every check
// below circular, so removing a family would shrink both sides and stay green.
//
// This roster is NOT the coverage ledger, and being on it does not entitle a
// family to a coverage row. It records three of the ledger's four conditions.
// The fourth -- the live ifa-determinism / ifa-fault-injection matrices
// actually driving the family -- is unmet for every entry here, which is why
// both of them still carry their waiver rows in
// specs/ifa-materialized-edge-coverage-direct.v1.yaml.
var guardedDirectMaterializedEdgeFamilies = []guardedDirectFamily{
	{
		Family:    kubernetesNamespaceEnvironmentFamily,
		OduName:   ifa.KubernetesNamespaceEnvironmentFamilyOduName,
		EdgeTypes: []string{"TARGETS_ENVIRONMENT"},
	},
	{
		Family:    iamInstanceProfileRoleFamily,
		OduName:   ifa.IAMInstanceProfileRoleFamilyOduName,
		EdgeTypes: []string{"HAS_ROLE"},
	},
	{
		Family:    workloadCloudRelationshipFamily,
		OduName:   ifa.WorkloadCloudRelationshipFamilyOduName,
		EdgeTypes: []string{"USES"},
	},
}

// guardedDirectFamilyCoverageEntry builds the coverage entry a ledger row for
// family would resolve through, so every check below exercises the production
// MaterializedEdgeOduResolver dispatch rather than calling a guard directly.
func guardedDirectFamilyCoverageEntry(family, oduName string) replaycoverage.CoverageEntry {
	return replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + family,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          oduName,
	}
}

// TestGuardedDirectFamiliesAreEnumeratedAsDirect fails closed on a roster entry
// that is not actually a direct-materialization family.
//
// A shared-projection family listed here would be reconciled against the OTHER
// half of the ledger, so every check below would be asserting something about
// the wrong enumeration while still reporting green.
func TestGuardedDirectFamiliesAreEnumeratedAsDirect(t *testing.T) {
	t.Parallel()

	direct := map[string]struct{}{}
	for _, family := range reducer.DirectMaterializedEdgeFamilies() {
		direct[family] = struct{}{}
	}
	if len(direct) == 0 {
		t.Fatal("reducer.DirectMaterializedEdgeFamilies() returned zero families; every check below would pass vacuously")
	}
	for _, entry := range guardedDirectMaterializedEdgeFamilies {
		if _, ok := direct[entry.Family]; !ok {
			t.Errorf("family %q is on the #6228 guarded roster but reducer.DirectMaterializedEdgeFamilies() does not enumerate it", entry.Family)
		}
	}
}

// TestGuardedDirectFamiliesResolveToTheirWrittenEdgeTypes is the registry half
// of the #6228 work: until a family resolves here, `eshu-ifa assert-edges
// -domain <family>` errors before any Odù or expected-edge set is consulted,
// so no amount of fixture work can ever green it.
//
// The expected sets are hand-read off each writer's executed Cypher, NOT off
// its port name and NOT off its statement-metadata label. Both roster families
// are cases where a name-derived set would be wrong: the port
// WriteKubernetesNamespaceNodes MERGEs a TARGETS_ENVIRONMENT relationship, and
// iam_instance_profile_role's statement-metadata label is
// "IAM_INSTANCE_PROFILE_HAS_ROLE" while the relationship type its template
// MERGEs is HAS_ROLE.
func TestGuardedDirectFamiliesResolveToTheirWrittenEdgeTypes(t *testing.T) {
	t.Parallel()

	for _, entry := range guardedDirectMaterializedEdgeFamilies {
		t.Run(entry.Family, func(t *testing.T) {
			t.Parallel()
			types, err := MaterializedEdgeDomainEdgeTypes(entry.Family)
			if err != nil {
				t.Fatalf("MaterializedEdgeDomainEdgeTypes(%q): %v", entry.Family, err)
			}
			got := make([]string, 0, len(types))
			for edgeTypeName := range types {
				got = append(got, edgeTypeName)
			}
			sort.Strings(got)
			want := append([]string(nil), entry.EdgeTypes...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("family %q registers %v, want %v (read off the writer's executed Cypher)", entry.Family, got, want)
			}
		})
	}
}

// TestGuardedDirectFamilyEdgeTypesAreCanonical is the third independent source
// behind each derivation, after the write template and the retract alternation.
//
// It catches what the other two cannot: a typo'd or invented relationship type
// that is internally consistent between a registry and its retract but names an
// edge the graph never carries. Such a type would make the live gate assert an
// always-empty population -- a guard that can never fail.
func TestGuardedDirectFamilyEdgeTypesAreCanonical(t *testing.T) {
	t.Parallel()

	canonical := map[string]struct{}{}
	for _, e := range edgetype.All() {
		canonical[string(e)] = struct{}{}
	}
	if len(canonical) == 0 {
		t.Fatal("edgetype.All() is empty; this test would pass vacuously")
	}
	for _, entry := range guardedDirectMaterializedEdgeFamilies {
		for _, edgeTypeName := range entry.EdgeTypes {
			if _, ok := canonical[edgeTypeName]; !ok {
				t.Errorf("family %q names %q, which the canonical edgetype registry does not define; the live gate would assert an always-empty population for it", entry.Family, edgeTypeName)
			}
		}
	}
}

// TestGuardedDirectFamiliesResolveTheirOduCovered runs each roster family's Odù
// through the production resolver -- the same MaterializedEdgeOduResolver a
// coverage row resolves through -- and requires a covered verdict.
//
// This is the check that makes the fixture load-bearing: it runs the real
// reducer extractor over the Odù's facts and compares the result against the
// hand-derived expected-edge set, so a writer or extractor change that stops
// producing the family's edges turns it red here rather than on a live gate
// acquisition.
func TestGuardedDirectFamiliesResolveTheirOduCovered(t *testing.T) {
	t.Parallel()

	catalog := ifa.CatalogByName()
	resolver := MaterializedEdgeOduResolver{Catalog: catalog, RepoRoot: repoRootDir(t)}
	for _, entry := range guardedDirectMaterializedEdgeFamilies {
		t.Run(entry.Family, func(t *testing.T) {
			t.Parallel()
			if _, ok := catalog[entry.OduName]; !ok {
				t.Fatalf("Odù %q is not in ifa.Catalog(); a coverage row naming it could never resolve", entry.OduName)
			}
			ok, detail := resolver.Resolve(guardedDirectFamilyCoverageEntry(entry.Family, entry.OduName))
			if !ok {
				t.Fatalf("resolver reported %q uncovered: %s", entry.Family, detail)
			}
			t.Logf("%s: %s", entry.Family, detail)
		})
	}
}

// TestGuardedDirectFamilyGuardsRejectAFactlessOdu proves each guard fails
// closed on an Odù carrying no facts rather than reporting a vacuous covered.
//
// A fact-less Odù produces zero extractor rows. A guard that compared zero rows
// against its expected set only by "no mismatches found" would report green
// while proving nothing, which is the false-green shape #5589 exists to
// prevent. The empty Odù is substituted into the catalog under the SAME name
// the real one uses, so the guard reaches its own body rather than failing on
// an unresolvable ref.
func TestGuardedDirectFamilyGuardsRejectAFactlessOdu(t *testing.T) {
	t.Parallel()

	for _, entry := range guardedDirectMaterializedEdgeFamilies {
		t.Run(entry.Family, func(t *testing.T) {
			t.Parallel()
			empty := ifa.Odu{Name: entry.OduName}
			resolver := MaterializedEdgeOduResolver{
				Catalog:  map[string]ifa.Odu{entry.OduName: empty},
				RepoRoot: repoRootDir(t),
			}
			ok, detail := resolver.Resolve(guardedDirectFamilyCoverageEntry(entry.Family, entry.OduName))
			if ok {
				t.Fatalf("guard for %q reported covered on a fact-less Odù: %s", entry.Family, detail)
			}
			t.Logf("%s fails closed: %s", entry.Family, detail)
		})
	}
}

// TestGuardedDirectFamiliesStillCarryTheirWaivers is the honesty check on this
// roster.
//
// Three of the ledger's four conditions are met for every family above, and the
// fourth is not: neither live matrix drives them. A coverage row asserts all
// four, so promoting one of these families to a coverage row while its live
// wiring is absent would claim a proof that does not exist -- the nominally
// covered failure #5589 and #6181 both exist to prevent. This test makes that
// promotion fail here, in a cheap unit run, rather than being noticed later.
//
// Retiring a waiver is therefore a two-sided edit: wire the family into the
// live matrices, then remove BOTH its waiver row and its entry here.
func TestGuardedDirectFamiliesStillCarryTheirWaivers(t *testing.T) {
	t.Parallel()

	manifest, waivers, err := LoadMaterializedEdgeLedger(filepath.Join(repoRootDir(t), "specs"))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeLedger: %v", err)
	}
	covered := map[string]struct{}{}
	for _, row := range manifest.Coverage {
		covered[strings.TrimPrefix(row.Surface, MaterializedEdgeSurfacePrefix)] = struct{}{}
	}
	waived := map[string]int{}
	for _, waiver := range waivers {
		waived[strings.TrimPrefix(waiver.Surface, MaterializedEdgeSurfacePrefix)]++
	}
	for _, entry := range guardedDirectMaterializedEdgeFamilies {
		if _, isCovered := covered[entry.Family]; isCovered {
			t.Errorf("family %q claims a coverage row while still on the guarded roster; a coverage row asserts the live matrices proved it, and no live matrix drives this family yet", entry.Family)
		}
		if got := waived[entry.Family]; got != 2 {
			t.Errorf("family %q carries %d waiver row(s), want 2 (one per live gate); its live proof is still outstanding, so both rows must stand", entry.Family, got)
		}
	}
}
