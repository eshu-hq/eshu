// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// Service API surface endpoint paging bounds (#10477): the dossier preview
// stays at the 50-row serviceStoryItemLimit, while the dedicated list tool
// pages the same ordered read with its own caller-chosen page size inside
// the shared indirect-evidence ceiling.
const (
	// ServiceAPISurfaceListDefaultLimit is the page size a list call gets
	// when it names none.
	ServiceAPISurfaceListDefaultLimit = 50
	// ServiceAPISurfaceListMaxLimit caps a caller-named page size. It reuses
	// the shared indirect-evidence ceiling so one list page cannot reprice
	// the shared reads that ceiling protects.
	ServiceAPISurfaceListMaxLimit = querycontract.MaxIndirectEvidenceSearchLimit
)

// ListServiceAPISurfaceEndpoints returns one page of a service repository's
// API surface endpoints in ORDER BY path, endpoint_id order (#10477). offset
// applies at the Cypher read, never by slicing a capped preview. The page
// carries endpoints, endpoint_count (the count-query total, not the page
// size), detail_limit/detail_truncated for the page, offset, ordering, and
// next_offset only when rows remain past the page. A non-positive limit falls
// back to the default page size and a negative offset to zero.
func ListServiceAPISurfaceEndpoints(
	ctx context.Context,
	graph querycontract.GraphQuery,
	repoID string,
	limit, offset int,
) (map[string]any, error) {
	if graph == nil || strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = ServiceAPISurfaceListDefaultLimit
	}
	if limit > ServiceAPISurfaceListMaxLimit {
		limit = ServiceAPISurfaceListMaxLimit
	}
	if offset < 0 {
		offset = 0
	}
	surface, err := repository.QueryRepoAPISurfacePaged(ctx, graph, map[string]any{"repo_id": repoID}, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list service API surface endpoints: %w", err)
	}
	if len(surface) == 0 {
		return nil, nil
	}
	surface["ordering"] = "path, endpoint_id"
	return surface, nil
}
