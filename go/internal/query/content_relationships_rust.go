// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

// buildOutgoingRustImplBlockRelationships lists the Functions an ImplBlock
// contains by scanning the repository's Functions and keeping those whose
// impl_context names the block.
//
// The scan is capped at the 20-row content-lookup ceiling before the
// impl_context filter runs, so the truncation flag means the scan hit that
// ceiling, not that more members exist. Any repository with more than 20
// Functions therefore reports its ImplBlocks as truncated, even when every
// real member was returned.
func buildOutgoingRustImplBlockRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, bool, error) {
	if entity.EntityType != "ImplBlock" || entity.EntityName == "" {
		return nil, false, false, nil
	}

	matches, err := reader.SearchEntitiesByName(
		ctx, entity.RepoID, "Function", "", contentRelationshipFetchLimit,
	)
	if err != nil {
		return nil, true, false, fmt.Errorf("search rust impl functions: %w", err)
	}
	matches, clipped := capContentLookup(matches)

	relationships := make([]map[string]any, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if match.EntityID == entity.EntityID || match.RelativePath != entity.RelativePath {
			continue
		}
		if implContext, _ := metadataNonEmptyString(match.Metadata, "impl_context"); implContext != entity.EntityName {
			continue
		}
		if _, ok := seen[match.EntityID]; ok {
			continue
		}
		seen[match.EntityID] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        "CONTAINS",
			"target_name": match.EntityName,
			"target_id":   match.EntityID,
			"reason":      "rust_impl_context",
		})
	}

	return relationships, true, clipped, nil
}

func buildIncomingRustImplBlockRelationships(
	ctx context.Context,
	reader ContentStore,
	entity EntityContent,
) ([]map[string]any, bool, bool, error) {
	implContext, ok := metadataNonEmptyString(entity.Metadata, "impl_context")
	if entity.EntityType != "Function" || !ok || implContext == "" {
		return nil, false, false, nil
	}

	matches, err := reader.SearchEntitiesByName(
		ctx, entity.RepoID, "ImplBlock", implContext, contentRelationshipFetchLimit,
	)
	if err != nil {
		return nil, true, false, fmt.Errorf("search rust impl blocks: %w", err)
	}
	matches, clipped := capContentLookup(matches)

	relationships := make([]map[string]any, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if match.EntityID == entity.EntityID || match.RelativePath != entity.RelativePath {
			continue
		}
		if _, ok := seen[match.EntityID]; ok {
			continue
		}
		seen[match.EntityID] = struct{}{}
		relationships = append(relationships, map[string]any{
			"type":        "CONTAINS",
			"source_name": match.EntityName,
			"source_id":   match.EntityID,
			"reason":      "rust_impl_context",
		})
	}

	return relationships, true, clipped, nil
}
