// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const documentationStoryReadLimit = querycontract.DocumentationStoryReadLimit

func loadServiceStoryTargetDocumentationForOperation(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
	operation string,
) (map[string]any, error) {
	if strings.TrimSpace(operation) != "service_story" {
		return nil, nil
	}
	return loadServiceStoryTargetDocumentation(ctx, content, workloadContext)
}

func loadServiceStoryTargetDocumentation(
	ctx context.Context,
	content ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	repoID := safeStr(workloadContext, "repo_id")
	serviceID := safeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return nil, nil
	}
	filter := documentationFindingFilter{
		Repository: repoID,
		Limit:      documentationStoryReadLimit,
	}
	if serviceID != "" {
		filter.TargetKind = "service"
		filter.TargetID = serviceID
		filter.ServiceID = serviceID
	} else {
		filter.TargetKind = "repository"
		filter.TargetID = repoID
	}
	return querycontract.LoadStoryTargetDocumentation(ctx, content, filter)
}
