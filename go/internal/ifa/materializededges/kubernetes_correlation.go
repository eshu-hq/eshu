// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
)

// kubernetesCorrelationFamily is the materialized-edge family key this
// guard asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const kubernetesCorrelationFamily = "kubernetes_correlation"

// kubernetesCorrelationRelationshipType is the single relationship type the
// canonical Kubernetes correlation upsert template MERGEs: a static token
// in the template, not a per-row substitution — no row content can change
// the type this writer emits. The source label is NOT pinned here: it
// varies per row (OciImageManifest vs OciImageIndex) and rides the
// template's %s substitution, so a single literal cannot name it — the
// live drive exercises both labels instead.
//
// Read it off the template and the kubernetesRunsImageRelType const, never
// by deriving from the port or family name — "KUBERNETES_CORRELATION"
// appears nowhere in code: taking the type from the port or family name is
// the same name-derived mistake one level above, and it would make this
// guard assert an always-empty population.
//
// Duplicated here as a plain literal rather than imported, and it fails CLOSED:
// missingDirectFamilyExpectedTypes compares the FIXTURE against the REGISTRY,
// so a stale literal here yields a fixture whose type the registry does not
// contain and the guard reds instead of quietly asserting less.
const kubernetesCorrelationRelationshipType = "RUNS_IMAGE"

// kubernetesCorrelationResolutionMode is the writer's only resolution mode,
// stamped on every row. Digest-form live refs resolve against the source
// digest index (exact), tag-form refs against the tag index (derived);
// only exact digest outcomes promote to edges, so every row this guard can
// see carries this single mode: pinning it catches a future second mode
// silently changing what the rows claim.
const kubernetesCorrelationResolutionMode = "digest"

// kubernetesCorrelationExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const kubernetesCorrelationExpectedEdgesRelPath = "go/internal/ifa/testdata/kubernetescorrelation/ifa-kubernetes-correlation-family-expected-edges.json"

// kubernetesCorrelationExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func kubernetesCorrelationExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, kubernetesCorrelationExpectedEdgesRelPath)
}

// resolveKubernetesCorrelationMaterializedEdges is kubernetes_correlation's
// named vacuity guard (#6228).
//
// Like iam_escalation, this family's extractor already emits exactly the
// rows the write template UNWINDs, so the rows-to-edges mapping is
// one-for-one with no routing predicate to reproduce. What the fixture
// proves instead is the classifier's resolution behaviour: two workloads
// declare digest-form refs matching active deployment sources (one
// manifest, one index — proving both digest-addressed source labels the
// template MATCHes) while a workload naming a tombstone-only digest stays
// stale, a workload naming a tag two digests share stays ambiguous, and a
// workload naming an unobserved digest stays unresolved — each resolving
// to nothing rather than to a fabricated edge. The resolution mode is
// pinned on every row (single-mode contract), and the skip taxonomy itself
// stays covered by the reducer's own unit tests: the guard asserts the
// exact two-edge set, so a ghost edge or a dropped producer reds here
// either way.
//
// The Odù's facts pass through to the extractor unpartitioned: the
// extraction entry takes the whole envelope slice and dispatches on fact
// kind internally (pod templates, relationships, warnings, manifests,
// indexes, tag observations), so a guard-side partition would be a second
// copy of that dispatch to keep in step rather than the two-case kind
// split the AWS families reproduce. Tombstoned envelopes pass through on
// both sides alike: the tombstone filter lives inside the extraction, not
// in the split — the legacy manifest's tombstone is load-bearing fixture
// content, and dropping it here would turn the stale restraint into an
// unresolved one without reddening anything.
//
// A quarantined fact is fatal rather than skipped, for the same reason as every
// sibling guard: a fixture that stopped decoding against the pod template
// or OCI contract cannot prove the set it names, and the surviving facts
// would understate its own claim.
func resolveKubernetesCorrelationMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, kubernetesCorrelationFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, _, quarantined, err := kubernetescorrelation.ExtractKubernetesCorrelationEdgeRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractKubernetesCorrelationEdgeRows failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the pod template / OCI contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractKubernetesCorrelationEdgeRows produced zero RUNS_IMAGE rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["resolution_mode"]); got != kubernetesCorrelationResolutionMode {
			return false, fmt.Sprintf("odù %q: row %d carries resolution_mode %q, want %q (the writer's single-mode contract)", odu.Name, index, got, kubernetesCorrelationResolutionMode)
		}
	}

	actual := kubernetesCorrelationRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, kubernetesCorrelationFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractKubernetesCorrelationEdgeRows reproduces the expected %d-edge RUNS_IMAGE set exactly across all %d registry type(s) and %d fact envelope(s); the stale, ambiguous, and unresolved workloads produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// kubernetesCorrelationRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// workload_uid is the :KubernetesWorkload uid and source_uid the OCI node
// uid, matched by the template's two MATCH clauses, so they are the edge's
// source and target identity. resolution_mode, image_ref, and
// source_digest ride along as Properties pins: the template SETs them as
// plain strings from the row (unlike scope_id/generation_id/
// evidence_source, which the WRITER stamps from its own per-run intent
// arguments rather than carrying on the extractor's rows — no static set
// can pin per-run values, and their stamping is covered by the writer unit
// test asserting the annotated row contents instead). The live assert reads
// only fixture-declared property keys off each rel, so extra live props
// never collide with these pins.
func kubernetesCorrelationRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: kubernetesCorrelationRelationshipType,
			SourceEntityID:   anyToStringValue(row["workload_uid"]),
			TargetEntityID:   anyToStringValue(row["source_uid"]),
			Properties: map[string]string{
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
				"image_ref":       anyToStringValue(row["image_ref"]),
				"source_digest":   anyToStringValue(row["source_digest"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
