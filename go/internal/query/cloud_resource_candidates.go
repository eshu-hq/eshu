// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

const uncorrelatedCloudResourceCandidateLimit = serviceStoryItemLimit

// infraResourceFreeTextPredicate stays on one line for NornicDB compatibility.
const infraResourceFreeTextPredicate = "(coalesce(n.name, '') CONTAINS $query OR coalesce(n.id, '') CONTAINS $query OR coalesce(n.kind, '') CONTAINS $query OR coalesce(n.resource_type, n.data_type, '') = $resource_type_query OR coalesce(n.resource_type, n.data_type, '') CONTAINS $resource_type_query OR coalesce(n.arn, '') CONTAINS $query OR coalesce(n.resource_id, '') CONTAINS $query OR coalesce(n.service_kind, '') CONTAINS $query OR coalesce(n.account_id, '') CONTAINS $query OR coalesce(n.region, '') CONTAINS $query OR coalesce(n.source, '') CONTAINS $query OR coalesce(n.config_path, '') CONTAINS $query)"

// loadUncorrelatedCloudResourceCandidates returns uncorrelated CloudResource
// candidates that mention the service. It discards the truncation signal; prefer
// loadUncorrelatedCloudResourceCandidatesBounded when the caller must surface
// whether the backend held more rows than the bound.
func loadUncorrelatedCloudResourceCandidates(
	ctx context.Context,
	graph GraphQuery,
	serviceName string,
	limit int,
) ([]map[string]any, error) {
	candidates, _, err := loadUncorrelatedCloudResourceCandidatesBounded(ctx, graph, serviceName, limit)
	return candidates, err
}

// loadUncorrelatedCloudResourceCandidatesBounded reads uncorrelated
// CloudResource candidates whose free-text fields mention the service name. The
// MATCH anchors the CloudResource label in the pattern so NornicDB uses a label
// scan rather than an all-node scan over the entire graph; the prior unlabeled
// `MATCH (n) WHERE (n:CloudResource)` shape forced a full-graph scan that hung
// the service-story dossier at repo scale (issue #3378, 481,728 nodes).
//
// It over-fetches one row beyond limit so the caller can report explicit
// truncation: truncated is true when the backend held more matches than limit.
// The returned rows are trimmed back to limit.
func loadUncorrelatedCloudResourceCandidatesBounded(
	ctx context.Context,
	graph GraphQuery,
	serviceName string,
	limit int,
) (candidates []map[string]any, truncated bool, err error) {
	serviceName = strings.TrimSpace(serviceName)
	if graph == nil || serviceName == "" {
		return nil, false, nil
	}
	if limit <= 0 || limit > uncorrelatedCloudResourceCandidateLimit {
		limit = uncorrelatedCloudResourceCandidateLimit
	}
	// Over-fetch by one row to detect truncation without a second count query.
	fetchLimit := limit + 1
	rows, err := graph.Run(ctx, `
MATCH (n:CloudResource)
WHERE `+infraResourceFreeTextPredicate+`
RETURN coalesce(n.id, '') AS id,
       coalesce(n.name, '') AS name,
       coalesce(n.kind, '') AS kind,
       coalesce(n.provider, '') AS provider,
       coalesce(n.source_system, '') AS source_system,
       coalesce(n.environment, '') AS environment,
       coalesce(n.source, n.source_system, '') AS source,
       coalesce(n.config_path, '') AS config_path,
       coalesce(n.resource_type, n.data_type, '') AS resource_type,
       coalesce(n.resource_service, n.service_kind, '') AS resource_service,
       coalesce(n.resource_category, '') AS resource_category,
       coalesce(n.resource_id, '') AS resource_id,
       coalesce(n.arn, '') AS arn,
       coalesce(n.account_id, '') AS account_id,
       coalesce(n.region, '') AS region,
       coalesce(n.service_kind, '') AS service_kind,
       coalesce(n.service_anchor_status, '') AS service_anchor_status,
       coalesce(n.service_anchor_reason, '') AS service_anchor_reason
ORDER BY n.name
LIMIT $limit`, map[string]any{
		"query":               serviceName,
		"resource_type_query": serviceName,
		"limit":               fetchLimit,
	})
	if err != nil {
		return nil, false, err
	}
	truncated = len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	candidates = make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		candidate := compactStringMap(map[string]any{
			"id":                    StringVal(row, "id"),
			"name":                  StringVal(row, "name"),
			"kind":                  StringVal(row, "kind"),
			"resource_type":         StringVal(row, "resource_type"),
			"provider":              firstNonEmptyString(StringVal(row, "provider"), StringVal(row, "source_system")),
			"environment":           StringVal(row, "environment"),
			"source":                StringVal(row, "source"),
			"config_path":           StringVal(row, "config_path"),
			"resource_service":      StringVal(row, "resource_service"),
			"resource_category":     StringVal(row, "resource_category"),
			"resource_id":           StringVal(row, "resource_id"),
			"arn":                   StringVal(row, "arn"),
			"account_id":            StringVal(row, "account_id"),
			"region":                StringVal(row, "region"),
			"service_kind":          StringVal(row, "service_kind"),
			"service_anchor_status": StringVal(row, "service_anchor_status"),
			"candidate_status":      cloudResourceCandidateStatus(row),
			"service_anchor_reason": StringVal(row, "service_anchor_reason"),
			"missing_relationship":  "workload_cloud_relationship",
		})
		if len(candidate) > 0 {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, truncated, nil
}

func cloudResourceCandidateStatus(row map[string]any) string {
	switch StringVal(row, "service_anchor_status") {
	case "ambiguous":
		return "ambiguous_anchor"
	case "stale":
		return "stale_anchor"
	case "weak":
		return "weak_anchor"
	default:
		return "uncorrelated"
	}
}
