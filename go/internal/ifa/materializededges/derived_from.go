// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// derivedFromFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const derivedFromFamily = "derived_from"

// derivedFromRelationshipType is the single relationship type the canonical
// provenance upsert template MERGEs: the only type this writer can emit
// (fixed template, no vocabulary substitution).
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const derivedFromRelationshipType = "DERIVED_FROM"

// derivedFromAttributionBasis is the only attribution basis the row builder
// stamps today: Dockerfile-only inference from a repository that resolves
// to exactly one distinct exact-digest runtime base. Pinning the single
// basis catches a future second basis (CI, SLSA per-image links) silently
// changing what the rows claim: a row carrying a new basis reds here until
// the fixture learns to cover it.
const derivedFromAttributionBasis = "repository_single_base"

// derivedFromExpectedEdgesRelPath is the family's hand-derived expected-edge
// fixture, repoRoot-anchored.
const derivedFromExpectedEdgesRelPath = "go/internal/ifa/testdata/derivedfrom/ifa-derived-from-family-expected-edges.json"

// derivedFromExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func derivedFromExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, derivedFromExpectedEdgesRelPath)
}

// resolveDerivedFromMaterializedEdges is derived_from's named vacuity guard
// (#6228).
//
// The pipeline is two production stages, both exercised: the Odù's facts
// classify into container-image-identity decisions
// (BuildContainerImageIdentityDecisionsWithQuarantine, scope-free like every
// existing caller), and the owning repository's exact_digest children of
// its single Dockerfile-declared base project to DERIVED_FROM rows
// (ContainerImageDerivedFromRows). What the fixture proves is the
// conservative attribution behaviour: two children built by the owning
// repository from one base fan out to one edge per child — while the base
// itself (an image is never its own ancestor), a digest the repository
// merely deploys, and any second-base ambiguity each resolve to nothing
// rather than to fabricated CVE lineage. The owning repository is derived
// from the Odù's single repository fact, never hard-coded beside the rows,
// so a fixture that stopped anchoring its repo reds instead of attributing
// to a stale owner.
//
// scope_id and evidence_source are MERGE identity the writer stamps per
// run, so the guard reproduces the live contract rather than reading it
// off the rows: scope_id is the Odù's own single scope (the writing
// intent's scope — the guard fails closed when the facts span scopes), and
// evidence_source is the container-image-base-image domain constant. The
// fixture pins both; the live cell must drive the writing intent in that
// scope with that evidence source.
//
// A quarantined fact is fatal rather than skipped, for the same reason as
// every sibling guard: a fixture that stopped decoding against the OCI /
// Dockerfile contract cannot prove the set it names, and the surviving
// facts would understate its own claim.
func resolveDerivedFromMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, derivedFromFamily, odu.Name,
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
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the OCI / Dockerfile contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	owner, problem := directFamilyOwningRepository(odu, derivedFromFamily)
	if problem != "" {
		return false, problem
	}
	rows := containerimage.ContainerImageDerivedFromRows(decisions, owner)
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ContainerImageDerivedFromRows produced zero DERIVED_FROM rows across %d decisions for owner %q; this fixture cannot prove anything", odu.Name, len(decisions), owner)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["attribution_basis"]); got != derivedFromAttributionBasis {
			return false, fmt.Sprintf("odù %q: row %d carries attribution_basis %q, want %q (the only basis this writer's contract admits today)", odu.Name, index, got, derivedFromAttributionBasis)
		}
	}
	scopeID, problem := directFamilySingleScope(odu, derivedFromFamily)
	if problem != "" {
		return false, problem
	}

	actual := derivedFromRowsToExpectedEdges(rows, scopeID)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, derivedFromFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ContainerImageDerivedFromRows reproduces the expected %d-edge DERIVED_FROM set exactly across all %d registry type(s), %d decision(s) and %d fact envelope(s) for owner %q; the self-loop, deploy-only and ambiguous bases produced no spurious rows",
		odu.Name, len(expected), len(registry), len(decisions), len(odu.Facts), owner,
	)
}

// directFamilyOwningRepository returns the owning repository id for a
// family whose rows attribute children to the repository that built them:
// the graph id of the Odù's single repository fact. It fails closed when
// the Odù carries zero or several repository facts, so a fixture that
// stopped anchoring its repo cannot silently attribute to a stale owner.
func directFamilyOwningRepository(odu ifa.Odu, family string) (string, string) {
	var owners []string
	for _, env := range odu.Facts {
		if env.FactKind != factload.FactKindRepository || env.IsTombstone {
			continue
		}
		owner := payloadcore.FirstNonBlank(
			payloadcore.PayloadStr(env.Payload, "graph_id"),
			payloadcore.PayloadStr(env.Payload, "repo_id"),
			payloadcore.PayloadStr(env.Payload, "repository_id"),
		)
		if owner == "" {
			return "", fmt.Sprintf("odù %q: %s repository fact carries no graph_id, repo_id or repository_id; the owning repository cannot be derived", odu.Name, family)
		}
		owners = append(owners, owner)
	}
	if len(owners) != 1 {
		return "", fmt.Sprintf("odù %q: %s carries %d repository facts, want exactly the one owning repository the rows attribute to", odu.Name, family, len(owners))
	}
	return owners[0], ""
}

// derivedFromRowsToExpectedEdges converts the extractor's rows one-for-one
// into the edge identity the write template MERGEs.
//
// digest and base_digest are the built :ContainerImage digest and the base
// :ContainerImage digest the template's two MATCH clauses bind, so they are
// the edge's source and target identity. scope_id and evidence_source are
// MERGE identity the writer stamps per run: the guard reproduces the live
// contract (the Odù's single scope, the container-image-base-image domain),
// and the fixture pins the same values. attribution_basis is SET after the
// MERGE — deliberately outside the relationship key — and is asserted here
// as a mutable pin so a new attribution model cannot pass on edge identity
// alone. generation_id, evidence_kinds and source_tool are SET after the
// MERGE from per-run writer arguments and carried on no row, so they are
// outside what this guard can or should assert. The live assert reads only
// fixture-declared keys off each rel, so extra live props never collide
// with these pins.
func derivedFromRowsToExpectedEdges(rows []map[string]any, scopeID string) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: derivedFromRelationshipType,
			SourceEntityID:   anyToStringValue(row["digest"]),
			TargetEntityID:   anyToStringValue(row["base_digest"]),
			Identity: map[string]string{
				"scope_id":        scopeID,
				"evidence_source": containerimage.ContainerImageDerivedFromProvenanceEvidenceSource,
			},
			Properties: map[string]string{
				"attribution_basis": anyToStringValue(row["attribution_basis"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
