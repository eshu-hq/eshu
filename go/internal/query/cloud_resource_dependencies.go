package query

import (
	"context"
	"strings"
)

const serviceCloudResourceDependencyLimit = serviceStoryItemLimit

func loadMaterializedServiceCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	workloadID string,
	limit int,
) ([]map[string]any, error) {
	workloadID = strings.TrimSpace(workloadID)
	if graph == nil || workloadID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > serviceCloudResourceDependencyLimit {
		limit = serviceCloudResourceDependencyLimit
	}
	rows, err := graph.Run(ctx, `
MATCH (workload:Workload {id: $workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)-[rel:USES]->(c:CloudResource)
RETURN DISTINCT coalesce(c.id, c.uid, c.resource_id, c.arn, c.name) AS id,
       c.name AS name,
       coalesce(c.kind, c.resource_type, c.data_type, '') AS kind,
       coalesce(c.resource_type, c.data_type, c.kind, '') AS resource_type,
       coalesce(c.provider, c.source_system, '') AS provider,
       coalesce(rel.environment, c.environment, instance.environment, '') AS environment,
       coalesce(c.resource_id, '') AS resource_id,
       coalesce(c.arn, '') AS arn,
       coalesce(c.account_id, '') AS account_id,
       coalesce(c.region, '') AS region,
       coalesce(rel.resolution_mode, '') AS resolution_mode,
       coalesce(rel.evidence_source, '') AS evidence_source,
       coalesce(rel.relationship_basis, 'materialized_workload_cloud_relationship') AS relationship_basis,
       coalesce(rel.service_anchor_source, '') AS service_anchor_source,
       coalesce(rel.service_anchor_reason, '') AS service_anchor_reason,
       coalesce(rel.source_fact_id, '') AS source_fact_id,
       coalesce(rel.stable_fact_key, '') AS stable_fact_key,
       coalesce(rel.source_system, '') AS source_system,
       coalesce(rel.source_record_id, '') AS source_record_id,
       coalesce(rel.collector_kind, '') AS collector_kind
ORDER BY name, id
LIMIT $limit`, map[string]any{
		"workload_id": workloadID,
		"limit":       limit,
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
			"relationship_basis":    StringVal(row, "relationship_basis"),
			"resolution_mode":       StringVal(row, "resolution_mode"),
			"evidence_source":       StringVal(row, "evidence_source"),
			"service_anchor_source": StringVal(row, "service_anchor_source"),
			"service_anchor_reason": StringVal(row, "service_anchor_reason"),
			"source_fact_id":        StringVal(row, "source_fact_id"),
			"stable_fact_key":       StringVal(row, "stable_fact_key"),
			"source_system":         StringVal(row, "source_system"),
			"source_record_id":      StringVal(row, "source_record_id"),
			"collector_kind":        StringVal(row, "collector_kind"),
		})
		if len(resource) > 0 {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}
