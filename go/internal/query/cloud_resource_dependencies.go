package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const serviceCloudResourceDependencyLimit = serviceStoryItemLimit
const maxServiceCloudResourceConfigRefs = serviceStoryItemLimit

func loadServiceCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	workloadID string,
	serviceName string,
	configRefs []string,
	limit int,
) ([]map[string]any, error) {
	workloadID = strings.TrimSpace(workloadID)
	serviceName = strings.TrimSpace(serviceName)
	configRefs = normalizeServiceCloudResourceConfigRefs(configRefs)
	if graph == nil || (workloadID == "" && serviceName == "" && len(configRefs) == 0) {
		return nil, nil
	}
	if limit <= 0 || limit > serviceCloudResourceDependencyLimit {
		limit = serviceCloudResourceDependencyLimit
	}
	params := map[string]any{
		"workload_id":   workloadID,
		"service_name":  serviceName,
		"service_token": strings.ToLower(serviceName),
		"limit":         limit,
	}
	configPredicate := serviceCloudResourceConfigPredicate(configRefs, params)
	configClause := ""
	if configPredicate != "" {
		configClause = "\n  OR (" + configPredicate + ")"
	}
	rows, err := graph.Run(ctx, `
MATCH (c:CloudResource)
WHERE (
  c.service_anchor_status = 'strong'
    AND (
      ($workload_id <> '' AND coalesce(c.workload_id, '') = $workload_id) OR
      ($service_name <> '' AND toLower(coalesce(c.service_name, '')) = $service_token)
    )
)`+configClause+`
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
LIMIT $limit`, params)
	if err != nil {
		return nil, err
	}
	resources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		basis := "aws_resource_service_anchor"
		anchorStatus := StringVal(row, "service_anchor_status")
		anchorSource := StringVal(row, "service_anchor_source")
		anchorReason := StringVal(row, "service_anchor_reason")
		if anchorStatus != "strong" {
			basis = "deployment_config_read_evidence"
			anchorStatus = "strong"
			anchorSource = "deployment_evidence.reads_config_from"
			anchorReason = "config_resource_identity_match"
		}
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
			"relationship_basis":    basis,
			"service_anchor_status": anchorStatus,
			"service_anchor_source": anchorSource,
			"service_anchor_reason": anchorReason,
		})
		if len(resource) > 0 {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func serviceCloudResourceConfigRefs(workloadContext map[string]any) []string {
	refs := make([]string, 0)
	for _, artifact := range mapSliceValue(mapValue(workloadContext, "deployment_evidence"), "artifacts") {
		if StringVal(artifact, "relationship_type") != "READS_CONFIG_FROM" {
			continue
		}
		for _, key := range []string{"matched_value", "config_path", "resource_id", "arn"} {
			if value := strings.TrimSpace(StringVal(artifact, key)); value != "" {
				refs = append(refs, value)
			}
		}
	}
	return normalizeServiceCloudResourceConfigRefs(refs)
}

func normalizeServiceCloudResourceConfigRefs(refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = normalizeServiceCloudResourceConfigRef(ref)
		if !usableServiceCloudResourceConfigRef(ref) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	if len(out) > maxServiceCloudResourceConfigRefs {
		return out[:maxServiceCloudResourceConfigRefs]
	}
	return out
}

func normalizeServiceCloudResourceConfigRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for strings.HasSuffix(ref, "*") {
		ref = strings.TrimSpace(strings.TrimSuffix(ref, "*"))
	}
	return ref
}

func usableServiceCloudResourceConfigRef(ref string) bool {
	if len(ref) < 4 {
		return false
	}
	return strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "arn:")
}

func serviceCloudResourceConfigPredicate(refs []string, params map[string]any) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for i, ref := range refs {
		key := fmt.Sprintf("config_ref_%d", i)
		params[key] = ref
		parts = append(parts, "(coalesce(c.arn, '') CONTAINS $"+key+" OR coalesce(c.resource_id, '') CONTAINS $"+key+")")
	}
	return "(coalesce(c.resource_type, c.data_type, '') = 'aws_ssm_parameter' OR coalesce(c.resource_type, c.data_type, '') = 'aws_secretsmanager_secret') AND (" +
		strings.Join(parts, " OR ") + ")"
}
