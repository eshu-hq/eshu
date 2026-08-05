// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
)

const repositoryInfrastructureEntityLimit = 5000

// queryRepoInfrastructureRows prefers the content read model and falls back to
// a graph read. A graph-read error is returned to the caller rather than
// swallowed: infrastructure is a genuine auxiliary panel (#5764), so callers
// keep answering 200 on this error, but they attribute the degradation
// instead of silently returning an empty list indistinguishable from "no
// infrastructure detected". The returned bool reports whether the GRAPH read
// hit its LIMIT bound (P2-2 follow-up); the content path's own silent
// truncation at the same constant is pre-existing, already-shipped behavior
// and is not signaled here.
func queryRepoInfrastructureRows(
	ctx context.Context,
	reader GraphQuery,
	content ContentStore,
	params map[string]any,
) ([]map[string]any, bool, error) {
	repoID := StringVal(params, "repo_id")
	if content != nil && repoID != "" {
		if rows := queryRepoInfrastructureFromContent(ctx, content, repoID); len(rows) > 0 {
			return rows, false, nil
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

// queryRepoInfrastructureFromGraph returns an error rather than silently
// returning an empty result when the graph read itself fails (#5764) -- the
// prior `err != nil || len(rows) == 0` swallow made a deadlined/unavailable
// read indistinguishable from a repository that genuinely has no
// infrastructure. Callers attribute this error (limitations / stage-log
// failure_class) and still answer 200, because infrastructure is a genuine
// auxiliary panel and this OR-heavy label scan is the read most likely to hit
// the graph-read deadline. The read is bounded at
// repositoryInfrastructureEntityLimit (5000): the preferred content path in
// this same file already truncates silently at this exact constant
// (queryRepoInfrastructureFromContent's ListRepoEntities call above), so
// capping the graph fallback here is parity with already-shipped behavior in
// terms of the bound's existence -- but unlike that pre-existing content-path
// truncation, a graph read that lands past the bound (more rows exist beyond
// it) is disclosed. The
// read requests repositoryInfrastructureEntityLimit+1 rows (P3 review
// follow-up, matching repository_deployment_evidence.go's
// queryRepoDeploymentEvidenceDirection idiom) so the returned bool reports
// EXACT truncation (len(rows) > limit) instead of the ambiguous
// len(rows) == limit check, which cannot distinguish "exactly limit entities
// exist" from "more entities exist past the bound." Rows past the limit are
// capped in Go before the response is built, so a caller can distinguish
// "every infrastructure entity is present" from "there may be more past the
// bound" instead of the two looking identical (P2-2 follow-up).
func queryRepoInfrastructureFromGraph(ctx context.Context, reader GraphQuery, params map[string]any) ([]map[string]any, bool, error) {
	// infra:K8sResource also covers Crossplane Claims: a Claim is edge-only
	// (issue #5347) and stays a K8sResource node, so a separate
	// infra:CrossplaneClaim predicate would always match zero rows and is
	// intentionally absent (issue #5478).
	queryParams := copyMap(params)
	queryParams["limit"] = repositoryInfrastructureEntityLimit + 1
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(infra)
		WHERE infra:K8sResource OR infra:TerraformResource OR infra:TerraformModule
		      OR infra:TerraformDataSource
		      OR infra:TerraformBackend OR infra:TerraformImport
		      OR infra:TerraformMovedBlock OR infra:TerraformRemovedBlock
		      OR infra:TerraformCheck OR infra:TerraformLockProvider
		      OR infra:TerragruntConfig OR infra:TerragruntDependency
		      OR infra:ArgoCDApplication OR infra:ArgoCDApplicationSet
		      OR infra:HelmChart OR infra:HelmValues
		      OR infra:KustomizeOverlay
		      OR infra:CrossplaneXRD OR infra:CrossplaneComposition
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
		LIMIT $limit
	`, queryParams)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > repositoryInfrastructureEntityLimit
	if truncated {
		rows = rows[:repositoryInfrastructureEntityLimit]
	}
	if len(rows) == 0 {
		return make([]map[string]any, 0), truncated, nil
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := repositoryInfrastructureEntryFromRow(row)
		if !isRepositoryInfrastructureType(StringVal(entry, "type")) {
			continue
		}
		result = append(result, entry)
	}
	return result, truncated, nil
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
		"HelmValues", "CrossplaneXRD", "CrossplaneComposition",
		"CloudFormationResource", "K8sResource", "TerraformResource", "TerraformDataSource",
		"TerraformBackend", "TerraformImport", "TerraformMovedBlock", "TerraformRemovedBlock",
		"TerraformCheck", "TerraformLockProvider":
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
		"TerraformBackend", "TerraformImport", "TerraformMovedBlock", "TerraformRemovedBlock",
		"TerraformCheck", "TerraformLockProvider",
		"TerragruntConfig", "TerragruntDependency",
		"ArgoCDApplication", "ArgoCDApplicationSet",
		"HelmChart", "HelmValues", "KustomizeOverlay",
		"CrossplaneXRD", "CrossplaneComposition",
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
