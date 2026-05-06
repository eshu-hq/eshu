package query

import (
	"context"
	"sort"
)

const repositoryInfrastructureEntityLimit = 5000

func queryRepoInfrastructureRows(
	ctx context.Context,
	reader GraphQuery,
	content ContentStore,
	params map[string]any,
) []map[string]any {
	repoID := StringVal(params, "repo_id")
	if content != nil && repoID != "" {
		if rows := queryRepoInfrastructureFromContent(ctx, content, repoID); len(rows) > 0 {
			return rows
		}
	}

	return queryRepoInfrastructureFromGraph(ctx, reader, params)
}

// queryRepoInfrastructureFromContent uses the content read model as the
// preferred source when parsed infrastructure entities are present.
func queryRepoInfrastructureFromContent(ctx context.Context, content ContentStore, repoID string) []map[string]any {
	entities, err := content.ListRepoEntities(ctx, repoID, repositoryInfrastructureEntityLimit)
	if err != nil || len(entities) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(entities))
	seen := make(map[string]struct{}, len(entities))
	for _, entity := range entities {
		entry, ok := repositoryInfrastructureEntryFromContent(entity)
		if !ok {
			continue
		}
		key := repositoryInfrastructureEntryKey(entry)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entry)
	}

	sort.SliceStable(result, func(i, j int) bool {
		leftType := StringVal(result[i], "type")
		rightType := StringVal(result[j], "type")
		if leftType != rightType {
			return leftType < rightType
		}
		leftName := StringVal(result[i], "name")
		rightName := StringVal(result[j], "name")
		if leftName != rightName {
			return leftName < rightName
		}
		return StringVal(result[i], "file_path") < StringVal(result[j], "file_path")
	})

	return result
}

func queryRepoInfrastructureFromGraph(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(infra)
		WHERE infra:K8sResource OR infra:TerraformResource OR infra:TerraformModule
		      OR infra:TerraformDataSource
		      OR infra:TerragruntConfig OR infra:TerragruntDependency
		      OR infra:ArgoCDApplication OR infra:ArgoCDApplicationSet
		      OR infra:HelmChart OR infra:HelmValues
		      OR infra:KustomizeOverlay
		      OR infra:CrossplaneXRD OR infra:CrossplaneComposition OR infra:CrossplaneClaim
		      OR infra:CloudFormationResource
		RETURN labels(infra)[0] AS type, infra.name AS name,
		       infra.kind AS kind, infra.source AS source,
		       infra.terraform_source AS terraform_source,
		       infra.config_path AS config_path,
		       infra.provider AS provider,
		       coalesce(infra.resource_type, infra.data_type, '') AS resource_type,
		       infra.resource_service AS resource_service,
		       infra.resource_category AS resource_category,
		       f.relative_path AS file_path
		ORDER BY type, name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := repositoryInfrastructureEntryFromRow(row)
		if !isRepositoryInfrastructureType(StringVal(entry, "type")) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func repositoryInfrastructureEntryFromRow(row map[string]any) map[string]any {
	entry := map[string]any{
		"type":      StringVal(row, "type"),
		"name":      StringVal(row, "name"),
		"file_path": StringVal(row, "file_path"),
	}
	if kind := StringVal(row, "kind"); kind != "" {
		entry["kind"] = kind
	}
	if source := StringVal(row, "source"); source != "" {
		entry["source"] = source
	}
	if terraformSource := StringVal(row, "terraform_source"); terraformSource != "" {
		entry["terraform_source"] = terraformSource
	}
	if configPath := StringVal(row, "config_path"); configPath != "" {
		entry["config_path"] = configPath
	}
	copyInfrastructureClassification(entry, row)
	return entry
}

func repositoryInfrastructureEntryFromContent(entity EntityContent) (map[string]any, bool) {
	entry := map[string]any{
		"type":      entity.EntityType,
		"name":      entity.EntityName,
		"file_path": entity.RelativePath,
	}
	switch entity.EntityType {
	case "TerraformModule":
		if source, ok := metadataNonEmptyString(entity.Metadata, "source"); ok {
			entry["source"] = source
		}
		if deploymentName, ok := metadataNonEmptyString(entity.Metadata, "deployment_name"); ok {
			entry["deployment_name"] = deploymentName
		}
	case "TerragruntConfig":
		if terraformSource, ok := metadataNonEmptyString(entity.Metadata, "terraform_source"); ok {
			entry["terraform_source"] = terraformSource
			entry["source"] = terraformSource
		}
		if includes := metadataStringSlice(entity.Metadata, "includes"); len(includes) > 0 {
			entry["includes"] = includes
		}
		if inputs := metadataStringSlice(entity.Metadata, "inputs"); len(inputs) > 0 {
			entry["inputs"] = inputs
		}
		if locals := metadataStringSlice(entity.Metadata, "locals"); len(locals) > 0 {
			entry["locals"] = locals
		}
	case "TerragruntDependency":
		if configPath, ok := metadataNonEmptyString(entity.Metadata, "config_path"); ok {
			entry["config_path"] = configPath
		}
	case "ArgoCDApplication", "ArgoCDApplicationSet", "KustomizeOverlay", "HelmChart",
		"HelmValues", "CrossplaneXRD", "CrossplaneComposition", "CrossplaneClaim",
		"CloudFormationResource", "K8sResource", "TerraformResource", "TerraformDataSource":
		if source, ok := metadataNonEmptyString(entity.Metadata, "source"); ok {
			entry["source"] = source
		}
		if kind, ok := metadataNonEmptyString(entity.Metadata, "kind"); ok {
			entry["kind"] = kind
		}
		copyInfrastructureClassification(entry, entity.Metadata)
	default:
		return nil, false
	}
	return entry, true
}

// isRepositoryInfrastructureType is a defensive response gate for backends that
// may over-return rows for OR-heavy label predicates.
func isRepositoryInfrastructureType(entityType string) bool {
	switch entityType {
	case "K8sResource", "TerraformResource", "TerraformModule", "TerraformDataSource",
		"TerragruntConfig", "TerragruntDependency",
		"ArgoCDApplication", "ArgoCDApplicationSet",
		"HelmChart", "HelmValues", "KustomizeOverlay",
		"CrossplaneXRD", "CrossplaneComposition", "CrossplaneClaim",
		"CloudFormationResource":
		return true
	default:
		return false
	}
}

func copyInfrastructureClassification(entry map[string]any, source map[string]any) {
	for _, key := range []string{"provider", "resource_type", "resource_service", "resource_category"} {
		if value := StringVal(source, key); value != "" {
			entry[key] = value
		}
	}
}

func repositoryInfrastructureEntryKey(entry map[string]any) string {
	return StringVal(entry, "type") + "|" + StringVal(entry, "name") + "|" + StringVal(entry, "file_path")
}
