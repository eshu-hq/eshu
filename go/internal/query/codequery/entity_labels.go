// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the NornicDB entity-label reader of the relationships
// family. Label resolution, the metadata builders, and the inheritance
// grant filter live in the relationships leaf; nornicDBRelationshipEntityLabel
// stays here because it carries a queryplan source_sha256 pin in
// grandfathered_non_hot.go -- its body is byte-identical to the
// pre-move text, and the builders it names resolve through the
// same-named forwarders below. Edit a pinned body only with a manifest
// update in the same change; re-freezing a digest to match an edit is
// not a fix.

func (h *CodeHandler) nornicDBRelationshipEntityLabel(
	ctx context.Context,
	entityID string,
	repoID string,
) (string, error) {
	entityID = strings.TrimSpace(entityID)
	if h == nil || entityID == "" {
		return "", nil
	}
	if h.Content != nil {
		entity, err := h.Content.GetEntityContent(ctx, entityID)
		if err == nil && entity != nil {
			return nornicDBGraphLabelForContentEntityType(entity.EntityType), nil
		}
	}
	if h.Neo4j == nil {
		return "", nil
	}
	params := map[string]any{"entity_id": entityID}
	repoID = strings.TrimSpace(repoID)
	if repoID != "" {
		params["repo_id"] = repoID
	}
	for _, property := range []string{"uid", "id"} {
		rows, err := h.Neo4j.Run(
			ctx,
			nornicDBRelationshipEntityLabelCypher(property, repoID != ""),
			params,
		)
		if err != nil {
			return "", err
		}
		if len(rows) == 1 {
			return nornicDBPrimaryEntityLabel(rows[0]), nil
		}
		if len(rows) > 1 {
			return "", nil
		}
	}
	return "", nil
}

// The helpers below keep the pre-move spellings the pinned body above
// resolves, forwarding to the relationships leaf.

// nornicDBGraphLabelForContentEntityType resolves a content entity type
// to its graph label, or "" when the type is not a known code entity.
func nornicDBGraphLabelForContentEntityType(entityType string) string {
	return relationships.NornicDBGraphLabelForContentEntityType(entityType)
}

// nornicDBRelationshipEntityLabelCypher builds the per-label UNION
// lookup for one entity id property.
func nornicDBRelationshipEntityLabelCypher(property string, repositoryScoped bool) string {
	return relationships.EntityLabelCypher(property, repositoryScoped)
}

// nornicDBPrimaryEntityLabel returns the row's first known code-entity
// label, or "" when the row carries none.
func nornicDBPrimaryEntityLabel(row map[string]any) string {
	return relationships.PrimaryEntityLabel(row)
}

// nornicDBRelationshipMetadataPredicate builds the metadata lookup's
// WHERE and its parameters. The grant proof names this spelling.
func nornicDBRelationshipMetadataPredicate(
	name string,
	repoID string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return relationships.MetadataPredicate(name, repoID, access)
}

// nornicDBRelationshipMetadataCypher builds the single-clause metadata
// read. The grant proof names this spelling.
func nornicDBRelationshipMetadataCypher(predicate string, entityLabel string, entityIDProperty string) string {
	return relationships.MetadataCypher(predicate, entityLabel, entityIDProperty)
}
