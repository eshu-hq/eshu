package query

import (
	"context"
	"strings"
)

const serviceCloudResourceDependencyLimit = serviceStoryItemLimit

func loadServiceCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	workloadID string,
	serviceName string,
	limit int,
) ([]map[string]any, error) {
	workloadID = strings.TrimSpace(workloadID)
	serviceName = strings.TrimSpace(serviceName)
	if graph == nil || (workloadID == "" && serviceName == "") {
		return nil, nil
	}
	if limit <= 0 || limit > serviceCloudResourceDependencyLimit {
		limit = serviceCloudResourceDependencyLimit
	}
	rows, err := graph.Run(ctx, `
MATCH (c:CloudResource)
WHERE c.service_anchor_status = 'strong'
  AND (
    ($workload_id <> '' AND coalesce(c.workload_id, '') = $workload_id) OR
    ($service_name <> '' AND toLower(coalesce(c.service_name, '')) = $service_token)
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
       coalesce(c.region, '') AS region,
       coalesce(c.service_anchor_status, '') AS service_anchor_status,
       coalesce(c.service_anchor_source, '') AS service_anchor_source,
       coalesce(c.service_anchor_reason, '') AS service_anchor_reason
ORDER BY name, id
LIMIT $limit`, map[string]any{
		"workload_id":   workloadID,
		"service_name":  serviceName,
		"service_token": strings.ToLower(serviceName),
		"limit":         limit,
	})
	if err != nil {
		return nil, err
	}
	resources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		resource := compactStringMap(map[string]any{
			"id":                    StringVal(row, "id"),
			"name":                  StringVal(row, "name"),
			"kind":                  StringVal(row, "kind"),
			"resource_type":         StringVal(row, "resource_type"),
			"provider":              StringVal(row, "provider"),
			"environment":           StringVal(row, "environment"),
			"resource_id":           StringVal(row, "resource_id"),
			"arn":                   StringVal(row, "arn"),
			"account_id":            StringVal(row, "account_id"),
			"region":                StringVal(row, "region"),
			"relationship_basis":    "aws_resource_service_anchor",
			"service_anchor_status": StringVal(row, "service_anchor_status"),
			"service_anchor_source": StringVal(row, "service_anchor_source"),
			"service_anchor_reason": StringVal(row, "service_anchor_reason"),
		})
		if len(resource) > 0 {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}
