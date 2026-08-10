// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"strings"
	"testing"
)

// TestRepoDependencyRegistryMatchesTheLiveRetractRoles pins the family's seven
// types against what the LIVE retract path deletes (#5999).
//
// The live path is buildRepoDependencySplitRetractStatements, which splits into
// roles: role 1 deletes the six-type repo-to-repo alternation, role 2 deletes
// RUNS_ON through DEFINES/INSTANCE_OF. Both roles are read out of the built
// statements here rather than from a literal, so the guard tracks the production
// dispatch instead of a copy of it.
//
// retractRepoDependencyEdgesCypher is NOT the thing to pin against. It names
// DEPENDS_ON alone and reads authoritatively, but its only user
// (BuildRetractRepoDependencyEdges) has no production caller. Pinning a registry
// against dead code is a guard that can never fail for the right reason, and it
// would have scored this family at one type instead of seven.
func TestRepoDependencyRegistryMatchesTheLiveRetractRoles(t *testing.T) {
	t.Parallel()

	stmts := buildRepoDependencySplitRetractStatements([]string{"repo-1"}, "resolver/cross-repo")
	if len(stmts) == 0 {
		t.Fatal("live repo_dependency retract built no statements; nothing would be reaped")
	}
	retracted := map[string]struct{}{}
	for _, stmt := range stmts {
		for _, m := range relTypeAlternation.FindAllStringSubmatch(stmt.stmt.Cypher, -1) {
			for _, relType := range strings.Split(m[1], "|") {
				if relType = strings.TrimSpace(relType); relType != "" {
					retracted[relType] = struct{}{}
				}
			}
		}
	}
	// DEFINES and INSTANCE_OF are traversal hops in the RUNS_ON role, not edges
	// this family materializes; workload materialization writes them.
	delete(retracted, "DEFINES")
	delete(retracted, "INSTANCE_OF")

	// The retract legitimately reaps MORE than the registry asserts. Role 3
	// DETACH DELETEs the evidence artifacts, whose endpoint uid embeds the
	// generation id and ordinal and so cannot appear in an exact hand-derived
	// edge set. That exclusion is named here rather than papered over by
	// loosening the check: any OTHER retract-only type still fails, which is what
	// catches a genuinely forgotten edge type.
	documentedRetractOnly := map[string]struct{}{
		"HAS_DEPLOYMENT_EVIDENCE":           {},
		"EVIDENCES_REPOSITORY_RELATIONSHIP": {},
	}

	registry := RepoDependencyMaterializedEdgeTypes()
	var missingFromRegistry, missingFromRetract []string
	for relType := range retracted {
		if _, ok := documentedRetractOnly[relType]; ok {
			continue
		}
		if _, ok := registry[relType]; !ok {
			missingFromRegistry = append(missingFromRegistry, relType)
		}
	}
	for relType := range registry {
		if _, ok := retracted[relType]; !ok {
			missingFromRetract = append(missingFromRetract, relType)
		}
	}
	sort.Strings(missingFromRegistry)
	sort.Strings(missingFromRetract)

	if len(missingFromRegistry) > 0 {
		t.Errorf("the live retract deletes %v but the registry omits them; the Ifá baseline gate would ignore those edges while calling repo_dependency exhaustive",
			missingFromRegistry)
	}
	if len(missingFromRetract) > 0 {
		t.Errorf("registry names %v but the live retract does not delete them; those edges would survive every reprojection as stale truth",
			missingFromRetract)
	}
}

// TestRepoDependencyRegistryDerivesTheAlternationRatherThanRelistingIt proves the
// six repo-to-repo types come out of the production const by splitting, not from
// a second hand-maintained list.
//
// Adding a seventh alternative to repoDependencyRelationshipEdgeTypes must widen
// the registry with no edit here; a re-listed copy would leave the new type
// unasserted by the live gate while every test stayed green.
func TestRepoDependencyRegistryDerivesTheAlternationRatherThanRelistingIt(t *testing.T) {
	t.Parallel()

	registry := RepoDependencyMaterializedEdgeTypes()
	for _, edgeType := range strings.Split(repoDependencyRelationshipEdgeTypes, "|") {
		edgeType = strings.TrimSpace(edgeType)
		if edgeType == "" {
			continue
		}
		if _, ok := registry[edgeType]; !ok {
			t.Errorf("repoDependencyRelationshipEdgeTypes names %q but the registry omits it; the set is not derived from the alternation", edgeType)
		}
	}
	if _, ok := registry["RUNS_ON"]; !ok {
		t.Error("registry omits RUNS_ON; it is reaped by a separate retract role and is invisible in the repo-to-repo alternation")
	}
	if len(registry) != len(strings.Split(repoDependencyRelationshipEdgeTypes, "|"))+1 {
		t.Errorf("registry has %d types, want the alternation plus RUNS_ON; an extra type claims edges this family does not own", len(registry))
	}
}

// TestRepoDependencyRegistryExcludesCrossDomainAndUnpinnableTypes documents the
// deliberate exclusions as assertions rather than prose.
//
// All three are excluded on ONE ground: their source is an EvidenceArtifact
// MERGEd with a generation-embedded id, so the endpoint identity changes on
// every reprojection and no exact hand-derived set can pin it.
//
// TARGETS_ENVIRONMENT is also written by kubernetes_namespace_node_writer.go,
// but that is no longer why it is excluded: materialized_edge_endpoints.go can
// now separate the two writers by endpoint label. Stating the real ground here
// stops a maintainer lifting the exclusion on the strength of the endpoint table
// and discovering the identity problem at the cost of a live-gate acquisition.
//
// All three are written under this domain, so a future "just enumerate the write
// path" sweep would pull them back in — this test makes that a failure rather
// than a silent change.
func TestRepoDependencyRegistryExcludesCrossDomainAndUnpinnableTypes(t *testing.T) {
	t.Parallel()

	registry := RepoDependencyMaterializedEdgeTypes()
	for _, excluded := range []string{"TARGETS_ENVIRONMENT", "HAS_DEPLOYMENT_EVIDENCE", "EVIDENCES_REPOSITORY_RELATIONSHIP"} {
		if _, ok := registry[excluded]; ok {
			t.Errorf("registry claims %q; it is deliberately excluded (cross-domain writer, or a generation-embedded endpoint uid no exact set can pin)", excluded)
		}
	}
}
