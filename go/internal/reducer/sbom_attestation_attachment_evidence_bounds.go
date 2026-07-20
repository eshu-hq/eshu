// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

const (
	// maxSBOMAttachmentDependencyRelationshipRows bounds the number of
	// sbom.dependency_relationship evidence rows persisted per document.
	// Bounding happens at WRITE time (here) because the attachment fact's
	// payload is stored in the shared fact JSONB column, not just returned
	// on the wire — an unbounded evidence array bloats every read of the
	// fact, not only large-result API pages.
	// DependencyRelationshipCount on the decision always reports the full
	// distinct-tuple count computed BEFORE this cap, so a caller can tell
	// truncation happened even though the persisted row set is bounded.
	maxSBOMAttachmentDependencyRelationshipRows = 100
	// maxSBOMAttachmentExternalReferenceRows bounds the number of
	// sbom.external_reference evidence rows persisted per document,
	// mirroring maxSBOMAttachmentDependencyRelationshipRows.
	maxSBOMAttachmentExternalReferenceRows = 50
)

// dependencyRelationshipEvidenceRows deduplicates the decoded
// sbom.dependency_relationship evidence for one document on
// (from_component_id, to_component_id, relationship_type,
// relationship_origin), sorts the distinct tuples lexicographically with
// fact_id as the final tiebreaker for a deterministic cap (never inheriting
// fact envelope load order, which would make the cap flaky under a
// different load/replay order), and returns the capped row set plus the
// full distinct-tuple count computed BEFORE the cap.
func dependencyRelationshipEvidenceRows(deps []sbomAttachmentDependencyEvidence) ([]map[string]string, int) {
	sorted := append([]sbomAttachmentDependencyEvidence(nil), deps...)
	sort.Slice(sorted, func(i, j int) bool {
		return dependencyRelationshipLess(sorted[i], sorted[j])
	})
	deduped := make([]sbomAttachmentDependencyEvidence, 0, len(sorted))
	for i, dep := range sorted {
		if i > 0 && dependencyRelationshipTupleEqual(sorted[i-1], dep) {
			continue
		}
		deduped = append(deduped, dep)
	}
	count := len(deduped)
	if count > maxSBOMAttachmentDependencyRelationshipRows {
		deduped = deduped[:maxSBOMAttachmentDependencyRelationshipRows]
	}
	out := make([]map[string]string, 0, len(deduped))
	for _, dep := range deduped {
		out = append(out, map[string]string{
			"from_component_id":   dep.fromComponentID,
			"to_component_id":     dep.toComponentID,
			"relationship_type":   dep.relationshipType,
			"relationship_origin": dep.relationshipOrigin,
			"fact_id":             dep.factID,
		})
	}
	return out, count
}

func dependencyRelationshipLess(a, b sbomAttachmentDependencyEvidence) bool {
	if a.fromComponentID != b.fromComponentID {
		return a.fromComponentID < b.fromComponentID
	}
	if a.toComponentID != b.toComponentID {
		return a.toComponentID < b.toComponentID
	}
	if a.relationshipType != b.relationshipType {
		return a.relationshipType < b.relationshipType
	}
	if a.relationshipOrigin != b.relationshipOrigin {
		return a.relationshipOrigin < b.relationshipOrigin
	}
	return a.factID < b.factID
}

func dependencyRelationshipTupleEqual(a, b sbomAttachmentDependencyEvidence) bool {
	return a.fromComponentID == b.fromComponentID &&
		a.toComponentID == b.toComponentID &&
		a.relationshipType == b.relationshipType &&
		a.relationshipOrigin == b.relationshipOrigin
}

// externalReferenceEvidenceRows deduplicates the decoded
// sbom.external_reference evidence for one document on (component_id,
// reference_type, reference_url, reference_locator), sorts the distinct
// tuples lexicographically with fact_id as the final tiebreaker, and
// returns the capped row set plus the full distinct-tuple count computed
// BEFORE the cap. Mirrors dependencyRelationshipEvidenceRows.
func externalReferenceEvidenceRows(refs []sbomAttachmentExternalReferenceEvidence) ([]map[string]string, int) {
	sorted := append([]sbomAttachmentExternalReferenceEvidence(nil), refs...)
	sort.Slice(sorted, func(i, j int) bool {
		return externalReferenceLess(sorted[i], sorted[j])
	})
	deduped := make([]sbomAttachmentExternalReferenceEvidence, 0, len(sorted))
	for i, ref := range sorted {
		if i > 0 && externalReferenceTupleEqual(sorted[i-1], ref) {
			continue
		}
		deduped = append(deduped, ref)
	}
	count := len(deduped)
	if count > maxSBOMAttachmentExternalReferenceRows {
		deduped = deduped[:maxSBOMAttachmentExternalReferenceRows]
	}
	out := make([]map[string]string, 0, len(deduped))
	for _, ref := range deduped {
		out = append(out, map[string]string{
			"component_id":      ref.componentID,
			"reference_type":    ref.referenceType,
			"reference_url":     ref.referenceURL,
			"reference_locator": ref.referenceLocator,
			"fact_id":           ref.factID,
		})
	}
	return out, count
}

func externalReferenceLess(a, b sbomAttachmentExternalReferenceEvidence) bool {
	if a.componentID != b.componentID {
		return a.componentID < b.componentID
	}
	if a.referenceType != b.referenceType {
		return a.referenceType < b.referenceType
	}
	if a.referenceURL != b.referenceURL {
		return a.referenceURL < b.referenceURL
	}
	if a.referenceLocator != b.referenceLocator {
		return a.referenceLocator < b.referenceLocator
	}
	return a.factID < b.factID
}

func externalReferenceTupleEqual(a, b sbomAttachmentExternalReferenceEvidence) bool {
	return a.componentID == b.componentID &&
		a.referenceType == b.referenceType &&
		a.referenceURL == b.referenceURL &&
		a.referenceLocator == b.referenceLocator
}
