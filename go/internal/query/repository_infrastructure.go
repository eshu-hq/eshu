// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
)

const repositoryInfrastructureEntityLimit = 5000

// repositoryInfrastructureEntityTypes is the canonical content_entities
// entity_type set for the repository infrastructure panel: Kubernetes,
// Terraform, Terragrunt, ArgoCD, Helm, Kustomize, Crossplane, and
// CloudFormation entity types. It is the single source of truth for both the
// type-filtered content read (queryRepoInfrastructureFromContent, via
// ListRepoEntitiesByTypes) and the defensive graph-response gate
// (isRepositoryInfrastructureType) so the two lists cannot drift apart --
// they drove the #5764 P1 review defect below when a caller filtered on one
// set but bounded on another. repositoryInfrastructureEntryFromContent's
// switch must keep classifying exactly this set: a type present here but
// missing from that switch would fetch rows the content path then silently
// drops (ok=false), narrowing the panel without a truncation signal to
// explain why.
var repositoryInfrastructureEntityTypes = []string{
	"K8sResource", "TerraformResource", "TerraformModule", "TerraformDataSource",
	"TerraformBackend", "TerraformImport", "TerraformMovedBlock", "TerraformRemovedBlock",
	"TerraformCheck", "TerraformLockProvider",
	"TerragruntConfig", "TerragruntDependency",
	"ArgoCDApplication", "ArgoCDApplicationSet",
	"HelmChart", "HelmValues", "KustomizeOverlay",
	"CrossplaneXRD", "CrossplaneComposition",
	"CloudFormationResource",
}

// queryRepoInfrastructureRows prefers the content read model and falls back to
// a graph read. A graph-read error is returned to the caller rather than
// swallowed: infrastructure is a genuine auxiliary panel (#5764), so callers
// keep answering 200 on this error, but they attribute the degradation
// instead of silently returning an empty list indistinguishable from "no
// infrastructure detected". The returned bool reports whether EITHER the
// content read or the graph read hit its LIMIT bound (P2-3 follow-up): the
// content path is tried first on a normally-wired deployment, so a truncation
// signal that fired only on the graph fallback would never disclose the
// common case where content itself clipped at
// repositoryInfrastructureEntityLimit INFRASTRUCTURE-TYPED entities (P1
// review follow-up: the bound is on the type-filtered set returned by
// ListRepoEntitiesByTypes, not the repo's total content-entity count of any
// type -- see queryRepoInfrastructureFromContent).
func queryRepoInfrastructureRows(
	ctx context.Context,
	reader GraphQuery,
	content ContentStore,
	params map[string]any,
) ([]map[string]any, bool, error) {
	repoID := StringVal(params, "repo_id")
	if content != nil && repoID != "" {
		rows, truncated := queryRepoInfrastructureFromContent(ctx, content, repoID)
		if len(rows) > 0 {
			return rows, truncated, nil
		}
		// truncated deliberately does not survive this block (round-7 P3
		// review finding, answered rather than changed). It describes the
		// content read's own 5001-row window, and when that read produced no
		// panel rows the rows this function goes on to return come from the
		// graph fallback below instead. Carrying a content-side bound onto a
		// graph-sourced panel would report "more rows exist past the bound"
		// about a bound those rows were never subject to; the graph read
		// reports its own. The only way to reach this line with truncated=true
		// is a content read that filled its window with rows
		// repositoryInfrastructureEntryFromContent then dropped -- the type-list
		// drift the canonical-list doc comment above warns about, which
		// TestRepositoryInfrastructureEntryFromContentClassifiesEveryCanonicalType
		// now fails on directly instead of leaving it to surface as a
		// misattributed truncation signal.
	}

	return queryRepoInfrastructureFromGraph(ctx, reader, params)
}

// queryRepoInfrastructureFromContent uses the content read model as the
// preferred source when parsed infrastructure entities are present. It
// requests repositoryInfrastructureEntityLimit+1 rows FILTERED to
// repositoryInfrastructureEntityTypes (via ListRepoEntitiesByTypes) and caps
// in Go (P2-3 follow-up to #5764, mirroring queryRepoInfrastructureFromGraph's
// own limit+1 idiom below) so the returned bool reports EXACT truncation of
// the INFRASTRUCTURE-TYPED set (len(entities) > limit) instead of silently
// discarding whether more infrastructure entities exist past the bound.
//
// P1 review follow-up: this used to call the untyped ListRepoEntities, which
// bounds and reports truncation on the repo's TOTAL content-entity count
// (functions, classes, structs -- every parsed entity), not the
// infrastructure-typed subset. That inverted the defect #5764 exists to
// remove: "truncated" meant "this repo has more than 5000 content entities of
// ANY type" (true for nearly every real repository), not "the infrastructure
// panel was clipped," while every infrastructure entity was actually present
// and undisclosed as complete. It also meant the 5001-row window, sorted
// relative_path/start_line across ALL types, could silently drop
// infrastructure entities in late-sorting paths before this function's own
// type filter ever saw them (the exact hazard content_reader_by_type.go
// documents and ListRepoEntitiesByType/fetchK8sResourceCandidates exist to
// avoid). Filtering by type in the query itself, not just in the
// classification loop below, closes both holes: the LIMIT now bounds only
// rows this function can actually turn into an infrastructure entry.
func queryRepoInfrastructureFromContent(ctx context.Context, content ContentStore, repoID string) ([]map[string]any, bool) {
	entities, err := content.ListRepoEntitiesByTypes(ctx, repoID, repositoryInfrastructureEntityTypes, repositoryInfrastructureEntityLimit+1)
	if err != nil || len(entities) == 0 {
		return nil, false
	}
	truncated := len(entities) > repositoryInfrastructureEntityLimit
	if truncated {
		entities = entities[:repositoryInfrastructureEntityLimit]
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

	return result, truncated
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
// this same file is bounded at the same constant and, like this graph
// fallback, discloses truncation via its own returned bool
// (queryRepoInfrastructureFromContent, P2-3 follow-up) rather than silently
// truncating, so a HEALTHY read that lands past the bound (more rows exist
// beyond it) is disclosed on either path. The
// read requests repositoryInfrastructureEntityLimit+1 rows (P3 review
// follow-up, matching repository_deployment_evidence.go's
// queryRepoDeploymentEvidenceDirection idiom) so the returned bool reports
// EXACT truncation (len(rows) > limit) instead of the ambiguous
// len(rows) == limit check, which cannot distinguish "exactly limit entities
// exist" from "more entities exist past the bound." Rows past the limit are
// capped in Go before the response is built, so a caller can distinguish
// "every infrastructure entity is present" from "there may be more past the
// bound" instead of the two looking identical (P2-2 follow-up).
//
// Queryplan classification (round-7 P3 review finding, answered here because
// query-source-coverage.yaml carries no free-text field and no comments): this
// callsite is registered `non_hot: {class: keyed_support, key_bound:
// single_key, max_results: 5001}`. That disposition asserts exactly two things
// and no more -- the read is anchored on ONE caller-supplied key ($repo_id,
// via the `MATCH (r:Repository {id: $repo_id})` anchor, so it can never fan
// out across repositories) and it is capped at a declared row count. It
// asserts NOTHING about the plan, operator count, or latency of the two-hop
// REPO_CONTAINS -> CONTAINS expand or the 20-way OR label predicate applied
// after it. No closed non-hot class carries a plan assertion, so picking a
// different one would add no evidence, and none of the others fits anyway:
// `label_inventory` wants a single anchoring label (this read is
// repository-anchored, not a scan of one label), `delegated` wants another
// registered entry to own the plan, `operator_query` is for admin surfaces,
// and `backend_metadata` is for backend introspection. The one genuinely
// stronger designation is promotion to a hot `entry_ids` queryplan entry with
// plan assertions, which needs a measured plan on a real corpus -- evidence
// this change does not have and therefore does not claim. Until it exists the
// honest statement is the one this comment already makes on other grounds:
// this is the read most likely to hit the graph-read deadline, which is
// exactly why every caller degrades to an attributed 200 rather than depending
// on it succeeding.
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

// repositoryInfrastructureTypeSet backs isRepositoryInfrastructureType with a
// membership set built once from the single canonical
// repositoryInfrastructureEntityTypes list, so the graph-response gate can
// never drift from the content-path type filter.
var repositoryInfrastructureTypeSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(repositoryInfrastructureEntityTypes))
	for _, entityType := range repositoryInfrastructureEntityTypes {
		set[entityType] = struct{}{}
	}
	return set
}()

// isRepositoryInfrastructureType is a defensive response gate for backends that
// may over-return rows for OR-heavy label predicates.
func isRepositoryInfrastructureType(entityType string) bool {
	_, ok := repositoryInfrastructureTypeSet[entityType]
	return ok
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
