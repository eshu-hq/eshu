// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// UncorrelatedCloudResourceCandidateLimit caps uncorrelated cloud-resource
// candidate reads.
const UncorrelatedCloudResourceCandidateLimit = querycontract.ServiceStoryItemLimit

// InfraResourceFreeTextPredicate stays on one line for NornicDB compatibility.
const InfraResourceFreeTextPredicate = "(coalesce(n.name, '') CONTAINS $query OR coalesce(n.id, '') CONTAINS $query OR coalesce(n.kind, '') CONTAINS $query OR coalesce(n.resource_type, n.data_type, '') = $resource_type_query OR coalesce(n.resource_type, n.data_type, '') CONTAINS $resource_type_query OR coalesce(n.arn, '') CONTAINS $query OR coalesce(n.resource_id, '') CONTAINS $query OR coalesce(n.service_kind, '') CONTAINS $query OR coalesce(n.account_id, '') CONTAINS $query OR coalesce(n.region, '') CONTAINS $query OR coalesce(n.source, '') CONTAINS $query OR coalesce(n.config_path, '') CONTAINS $query)"

// loadUncorrelatedCloudResourceCandidates returns uncorrelated CloudResource
// candidates that mention the service. It discards the truncation signal; prefer
// loadUncorrelatedCloudResourceCandidatesBounded when the caller must surface
// whether the backend held more rows than the bound.
func LoadUncorrelatedCloudResourceCandidates(
	ctx context.Context,
	graph querycontract.GraphQuery,
	serviceName string,
	limit int,
) ([]map[string]any, error) {
	candidates, _, err := LoadUncorrelatedCloudResourceCandidatesBounded(ctx, graph, serviceName, limit)
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
func LoadUncorrelatedCloudResourceCandidatesBounded(
	ctx context.Context,
	graph querycontract.GraphQuery,
	serviceName string,
	limit int,
) (candidates []map[string]any, truncated bool, err error) {
	serviceName = strings.TrimSpace(serviceName)
	if graph == nil || serviceName == "" {
		return nil, false, nil
	}
	// CloudResource nodes do not carry repository ownership. Free-text matches
	// are uncorrelated candidates, so scoped tokens cannot safely authorize them.
	if querycontract.RepositoryAccessFilterFromContext(ctx).Scoped() {
		return nil, false, nil
	}
	if limit <= 0 || limit > UncorrelatedCloudResourceCandidateLimit {
		limit = UncorrelatedCloudResourceCandidateLimit
	}
	// Over-fetch by one row to detect truncation without a second count query.
	fetchLimit := limit + 1
	rows, err := graph.Run(ctx, `
MATCH (n:CloudResource)
WHERE `+InfraResourceFreeTextPredicate+`
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
ORDER BY n.name, n.id
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
		candidate := querycontract.CompactStringMap(map[string]any{
			"id":                    querycontract.StringVal(row, "id"),
			"name":                  querycontract.StringVal(row, "name"),
			"kind":                  querycontract.StringVal(row, "kind"),
			"resource_type":         querycontract.StringVal(row, "resource_type"),
			"provider":              querycontract.FirstNonEmptyString(querycontract.StringVal(row, "provider"), querycontract.StringVal(row, "source_system")),
			"environment":           querycontract.StringVal(row, "environment"),
			"source":                querycontract.StringVal(row, "source"),
			"config_path":           querycontract.StringVal(row, "config_path"),
			"resource_service":      querycontract.StringVal(row, "resource_service"),
			"resource_category":     querycontract.StringVal(row, "resource_category"),
			"resource_id":           querycontract.StringVal(row, "resource_id"),
			"arn":                   querycontract.StringVal(row, "arn"),
			"account_id":            querycontract.StringVal(row, "account_id"),
			"region":                querycontract.StringVal(row, "region"),
			"service_kind":          querycontract.StringVal(row, "service_kind"),
			"service_anchor_status": querycontract.StringVal(row, "service_anchor_status"),
			"candidate_status":      cloudResourceCandidateStatus(row),
			"service_anchor_reason": querycontract.StringVal(row, "service_anchor_reason"),
			"missing_relationship":  "workload_cloud_relationship",
		})
		if len(candidate) > 0 {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, truncated, nil
}

func cloudResourceCandidateStatus(row map[string]any) string {
	switch querycontract.StringVal(row, "service_anchor_status") {
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
