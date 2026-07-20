// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// DependencyRelationshipRow exposes one bounded sbom.dependency_relationship
// evidence row attached to a document. Rows are bounded and deduplicated at
// reducer write time (go/internal/reducer/sbom_attestation_attachment_evidence_bounds.go);
// DependencyRelationshipCount on the parent row/result reports the full
// distinct-tuple count so a caller can detect truncation.
type DependencyRelationshipRow struct {
	FromComponentID    string `json:"from_component_id,omitempty"`
	ToComponentID      string `json:"to_component_id,omitempty"`
	RelationshipType   string `json:"relationship_type,omitempty"`
	RelationshipOrigin string `json:"relationship_origin,omitempty"`
	FactID             string `json:"fact_id,omitempty"`
}

// ExternalReferenceRow exposes one bounded sbom.external_reference evidence
// row attached to a document or component. Mirrors DependencyRelationshipRow's
// bounding discipline.
type ExternalReferenceRow struct {
	ComponentID      string `json:"component_id,omitempty"`
	ReferenceType    string `json:"reference_type,omitempty"`
	ReferenceURL     string `json:"reference_url,omitempty"`
	ReferenceLocator string `json:"reference_locator,omitempty"`
	FactID           string `json:"fact_id,omitempty"`
}

// dependencyRelationshipRowsFromPayload decodes the reducer-written
// "dependency_relationship_evidence" payload array into typed rows.
func dependencyRelationshipRowsFromPayload(raw any) []DependencyRelationshipRow {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]DependencyRelationshipRow, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, DependencyRelationshipRow{
			FromComponentID:    StringVal(row, "from_component_id"),
			ToComponentID:      StringVal(row, "to_component_id"),
			RelationshipType:   StringVal(row, "relationship_type"),
			RelationshipOrigin: StringVal(row, "relationship_origin"),
			FactID:             StringVal(row, "fact_id"),
		})
	}
	return out
}

// externalReferenceRowsFromPayload decodes the reducer-written
// "external_reference_evidence" payload array into typed rows.
func externalReferenceRowsFromPayload(raw any) []ExternalReferenceRow {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ExternalReferenceRow, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, ExternalReferenceRow{
			ComponentID:      StringVal(row, "component_id"),
			ReferenceType:    StringVal(row, "reference_type"),
			ReferenceURL:     StringVal(row, "reference_url"),
			ReferenceLocator: StringVal(row, "reference_locator"),
			FactID:           StringVal(row, "fact_id"),
		})
	}
	return out
}
