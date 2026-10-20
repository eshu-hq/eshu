// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// publishesFamily is the materialized-edge family key this guard asserts
// (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const publishesFamily = "publishes"

// publishesRelationshipType is the single relationship type both canonical
// provenance upsert templates MERGE: the only type this writer can emit
// (fixed templates, no vocabulary substitution).
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const publishesRelationshipType = "PUBLISHES"

// publishesExpectedEdgesRelPath is the family's hand-derived expected-edge
// fixture, repoRoot-anchored.
const publishesExpectedEdgesRelPath = "go/internal/ifa/testdata/publishes/ifa-publishes-family-expected-edges.json"

// publishesExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func publishesExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, publishesExpectedEdgesRelPath)
}

// resolvePublishesMaterializedEdges is publishes' named vacuity guard
// (#6228).
//
// The pipeline is three production stages, all exercised: the Odù's facts
// classify into package-ownership decisions (BuildPackageSourceDecisions)
// and package-publication decisions (BuildPackagePublicationDecisions),
// and the exact/derived decisions of each project to PUBLISHES rows
// (PackageOwnershipPublishesRows, PackagePublicationPublishesRows)
// one-for-one with the write template's UNWIND. What the fixture proves is
// the correlation behaviour on both paths at once: a package-level exact
// hint owns the Package node and publishes its paired version, a
// version-scoped exact hint owns that version without ever reaching the
// publication path — while a tombstoned-only match (stale), a
// provenance-only homepage kind (rejected), an unmatched remote
// (unresolved) and a two-remote match (ambiguous, never a picked
// representative) each resolve to nothing rather than to a fabricated
// edge. The same endpoint pair under both evidence sources is two edges,
// not one: the handler writes the two decision sets through separate
// writer calls stamping their own evidence_source, and the MERGE identity
// includes it.
//
// scope_id and evidence_source are MERGE identity the writer stamps per
// run, so the guard reproduces the live contract rather than reading it
// off the rows: scope_id is the Odù's own single scope (the writing
// intent's scope — the guard fails closed when the facts span scopes), and
// evidence_source is the decision path's domain constant (ownership rows
// carry the ownership source, publication rows the publication source).
// The fixture pins both; the live cell must drive the writing intent in
// that scope with those evidence sources.
//
// Both builders are total over their inputs — a malformed hint classifies
// as rejected or unresolved rather than quarantining — so there is no
// quarantine check to fail closed on here: the exact-set comparison is
// what voids a fixture that stopped decoding.
func resolvePublishesMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, publishesFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	ownershipRows := correlation.PackageOwnershipPublishesRows(
		correlation.BuildPackageSourceDecisions(odu.Facts),
	)
	publicationRows := correlation.PackagePublicationPublishesRows(
		correlation.BuildPackagePublicationDecisions(odu.Facts),
	)
	if len(ownershipRows)+len(publicationRows) == 0 {
		return false, fmt.Sprintf("odù %q: both publishes row builders produced zero PUBLISHES rows; this fixture cannot prove anything", odu.Name)
	}
	scopeID, problem := directFamilySingleScope(odu, publishesFamily)
	if problem != "" {
		return false, problem
	}

	actual := publishesRowsToExpectedEdges(ownershipRows, publicationRows, scopeID)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, publishesFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: the ownership and publication row builders reproduce the expected %d-edge PUBLISHES set exactly across all %d registry type(s) and %d fact envelope(s) (%d ownership plus %d publication rows); the stale, rejected, unresolved and ambiguous hints produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts), len(ownershipRows), len(publicationRows),
	)
}

// publishesRowsToExpectedEdges converts both builders' rows one-for-one
// into the edge identity the write templates MERGE.
//
// repository_id is the :Repository id the templates' MATCH binds, and the
// target is the :Package uid (package_id rows) or the :PackageVersion uid
// (version_id rows) — the same bucketing the writer applies when it picks
// the Package or PackageVersion template, so a row mapped to the wrong
// target here describes an edge the writer never creates. scope_id and
// evidence_source are MERGE identity the writer stamps per run: the guard
// reproduces the live contract (the Odù's single scope; the ownership or
// publication domain per row set), and the fixture pins the same values.
// generation_id, evidence_kinds and source_tool are SET after the MERGE
// from per-run writer arguments and carried on no row, so they are outside
// what this guard can or should assert. The live assert reads only
// fixture-declared keys off each rel, so extra live props never collide
// with these pins.
func publishesRowsToExpectedEdges(ownershipRows, publicationRows []map[string]any, scopeID string) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(ownershipRows)+len(publicationRows))
	appendPublishesRows := func(rows []map[string]any, evidenceSource string) {
		for _, row := range rows {
			target := anyToStringValue(row["package_id"])
			if versionID := anyToStringValue(row["version_id"]); versionID != "" {
				target = versionID
			}
			edge := ExpectedEdge{
				RelationshipType: publishesRelationshipType,
				SourceEntityID:   anyToStringValue(row["repository_id"]),
				TargetEntityID:   target,
				Identity: map[string]string{
					"scope_id":        scopeID,
					"evidence_source": evidenceSource,
				},
			}
			edges = append(edges, edge)
		}
	}
	appendPublishesRows(ownershipRows, correlation.PackageOwnershipProvenanceEvidenceSource)
	appendPublishesRows(publicationRows, correlation.PackagePublicationProvenanceEvidenceSource)
	return edges
}
