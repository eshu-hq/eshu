// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ServiceCloudResourceDependencyLimit caps config-derived cloud-resource
// dependency reads.
const ServiceCloudResourceDependencyLimit = querycontract.ServiceStoryItemLimit

func LoadMaterializedServiceCloudResourceDependencies(
	ctx context.Context,
	graph querycontract.GraphQuery,
	repoID string,
	workloadID string,
	limit int,
) ([]map[string]any, error) {
	repoID = strings.TrimSpace(repoID)
	workloadID = strings.TrimSpace(workloadID)
	if graph == nil || repoID == "" || workloadID == "" {
		return nil, nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	// WorkloadInstance and USES relationships are global today and do not
	// carry repository ownership, so scoped callers cannot safely consume them.
	if access.Scoped() {
		return nil, nil
	}
	if limit <= 0 || limit > ServiceCloudResourceDependencyLimit {
		limit = ServiceCloudResourceDependencyLimit
	}
	params := access.GraphParams(map[string]any{
		"repo_id":     repoID,
		"workload_id": workloadID,
		"limit":       limit,
	})
	rows, err := graph.Run(ctx, fmt.Sprintf(`
MATCH (repo:Repository)-[:DEFINES]->(workload:Workload {id: $workload_id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)-[rel:USES]->(c:CloudResource)
WHERE repo.id = $repo_id%s
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
LIMIT $limit`, access.GraphPredicate("repo")), params)
	if err != nil {
		return nil, err
	}
	resources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		resource := querycontract.CompactStringMap(map[string]any{
			"id":                    querycontract.StringVal(row, "id"),
			"name":                  querycontract.StringVal(row, "name"),
			"kind":                  querycontract.StringVal(row, "kind"),
			"resource_type":         querycontract.StringVal(row, "resource_type"),
			"provider":              querycontract.StringVal(row, "provider"),
			"environment":           querycontract.StringVal(row, "environment"),
			"resource_id":           querycontract.StringVal(row, "resource_id"),
			"arn":                   querycontract.StringVal(row, "arn"),
			"account_id":            querycontract.StringVal(row, "account_id"),
			"region":                querycontract.StringVal(row, "region"),
			"relationship_basis":    querycontract.StringVal(row, "relationship_basis"),
			"resolution_mode":       querycontract.StringVal(row, "resolution_mode"),
			"evidence_source":       querycontract.StringVal(row, "evidence_source"),
			"service_anchor_source": querycontract.StringVal(row, "service_anchor_source"),
			"service_anchor_reason": querycontract.StringVal(row, "service_anchor_reason"),
			"source_fact_id":        querycontract.StringVal(row, "source_fact_id"),
			"stable_fact_key":       querycontract.StringVal(row, "stable_fact_key"),
			"source_system":         querycontract.StringVal(row, "source_system"),
			"source_record_id":      querycontract.StringVal(row, "source_record_id"),
			"collector_kind":        querycontract.StringVal(row, "collector_kind"),
		})
		if len(resource) > 0 {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func LoadConfigDerivedCloudResourceDependencies(
	ctx context.Context,
	graph querycontract.GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, error) {
	resources, _, err := LoadConfigDerivedCloudResourceDependenciesBounded(
		ctx,
		graph,
		deploymentEvidence,
		limit,
	)
	return resources, err
}

func LoadConfigDerivedCloudResourceDependenciesBounded(
	ctx context.Context,
	graph querycontract.GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, bool, error) {
	if limit <= 0 || limit > ServiceCloudResourceDependencyLimit {
		limit = ServiceCloudResourceDependencyLimit
	}
	resources, querySaturated, err := loadConfigDerivedCloudResourceDependenciesWithLimit(
		ctx,
		graph,
		deploymentEvidence,
		limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	resources, truncated := querycontract.CapMapRows(resources, limit)
	return resources, truncated || querySaturated, nil
}

func loadConfigDerivedCloudResourceDependenciesWithLimit(
	ctx context.Context,
	graph querycontract.GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, bool, error) {
	if graph == nil || len(deploymentEvidence) == 0 {
		return nil, false, nil
	}
	// CloudResource nodes do not carry repository ownership. A config-text
	// match is only a candidate, so a scoped token cannot safely authorize it.
	if querycontract.RepositoryAccessFilterFromContext(ctx).Scoped() {
		return nil, false, nil
	}
	anchors, anchorsTruncated := ConfigReadCloudResourceAnchors(deploymentEvidence)
	if len(anchors) == 0 {
		return nil, anchorsTruncated, nil
	}
	anchorPattern := configReadCloudResourceAnchorPattern(anchors)
	rows, err := graph.Run(ctx, `
MATCH (c:CloudResource)
WHERE coalesce(c.name, '') =~ $config_anchor_pattern
   OR coalesce(c.config_path, '') =~ $config_anchor_pattern
   OR coalesce(c.resource_id, '') =~ $config_anchor_pattern
   OR coalesce(c.arn, '') =~ $config_anchor_pattern
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
       coalesce(c.config_path, '') AS config_path
ORDER BY name, id
LIMIT $limit`, map[string]any{
		"config_anchor_pattern": anchorPattern,
		"limit":                 limit,
	})
	if err != nil {
		return nil, false, err
	}
	resources := make([]map[string]any, 0, min(len(rows), limit))
	seen := make(map[string]struct{}, limit)
	for _, row := range rows {
		anchor := matchingConfigReadCloudResourceAnchor(row, anchors)
		if anchor == "" {
			continue
		}
		resource := querycontract.CompactStringMap(map[string]any{
			"id":                 querycontract.StringVal(row, "id"),
			"name":               querycontract.StringVal(row, "name"),
			"kind":               querycontract.StringVal(row, "kind"),
			"resource_type":      querycontract.StringVal(row, "resource_type"),
			"provider":           querycontract.StringVal(row, "provider"),
			"environment":        querycontract.StringVal(row, "environment"),
			"resource_id":        querycontract.StringVal(row, "resource_id"),
			"arn":                querycontract.StringVal(row, "arn"),
			"account_id":         querycontract.StringVal(row, "account_id"),
			"region":             querycontract.StringVal(row, "region"),
			"relationship_basis": "deployment_config_read_evidence",
			"evidence_source":    "deployment_evidence",
			"matched_value":      anchor,
		})
		key := serviceCloudResourceRowKey(resource)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		resources = append(resources, resource)
	}
	return resources, anchorsTruncated || len(rows) >= limit, nil
}

func configReadCloudResourceAnchorPattern(anchors []string) string {
	escaped := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		escaped = append(escaped, regexp.QuoteMeta(anchor))
	}
	return ".*(?:" + strings.Join(escaped, "|") + ").*"
}

func matchingConfigReadCloudResourceAnchor(row map[string]any, anchors []string) string {
	for _, anchor := range anchors {
		for _, field := range []string{"name", "config_path", "resource_id", "arn"} {
			if strings.Contains(querycontract.StringVal(row, field), anchor) {
				return anchor
			}
		}
	}
	return ""
}

func ConfigReadCloudResourceAnchors(deploymentEvidence map[string]any) ([]string, bool) {
	seen := map[string]struct{}{}
	var anchors []string
	truncated := querycontract.BoolVal(deploymentEvidence, "artifacts_truncated")
	for _, artifact := range querycontract.MapSliceValue(deploymentEvidence, "artifacts") {
		if strings.TrimSpace(querycontract.StringVal(artifact, "relationship_type")) != "READS_CONFIG_FROM" {
			continue
		}
		anchor := normalizeConfigReadCloudResourceAnchor(querycontract.StringVal(artifact, "matched_value"))
		if anchor == "" {
			continue
		}
		if _, ok := seen[anchor]; ok {
			continue
		}
		if len(anchors) >= ServiceCloudResourceDependencyLimit {
			truncated = true
			continue
		}
		seen[anchor] = struct{}{}
		anchors = append(anchors, anchor)
	}
	return anchors, truncated
}

func normalizeConfigReadCloudResourceAnchor(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "*")
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "/")
	if len(value) < 3 {
		return ""
	}
	return value
}

func serviceCloudResourceRowKey(row map[string]any) string {
	return querycontract.FirstNonEmptyString(querycontract.StringVal(row, "id"), querycontract.StringVal(row, "resource_id"), querycontract.StringVal(row, "arn"), querycontract.StringVal(row, "name"))
}
