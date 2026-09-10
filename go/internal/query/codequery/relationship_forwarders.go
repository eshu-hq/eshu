// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
)

// This file keeps the pre-move spellings the handler and the pinned
// readers in relationship_handlers.go resolve, forwarding to the
// relationships leaf. The pinned readers themselves stay in
// relationship_handlers.go under their queryplan source_sha256 pins.

// relationshipCapability resolves the contract capability for one
// direction and relationship type.
func relationshipCapability(direction, relationshipType string) string {
	return relationships.Capability(direction, relationshipType)
}

// transitiveRelationshipCapability resolves the contract capability for
// transitive CALLS in one direction.
func transitiveRelationshipCapability(direction string) string {
	return relationships.TransitiveCapability(direction)
}

// transitiveRelationshipUnsupportedMessage renders the capability-gate
// message for transitive CALLS in one direction.
func transitiveRelationshipUnsupportedMessage(direction string) string {
	return relationships.TransitiveUnsupportedMessage(direction)
}

// normalizeRelationshipDirection coerces the direction filter
// handleRelationships accepts.
func normalizeRelationshipDirection(direction string) (string, error) {
	return relationships.NormalizeDirection(direction)
}

// filterRelationshipResponse shapes the handler payload after the graph
// rows resolve. Tests name this spelling.
func filterRelationshipResponse(
	response map[string]any,
	direction string,
	relationshipType string,
) map[string]any {
	return relationships.FilterResponse(response, direction, relationshipType)
}

// mapRelationships coerces a response leg to relationship rows. Tests
// name this spelling.
func mapRelationships(value any) []map[string]any {
	return relationships.MapRelationships(value)
}

// nornicDBRelationshipsGraphRow resolves one entity's direct
// relationships through the relationships leaf, pre-resolving the
// entity label through the pinned reader so the digest keeps covering
// the live path. The pinned graph reader and tests name this spelling.
func (h *CodeHandler) nornicDBRelationshipsGraphRow(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
	direction string,
	relationshipType string,
) (map[string]any, error) {
	label, err := h.nornicDBRelationshipEntityLabel(ctx, entityID, repoID)
	if err != nil {
		return nil, err
	}
	return relationships.GraphRow(ctx, h.Neo4j, entityID, name, repoID, direction, relationshipType, codeGrantAccessFilter(ctx), label)
}

// nornicDBRelationshipMetadataRow reads the metadata row through the
// relationships leaf, pre-resolving the entity label through the pinned
// reader. The pinned transitive reader names this spelling.
func (h *CodeHandler) nornicDBRelationshipMetadataRow(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
) (map[string]any, error) {
	label, err := h.nornicDBRelationshipEntityLabel(ctx, entityID, repoID)
	if err != nil {
		return nil, err
	}
	return relationships.MetadataRow(ctx, h.Neo4j, entityID, name, repoID, codeGrantAccessFilter(ctx), label)
}
