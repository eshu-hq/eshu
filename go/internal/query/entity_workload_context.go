package query

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// fetchWorkloadContext queries graph-backed workload context with a custom
// WHERE clause and enriches linked repositories with local context evidence.
func (h *EntityHandler) fetchWorkloadContext(ctx context.Context, whereClause string, params map[string]any) (map[string]any, error) {
	return h.fetchWorkloadContextForOperation(ctx, whereClause, params, "workload_context")
}

// fetchServiceWorkloadContext avoids a backend-sensitive OR predicate by
// trying exact service-name lookup before exact workload-id lookup.
func (h *EntityHandler) fetchServiceWorkloadContext(ctx context.Context, serviceName string, operation string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, nil
	}
	result, err := h.fetchWorkloadContextForOperation(
		ctx,
		"w.name = $service_name",
		map[string]any{"service_name": serviceName},
		operation,
	)
	if err != nil || result != nil {
		return result, err
	}
	result, err = h.fetchWorkloadContextForOperation(
		ctx,
		"w.id = $service_name",
		map[string]any{"service_name": serviceName},
		operation,
	)
	if err != nil || result != nil {
		return result, err
	}
	return h.fetchServiceReadModelWorkloadContext(ctx, serviceName)
}

// fetchWorkloadContextForOperation queries workload context and tags timing
// logs with the caller operation that will render the context.
func (h *EntityHandler) fetchWorkloadContextForOperation(ctx context.Context, whereClause string, params map[string]any, operation string) (map[string]any, error) {
	serviceName := StringVal(params, "service_name")
	if serviceName == "" {
		serviceName = StringVal(params, "workload_id")
	}
	if operation == "" {
		operation = "workload_context"
	}
	timer := startServiceQueryStage(ctx, h.Logger, operation, serviceName, "", "workload_lookup")
	baseCypher := fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
		LIMIT 1
	`, whereClause)

	row, err := h.Neo4j.RunSingle(ctx, baseCypher, params)
	timer.Done(ctx, slog.Bool("found", row != nil))
	if err != nil {
		return nil, err
	}

	if row == nil {
		return nil, nil
	}

	followupWhereClause := whereClause
	followupParams := params
	if workloadID := StringVal(row, "id"); workloadID != "" {
		followupWhereClause = "w.id = $workload_id"
		followupParams = map[string]any{"workload_id": workloadID}
	}

	repoID := StringVal(row, "repo_id")
	timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repository_lookup")
	repoName := ""
	if repoID != "" {
		repoName, err = h.fetchRepositoryNameByID(ctx, repoID)
	} else {
		repoID, repoName, err = h.fetchWorkloadRepository(ctx, followupWhereClause, followupParams)
	}
	timer.Done(ctx, slog.String("resolved_repo_id", repoID))
	if err != nil {
		return nil, err
	}
	if repoName == "" {
		repoName = StringVal(row, "repo_name")
	}

	timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "instance_lookup")
	instances, err := h.fetchWorkloadInstances(ctx, followupWhereClause, followupParams, repoID)
	timer.Done(ctx, slog.Int("row_count", len(instances)))
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		instances = extractInstances(row)
	}

	result := map[string]any{
		"id":        StringVal(row, "id"),
		"name":      StringVal(row, "name"),
		"kind":      StringVal(row, "kind"),
		"repo_id":   repoID,
		"repo_name": repoName,
		"instances": instances,
	}

	if repoID != "" {
		repoParams := map[string]any{"repo_id": repoID}
		timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repo_dependencies")
		result["dependencies"] = queryRepoDependencies(ctx, h.Neo4j, repoParams)
		timer.Done(ctx, slog.Int("row_count", len(mapSliceValue(result, "dependencies"))))
		timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repo_infrastructure")
		result["infrastructure"] = queryRepoInfrastructure(ctx, h.Neo4j, h.Content, repoParams)
		timer.Done(ctx, slog.Int("row_count", len(mapSliceValue(result, "infrastructure"))))
	}

	return result, nil
}

// fetchServiceReadModelWorkloadContext exposes repositories with workload
// identity facts even when no graph Workload node has been materialized yet.
func (h *EntityHandler) fetchServiceReadModelWorkloadContext(ctx context.Context, serviceName string) (map[string]any, error) {
	if h.Content == nil {
		return nil, nil
	}
	repo, err := h.Content.ResolveRepository(ctx, serviceName)
	if err != nil || repo == nil {
		return nil, err
	}

	summary := loadRepositoryReadModelSummary(ctx, h.Content, repo.ID)
	if summary == nil {
		return nil, nil
	}
	workloadName := matchingRepositoryWorkloadIdentity(serviceName, *repo, summary.WorkloadNames)
	if workloadName == "" {
		return nil, nil
	}

	repoParams := map[string]any{"repo_id": repo.ID}
	infrastructure := queryRepoInfrastructureFromContent(ctx, h.Content, repo.ID)
	if len(infrastructure) == 0 && h.Neo4j != nil {
		infrastructure = queryRepoInfrastructureFromGraph(ctx, h.Neo4j, repoParams)
	}
	dependencies := []map[string]any{}
	if h.Neo4j != nil {
		dependencies = queryRepoDependencies(ctx, h.Neo4j, repoParams)
	}
	return map[string]any{
		"id":                     "workload:" + workloadName,
		"name":                   workloadName,
		"kind":                   "service",
		"repo_id":                repo.ID,
		"repo_name":              repo.Name,
		"instances":              []map[string]any{},
		"dependencies":           dependencies,
		"infrastructure":         infrastructure,
		"materialization_status": "identity_only",
		"query_basis":            "repository_read_model",
		"limitations":            []string{"workload_identity_not_materialized"},
	}, nil
}

func matchingRepositoryWorkloadIdentity(serviceName string, repo RepositoryCatalogEntry, workloadNames []string) string {
	selector := strings.TrimSpace(serviceName)
	if selector == "" {
		return ""
	}
	plainSelector := strings.TrimPrefix(selector, "workload:")
	for _, workloadName := range workloadNames {
		normalized := strings.TrimSpace(workloadName)
		if normalized == "" {
			continue
		}
		if selector == normalized || plainSelector == normalized || selector == "workload:"+normalized {
			return normalized
		}
	}
	if selector != repo.Name && plainSelector != repo.Name {
		return ""
	}
	if len(workloadNames) != 1 {
		return ""
	}
	return strings.TrimSpace(workloadNames[0])
}

// fetchRepositoryNameByID uses the workload repo_id property as the selective
// anchor and avoids a relationship traversal on hot service read paths.
func (h *EntityHandler) fetchRepositoryNameByID(ctx context.Context, repoID string) (string, error) {
	cypher := `
		MATCH (r:Repository {id: $repo_id})
		RETURN r.name as repo_name
		LIMIT 1
	`
	row, err := h.Neo4j.RunSingle(ctx, cypher, map[string]any{"repo_id": repoID})
	if err != nil || row == nil {
		return "", err
	}
	return StringVal(row, "repo_name"), nil
}

// fetchWorkloadRepository resolves the repository link without OPTIONAL MATCH,
// avoiding backend-specific projection behavior in graph read paths.
func (h *EntityHandler) fetchWorkloadRepository(ctx context.Context, whereClause string, params map[string]any) (string, string, error) {
	cypher := fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		MATCH (r:Repository)-[:DEFINES]->(w)
		RETURN r.id as repo_id, r.name as repo_name
		LIMIT 1
	`, whereClause)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", nil
	}
	return StringVal(rows[0], "repo_id"), StringVal(rows[0], "repo_name"), nil
}

// fetchWorkloadInstances assembles instance and platform fields from scalar
// rows so query surfaces do not depend on backend map-projection semantics.
func (h *EntityHandler) fetchWorkloadInstances(ctx context.Context, whereClause string, params map[string]any, repoID string) ([]map[string]any, error) {
	workloadID := StringVal(params, "workload_id")
	instanceCypher := fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
		RETURN i.id as instance_id,
		       i.environment as environment,
		       i.materialization_confidence as materialization_confidence,
		       i.materialization_provenance as materialization_provenance
		ORDER BY environment, instance_id
	`, whereClause)
	if workloadID != "" {
		instanceCypher = `
			MATCH (i:WorkloadInstance)
			WHERE i.workload_id = $workload_id
			RETURN i.id as instance_id,
			       i.environment as environment,
			       i.materialization_confidence as materialization_confidence,
			       i.materialization_provenance as materialization_provenance
			ORDER BY environment, instance_id
		`
	}
	instanceRows, err := h.Neo4j.Run(ctx, instanceCypher, params)
	if err != nil {
		return nil, err
	}
	if len(instanceRows) == 0 {
		return []map[string]any{}, nil
	}

	instances := make([]map[string]any, 0, len(instanceRows))
	byID := make(map[string]map[string]any, len(instanceRows))
	for _, row := range instanceRows {
		instanceID := StringVal(row, "instance_id")
		if instanceID == "" {
			continue
		}
		instance := map[string]any{
			"instance_id":                instanceID,
			"platform_name":              "",
			"platform_kind":              "",
			"platforms":                  []map[string]any{},
			"environment":                StringVal(row, "environment"),
			"materialization_confidence": floatVal(row, "materialization_confidence"),
			"materialization_provenance": StringSliceVal(row, "materialization_provenance"),
			"platform_confidence":        0.0,
			"platform_reason":            "",
		}
		instances = append(instances, instance)
		byID[instanceID] = instance
	}

	platformRows, err := h.fetchWorkloadPlatformRows(ctx, instances)
	if err != nil {
		return nil, err
	}
	if len(platformRows) == 0 {
		provisionedRows, err := h.fetchProvisionedPlatformRows(ctx, repoID)
		if err != nil {
			return nil, err
		}
		if len(provisionedRows) == 1 {
			attachProvisionedPlatform(instances, provisionedRows[0])
			return instances, nil
		}
	}
	for _, row := range platformRows {
		instance := byID[StringVal(row, "instance_id")]
		if instance == nil {
			continue
		}
		platform := map[string]any{
			"platform_name":       StringVal(row, "platform_name"),
			"platform_kind":       StringVal(row, "platform_kind"),
			"platform_confidence": platformEdgeConfidence(row),
			"platform_reason":     platformEdgeReason(row),
		}
		instance["platforms"] = append(platformTargets(instance), platform)
		if StringVal(instance, "platform_name") == "" {
			instance["platform_name"] = platform["platform_name"]
			instance["platform_kind"] = platform["platform_kind"]
			instance["platform_confidence"] = platform["platform_confidence"]
			instance["platform_reason"] = platform["platform_reason"]
		}
	}

	return instances, nil
}

// fetchWorkloadPlatformRows anchors each platform lookup by exact instance id
// so NornicDB can preserve relationship properties and avoid workload-wide
// relationship expansion shapes on service read paths.
func (h *EntityHandler) fetchWorkloadPlatformRows(ctx context.Context, instances []map[string]any) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil || len(instances) == 0 {
		return nil, nil
	}
	const platformCypherTemplate = `
		MATCH (i:WorkloadInstance {id: %s})-[runsOn:RUNS_ON]->(p:Platform)
		RETURN i.id as instance_id,
		       p.name as platform_name,
		       p.kind as platform_kind,
		       runsOn.confidence as platform_confidence,
		       runsOn.reason as platform_reason,
		       properties(runsOn) as platform_edge
		ORDER BY platform_name
	`
	rows := make([]map[string]any, 0, len(instances))
	for _, instance := range instances {
		instanceID := StringVal(instance, "instance_id")
		if instanceID == "" {
			continue
		}
		instanceRows, err := h.Neo4j.Run(ctx, fmt.Sprintf(platformCypherTemplate, cypherStringLiteral(instanceID)), nil)
		if err != nil {
			return nil, err
		}
		rows = append(rows, instanceRows...)
	}
	return rows, nil
}

// cypherStringLiteral returns a single-quoted Cypher string literal for values
// that already came from graph truth and must be used in backend-sensitive
// exact-id read anchors.
func cypherStringLiteral(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// platformEdgeConfidence preserves edge confidence when a backend can return
// relationship properties but not the scalar relationship-property projection.
func platformEdgeConfidence(row map[string]any) float64 {
	if confidence := floatVal(row, "platform_confidence"); confidence != 0 {
		return confidence
	}
	return floatVal(mapValue(row, "platform_edge"), "confidence")
}

// platformEdgeReason preserves edge rationale through the same relationship
// properties fallback used for confidence.
func platformEdgeReason(row map[string]any) string {
	if reason := StringVal(row, "platform_reason"); reason != "" {
		return reason
	}
	return StringVal(mapValue(row, "platform_edge"), "reason")
}

// fetchProvisionedPlatformRows uses repository-level platform evidence as a
// bounded alternative to expanding RUNS_ON from each workload instance.
func (h *EntityHandler) fetchProvisionedPlatformRows(ctx context.Context, repoID string) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil || strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (target:Repository {id: $repo_id})<-[rel:PROVISIONS_DEPENDENCY_FOR]-(repo:Repository)-[:PROVISIONS_PLATFORM]->(p:Platform)
		RETURN DISTINCT p.id as platform_id,
		       p.name as platform_name,
		       p.kind as platform_kind,
		       p.provider as platform_provider,
		       p.region as platform_region,
		       p.locator as platform_locator,
		       rel.confidence as platform_confidence,
		       rel.reason as platform_reason
		ORDER BY platform_name, platform_id
	`, map[string]any{"repo_id": repoID})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// attachProvisionedPlatform applies one unambiguous provisioned platform to
// all materialized workload instances for the service.
func attachProvisionedPlatform(instances []map[string]any, row map[string]any) {
	platform := map[string]any{
		"platform_name":       StringVal(row, "platform_name"),
		"platform_kind":       StringVal(row, "platform_kind"),
		"platform_confidence": floatVal(row, "platform_confidence"),
		"platform_reason":     StringVal(row, "platform_reason"),
	}
	for _, instance := range instances {
		instance["platforms"] = []map[string]any{platform}
		instance["platform_name"] = platform["platform_name"]
		instance["platform_kind"] = platform["platform_kind"]
		instance["platform_confidence"] = platform["platform_confidence"]
		instance["platform_reason"] = platform["platform_reason"]
	}
}
