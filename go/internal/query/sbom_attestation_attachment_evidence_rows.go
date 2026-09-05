// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// DependencyRelationshipRow and ExternalReferenceRow moved to
// internal/query/supplychain with the attachment row that embeds them (#6060
// lane A); the staying decode helpers below reach them through root's
// aliases. See supply_chain_hub_alias.go.

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
