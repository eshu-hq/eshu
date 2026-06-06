package query

import (
	"context"
	"strings"
)

const uncorrelatedCloudResourceCandidateLimit = serviceStoryItemLimit

func loadUncorrelatedCloudResourceCandidates(
	ctx context.Context,
	graph GraphQuery,
	serviceName string,
	limit int,
) ([]map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	if graph == nil || serviceName == "" {
		return nil, nil
	}
	if limit <= 0 || limit > uncorrelatedCloudResourceCandidateLimit {
		limit = uncorrelatedCloudResourceCandidateLimit
	}
	rows, err := graph.Run(ctx, `
MATCH (c:CloudResource)
WHERE $service_name <> ''
  AND (
    toLower(coalesce(c.name, '')) CONTAINS $service_token OR
    toLower(coalesce(c.resource_id, '')) CONTAINS $service_token OR
    toLower(coalesce(c.arn, '')) CONTAINS $service_token
  )
RETURN DISTINCT coalesce(c.id, c.uid, c.resource_id, c.arn, c.name) AS id,
       c.name AS name,
       coalesce(c.kind, c.resource_type, c.data_type, '') AS kind,
       coalesce(c.resource_type, c.data_type, c.kind, '') AS resource_type,
       coalesce(c.provider, c.source_system, '') AS provider,
       coalesce(c.environment, '') AS environment,
       coalesce(c.resource_id, '') AS resource_id,
       coalesce(c.arn, '') AS arn,
       coalesce(c.account_id, '') AS account_id,
       coalesce(c.region, '') AS region
ORDER BY name, id
LIMIT $limit`, map[string]any{
		"service_name":  serviceName,
		"service_token": strings.ToLower(serviceName),
		"limit":         limit,
	})
	if err != nil {
		return nil, err
	}
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		candidate := compactStringMap(map[string]any{
			"id":                   StringVal(row, "id"),
			"name":                 StringVal(row, "name"),
			"kind":                 StringVal(row, "kind"),
			"resource_type":        StringVal(row, "resource_type"),
			"provider":             StringVal(row, "provider"),
			"environment":          StringVal(row, "environment"),
			"resource_id":          StringVal(row, "resource_id"),
			"arn":                  StringVal(row, "arn"),
			"account_id":           StringVal(row, "account_id"),
			"region":               StringVal(row, "region"),
			"candidate_status":     "uncorrelated",
			"missing_relationship": "workload_cloud_relationship",
		})
		if len(candidate) > 0 {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, nil
}
