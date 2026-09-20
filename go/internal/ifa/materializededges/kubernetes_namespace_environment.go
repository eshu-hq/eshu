// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// kubernetesNamespaceEnvironmentFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
//
// It is named for the RELATIONSHIP the family materializes, not for its port.
// The port is WriteKubernetesNamespaceNodes; deriving the family name from it
// would produce "kubernetes_namespace_nodes", an edge family named for nodes,
// and every reader of the ledger would inherit the same wrong belief that
// produced the name-derived enumeration #6181 replaced.
const kubernetesNamespaceEnvironmentFamily = "kubernetes_namespace_environment"

// kubernetesNamespaceEnvironmentRelationshipType is the single relationship
// type canonicalKubernetesNamespaceWithEnvironmentUpsertCypher MERGEs.
//
// Duplicated here as a plain literal rather than imported: the cypher package's
// own copy is inside an unexported Cypher const, and the registry lookup below
// is what actually binds the two. A stale copy here fails CLOSED --
// missingDirectFamilyExpectedTypes compares the fixture against the REGISTRY,
// so a wrong literal here produces a fixture whose types the registry does not
// contain and the guard reds rather than silently narrowing what it asserts.
const kubernetesNamespaceEnvironmentRelationshipType = "TARGETS_ENVIRONMENT"

// kubernetesNamespaceEnvironmentExpectedEdgesRelPath is the family's
// hand-derived expected-edge fixture, repoRoot-anchored.
const kubernetesNamespaceEnvironmentExpectedEdgesRelPath = "go/internal/ifa/testdata/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family-expected-edges.json"

// kubernetesNamespaceEnvironmentExpectedEdgesPath joins repoRoot onto the
// family's expected-edge fixture.
func kubernetesNamespaceEnvironmentExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, kubernetesNamespaceEnvironmentExpectedEdgesRelPath)
}

// resolveKubernetesNamespaceEnvironmentMaterializedEdges is
// kubernetes_namespace_environment's named vacuity guard (#6228).
//
// It follows the same three steps every family guard runs -- load the fixture,
// require it to cover the registry's types, run the production extractor over
// the Odù's facts and compare exactly -- with one difference that matters for a
// DIRECT family: the extractor's output is not the edge set.
//
// ExtractKubernetesNamespaceNodeRows emits a node row for EVERY namespace it
// decodes, bound or not. Only a row carrying a non-empty environment is routed
// to canonicalKubernetesNamespaceWithEnvironmentUpsertCypher, the one template
// that MERGEs the relationship; the sibling template writes the node and
// explicitly no edge. So kubernetesNamespaceRowsToExpectedEdges applies the
// writer's own routing predicate, and the fixture's two deliberately-unbound
// namespaces are the proof that it does: if the predicate were dropped, they
// would arrive as EXTRA edges to an Environment node the graph must never
// gain.
//
// A quarantined fact is fatal rather than skipped. A fixture that no longer
// decodes cleanly against the kubernetes_live.namespace contract cannot
// honestly claim to prove the edge set it names, and proceeding on the
// survivors would understate the fixture's own claim rather than catch the
// regression.
func resolveKubernetesNamespaceEnvironmentMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, kubernetesNamespaceEnvironmentFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, boundCount, quarantined, err := reducer.ExtractKubernetesNamespaceNodeRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractKubernetesNamespaceNodeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the kubernetes_live.namespace contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractKubernetesNamespaceNodeRows produced zero namespace rows; this fixture cannot prove anything", odu.Name)
	}
	if boundCount == 0 {
		return false, fmt.Sprintf("odù %q: %d namespace row(s) but none carries an environment binding, so the writer would MERGE no TARGETS_ENVIRONMENT edge at all", odu.Name, len(rows))
	}

	actual := kubernetesNamespaceRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, kubernetesNamespaceEnvironmentFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractKubernetesNamespaceNodeRows reproduces the expected %d-edge TARGETS_ENVIRONMENT set exactly across all %d registry type(s), from %d namespace row(s) of which %d bound; the %d unbound namespace(s) produced no edge",
		odu.Name, len(expected), len(registry), len(rows), boundCount, len(rows)-boundCount,
	)
}

// kubernetesNamespaceRowsToExpectedEdges applies the writer's own row-routing
// predicate to ExtractKubernetesNamespaceNodeRows' output and renders the
// surviving rows as the edges the write template would MERGE.
//
// The predicate is a non-empty row["environment"], exactly what
// KubernetesNamespaceNodeWriter.WriteKubernetesNamespaceNodes branches on.
// Reproducing it here rather than assuming every row is an edge is what makes
// the unbound half of the fixture load-bearing.
//
// target_entity_id is the Environment node's NAME, not a uid: the template
// MERGEs `(env:Environment {name: row.environment})`, so name is this label's
// canonical identity, and row["environment"] is passed through VERBATIM.
//
// Re-canonicalizing it here would be a false green, and this is not
// hypothetical -- an earlier version of this function called
// environment.Canonical on the row value, and a mutation that made
// namespaceEnvironmentFromLabels return the raw normalized token instead of
// the canonical one (leaving "production" where the graph would carry "prod")
// left the guard GREEN. The guard would have been repairing, inside itself,
// the exact drift it exists to report. Whatever the extractor puts on the row
// is what the writer MERGEs, so that is what gets compared.
//
// generation_id, cluster_id and the source_* provenance columns are
// deliberately not asserted: they are node properties this template SETs on the
// namespace, not relationship properties, and generation_id is run-specific.
// evidence_class IS asserted, because the template SETs it on the relationship
// itself and a blank one would mean the binding lost its evidence class while
// keeping its identity.
//
// evidence_source is likewise SET on the relationship (not the node) by the
// same template, from the writer's evidenceSource argument -- a compile-time
// constant (reducer.KubernetesNamespaceEvidenceSource), so it is knowable
// offline and is stamped here unconditionally: the stale-binding retraction
// matches on it, and the live `assert-edges` half asserts it from the fixture,
// so both halves now prove the same key.
func kubernetesNamespaceRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		environmentName := anyToStringValue(row["environment"])
		if environmentName == "" {
			continue
		}
		edge := ExpectedEdge{
			RelationshipType: kubernetesNamespaceEnvironmentRelationshipType,
			SourceEntityID:   anyToStringValue(row["uid"]),
			TargetEntityID:   environmentName,
			Properties: map[string]string{
				"evidence_source": reducer.KubernetesNamespaceEvidenceSource,
			},
		}
		if evidenceClass := anyToStringValue(row["evidence_class"]); evidenceClass != "" {
			edge.Properties["evidence_class"] = evidenceClass
		}
		edges = append(edges, edge)
	}
	return edges
}
