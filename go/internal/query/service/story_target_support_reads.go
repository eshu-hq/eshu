// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file hosts the operation-gated target-support and target-documentation
// loaders behind the service-story enrichment (Issue #6060, lane B B4). They
// moved here from the query root (service_story_target_support.go and
// documentation_story_overview.go), whose *ContentReader evidence methods
// must stay in package query: Go requires methods to live with their
// receiver type. The staying ContentReader method keeps serving the same
// rows through the exported LoadServiceStoryTargetSupport home. Bodies are
// unchanged modulo package qualifiers and the export rename below.

// serviceStoryTargetSupportStore is the read-model surface the loaders need.
// It is structural: the staying *ContentReader satisfies it without
// importing this package.
type serviceStoryTargetSupportStore interface {
	ServiceStoryTargetSupportEvidence(
		context.Context,
		querycontract.ServiceStoryTargetSupportFilter,
	) (querycontract.ServiceStoryTargetSupportReadModel, error)
}

// LoadServiceStoryTargetSupport loads the target-support section for a
// service-story workload context, or (nil, nil) when no store backs the
// read. Pinned by the staying interface-export tripwire test via the root
// forwarder, and by loadServiceStoryTargetSupportForOperation below.
func LoadServiceStoryTargetSupport(
	ctx context.Context,
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	store, ok := content.(serviceStoryTargetSupportStore)
	if !ok || store == nil {
		return nil, nil
	}
	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	serviceID := querycontract.SafeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return nil, nil
	}
	filter := querycontract.ServiceStoryTargetSupportFilter{
		Repository: repoID,
		Limit:      querycontract.ServiceStoryTargetSupportLimit,
	}
	if serviceID != "" {
		filter.TargetKind = "service"
		filter.TargetID = serviceID
		filter.ServiceID = serviceID
	} else {
		filter.TargetKind = "repository"
		filter.TargetID = repoID
	}
	readModel, err := store.ServiceStoryTargetSupportEvidence(ctx, filter)
	if err != nil {
		return nil, err
	}
	return readModel.Support, nil
}

func loadServiceStoryTargetSupportForOperation(
	ctx context.Context,
	content querycontract.ContentStore,
	workloadContext map[string]any,
	operation string,
) (map[string]any, error) {
	if strings.TrimSpace(operation) != "service_story" {
		return nil, nil
	}
	return LoadServiceStoryTargetSupport(ctx, content, workloadContext)
}

func loadServiceStoryTargetDocumentationForOperation(
	ctx context.Context,
	content querycontract.ContentStore,
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
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	serviceID := querycontract.SafeStr(workloadContext, "id")
	if repoID == "" && serviceID == "" {
		return nil, nil
	}
	filter := querycontract.DocumentationFindingFilter{
		Repository: repoID,
		Limit:      querycontract.DocumentationStoryReadLimit,
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
