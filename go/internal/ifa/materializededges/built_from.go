// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
)

// builtFromFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const builtFromFamily = "built_from"

// builtFromRelationshipType is the single relationship type the canonical
// provenance upsert template MERGEs: the only type this writer can emit
// (fixed templates, no vocabulary substitution).
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const builtFromRelationshipType = "BUILT_FROM"

// builtFromExpectedEdgesRelPath is the family's hand-derived expected-edge
// fixture, repoRoot-anchored.
const builtFromExpectedEdgesRelPath = "go/internal/ifa/testdata/builtfrom/ifa-built-from-family-expected-edges.json"

// builtFromExpectedEdgesPath joins repoRoot onto the family's expected-edge
// fixture.
func builtFromExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, builtFromExpectedEdgesRelPath)
}

// resolveBuiltFromMaterializedEdges is built_from's named vacuity guard
// (#6228).
//
// The pipeline is two production stages, both exercised: the Odù's facts
// classify into container-image-identity decisions
// (BuildContainerImageIdentityDecisionsWithQuarantine, scope-free like every
// existing caller — bare-digest SLSA synthesis stays out of this fixture by
// construction since it carries no SLSA facts), and the exact_digest
// decisions with build-provenance repositories project to BUILT_FROM rows
// (ContainerImageBuiltFromRows) one-for-one with the write template's
// UNWIND. What the fixture proves is the attribution behaviour: two digests
// whose OCI configs declare the fixture repository fan out to one edge per
// digest — while an exact digest with no build evidence, a digest whose
// source label names an unscanned repository, and a digest the repository
// merely deploys (runtime reference, never build evidence per #5796) each
// resolve to nothing rather than to a fabricated edge.
//
// scope_id and evidence_source are MERGE identity the writer stamps per
// run, so the guard reproduces the live contract rather than reading it
// off the rows: scope_id is the Odù's own single scope (the writing
// intent's scope — the guard fails closed when the facts span scopes, so
// no second scope can silently change the stamped identity), and
// evidence_source is the container-image-identity domain constant. The
// fixture pins both; the live cell must drive the writing intent in that
// scope with that evidence source.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against the OCI /
// repository contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveBuiltFromMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, builtFromFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	decisions, quarantined, err := containerimage.BuildContainerImageIdentityDecisionsWithQuarantine(odu.Facts, "")
	if err != nil {
		return false, fmt.Sprintf("odù %q: BuildContainerImageIdentityDecisionsWithQuarantine failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the OCI / repository contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	rows := containerimage.ContainerImageBuiltFromRows(decisions)
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ContainerImageBuiltFromRows produced zero BUILT_FROM rows across %d decisions; this fixture cannot prove anything", odu.Name, len(decisions))
	}
	scopeID, problem := directFamilySingleScope(odu, builtFromFamily)
	if problem != "" {
		return false, problem
	}

	actual, problem := builtFromRowsToExpectedEdges(rows, scopeID, odu)
	if problem != "" {
		return false, problem
	}
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, builtFromFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ContainerImageBuiltFromRows reproduces the expected %d-edge BUILT_FROM set exactly across all %d registry type(s), %d decision(s) and %d fact envelope(s); the evidence-less, ghost-builder and deploy-only digests produced no spurious rows",
		odu.Name, len(expected), len(registry), len(decisions), len(odu.Facts),
	)
}

// directFamilySingleScope returns the one scope an Odù's facts share: the
// writing intent's scope the writer stamps as scope_id. It fails closed
// when the facts span scopes, so no second scope can silently change the
// stamped identity the fixture pins.
func directFamilySingleScope(odu ifa.Odu, family string) (string, string) {
	scopes := make(map[string]struct{}, 2)
	for _, env := range odu.Facts {
		scopes[env.ScopeID] = struct{}{}
	}
	if len(scopes) != 1 {
		got := make([]string, 0, len(scopes))
		for scope := range scopes {
			got = append(got, scope)
		}
		sort.Strings(got)
		return "", fmt.Sprintf("odù %q: %s facts span %d scopes %q; the writer stamps one scope_id per intent, so a multi-scope Odù cannot pin the MERGE identity", odu.Name, family, len(scopes), got)
	}
	for scope := range scopes {
		return scope, ""
	}
	return "", fmt.Sprintf("odù %q: carries no facts", odu.Name)
}

// builtFromRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// repository_id is the :Repository id the template's second MATCH clause
// binds, so it is the edge's target identity. The source is NOT the row's
// bare digest: the template matches the :ContainerImage node by its digest
// property, but the edge endpoint is the node's id, which the OCI canonical
// writer stamps as the resolved descriptor uid
// (oci-descriptor://registry/repository@digest, same construction as
// ociDescriptorUID in projector/canonical/oci_registry.go). The resolver
// reproduces that construction from the Odù's own image-manifest facts
// rather than trusting the row, and fails closed when a row's digest has
// no (or no unambiguous oci-registry://) manifest. scope_id and
// evidence_source are MERGE identity the writer stamps per run: the guard
// reproduces the live contract (the Odù's single scope, the
// container-image-identity domain), and the fixture pins the same values.
// generation_id, evidence_kinds and source_tool are SET after the MERGE
// from per-run writer arguments and carried on no row, so they are outside
// what this guard can or should assert. The live assert reads only
// fixture-declared keys off each rel, so extra live props never collide
// with these pins.
func builtFromRowsToExpectedEdges(rows []map[string]any, scopeID string, odu ifa.Odu) ([]ExpectedEdge, string) {
	manifestRegistry := make(map[string]string, len(odu.Facts))
	for _, env := range odu.Facts {
		if env.FactKind != chain.OCIImageManifestFactKind {
			continue
		}
		payload := env.Payload
		if payload == nil {
			continue
		}
		digest := anyToStringValue(payload["digest"])
		registryID := anyToStringValue(payload["repository_id"])
		if digest == "" || registryID == "" {
			continue
		}
		if prev, dup := manifestRegistry[digest]; dup && prev != registryID {
			return nil, fmt.Sprintf("odù %q: digest %q maps to two manifest repository_ids %q and %q; the descriptor uid is ambiguous so the guard cannot pin the edge source", odu.Name, digest, prev, registryID)
		}
		manifestRegistry[digest] = registryID
	}
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		digest := anyToStringValue(row["digest"])
		registryID, ok := manifestRegistry[digest]
		if !ok || registryID == "" {
			return nil, fmt.Sprintf("odù %q: row digest %q has no image-manifest fact; the descriptor uid needs the manifest's oci-registry:// repository_id", odu.Name, digest)
		}
		if !strings.HasPrefix(registryID, "oci-registry://") {
			return nil, fmt.Sprintf("odù %q: manifest repository_id %q lacks the oci-registry:// prefix; the descriptor uid construction does not apply", odu.Name, registryID)
		}
		edge := ExpectedEdge{
			RelationshipType: builtFromRelationshipType,
			SourceEntityID:   "oci-descriptor://" + strings.TrimPrefix(registryID, "oci-registry://") + "@" + digest,
			TargetEntityID:   anyToStringValue(row["repository_id"]),
			Identity: map[string]string{
				"scope_id":        scopeID,
				"evidence_source": containerimage.ContainerImageBuiltFromProvenanceEvidenceSource,
			},
		}
		edges = append(edges, edge)
	}
	return edges, ""
}
