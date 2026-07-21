// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const serviceCloudResourceDependencyLimit = serviceStoryItemLimit

func loadMaterializedServiceCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	repoID string,
	workloadID string,
	limit int,
) ([]map[string]any, error) {
	repoID = strings.TrimSpace(repoID)
	workloadID = strings.TrimSpace(workloadID)
	if graph == nil || repoID == "" || workloadID == "" {
		return nil, nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	// WorkloadInstance and USES relationships are global today and do not
	// carry repository ownership, so scoped callers cannot safely consume them.
	if access.scoped() {
		return nil, nil
	}
	if limit <= 0 || limit > serviceCloudResourceDependencyLimit {
		limit = serviceCloudResourceDependencyLimit
	}
	params := access.graphParams(map[string]any{
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
LIMIT $limit`, access.graphPredicate("repo")), params)
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

func loadConfigDerivedCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, error) {
	resources, _, err := loadConfigDerivedCloudResourceDependenciesBounded(
		ctx,
		graph,
		deploymentEvidence,
		limit,
	)
	return resources, err
}

func loadConfigDerivedCloudResourceDependenciesBounded(
	ctx context.Context,
	graph GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, bool, error) {
	if limit <= 0 || limit > serviceCloudResourceDependencyLimit {
		limit = serviceCloudResourceDependencyLimit
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
	resources, truncated := capMapRows(resources, limit)
	return resources, truncated || querySaturated, nil
}

func loadConfigDerivedCloudResourceDependenciesWithLimit(
	ctx context.Context,
	graph GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, bool, error) {
	if graph == nil || len(deploymentEvidence) == 0 {
		return nil, false, nil
	}
	// CloudResource nodes do not carry repository ownership. A config-text
	// match is only a candidate, so a scoped token cannot safely authorize it.
	if repositoryAccessFilterFromContext(ctx).scoped() {
		return nil, false, nil
	}
	anchors, anchorsTruncated := configReadCloudResourceAnchors(deploymentEvidence)
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
		resource := compactStringMap(map[string]any{
			"id":                 StringVal(row, "id"),
			"name":               StringVal(row, "name"),
			"kind":               StringVal(row, "kind"),
			"resource_type":      StringVal(row, "resource_type"),
			"provider":           StringVal(row, "provider"),
			"environment":        StringVal(row, "environment"),
			"resource_id":        StringVal(row, "resource_id"),
			"arn":                StringVal(row, "arn"),
			"account_id":         StringVal(row, "account_id"),
			"region":             StringVal(row, "region"),
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
			if strings.Contains(StringVal(row, field), anchor) {
				return anchor
			}
		}
	}
	return ""
}

func configReadCloudResourceAnchors(deploymentEvidence map[string]any) ([]string, bool) {
	seen := map[string]struct{}{}
	var anchors []string
	truncated := BoolVal(deploymentEvidence, "artifacts_truncated")
	for _, artifact := range mapSliceValue(deploymentEvidence, "artifacts") {
		if strings.TrimSpace(StringVal(artifact, "relationship_type")) != "READS_CONFIG_FROM" {
			continue
		}
		anchor := normalizeConfigReadCloudResourceAnchor(StringVal(artifact, "matched_value"))
		if anchor == "" {
			continue
		}
		if _, ok := seen[anchor]; ok {
			continue
		}
		if len(anchors) >= serviceCloudResourceDependencyLimit {
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
	return firstNonEmptyString(StringVal(row, "id"), StringVal(row, "resource_id"), StringVal(row, "arn"), StringVal(row, "name"))
}
