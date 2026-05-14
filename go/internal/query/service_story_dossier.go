package query

import (
	"sort"
	"strings"
)

const serviceStoryItemLimit = 50

func enrichServiceStoryDossierResponse(response map[string]any, workloadContext map[string]any) {
	response["service_identity"] = buildServiceIdentity(workloadContext)
	response["api_surface"] = buildServiceDossierAPISurface(workloadContext)
	response["deployment_lanes"] = buildServiceDeploymentLanes(workloadContext)
	response["upstream_dependencies"] = buildServiceUpstreamDependencies(workloadContext)
	response["downstream_consumers"] = buildServiceDownstreamConsumers(workloadContext)
	response["evidence_graph"] = buildServiceEvidenceGraph(workloadContext)
	response["result_limits"] = buildServiceResultLimits(workloadContext)
	rawContextLimits := map[string]any{}
	for _, key := range []string{
		"hostnames",
		"entrypoints",
		"network_paths",
		"observed_config_environments",
		"dependents",
		"consumer_repositories",
		"provisioning_source_chains",
		"deployment_evidence",
		"limitations",
	} {
		if value, ok := workloadContext[key]; ok && value != nil {
			bounded, limit := boundedServiceStoryRawValue(value)
			response[key] = bounded
			if len(limit) > 0 {
				rawContextLimits[key] = limit
			}
		}
	}
	if len(rawContextLimits) > 0 {
		response["raw_context_limits"] = rawContextLimits
	}
}

func buildServiceIdentity(workloadContext map[string]any) map[string]any {
	identity := map[string]any{
		"service_id":   safeStr(workloadContext, "id"),
		"service_name": safeStr(workloadContext, "name"),
		"kind":         safeStr(workloadContext, "kind"),
		"repo_id":      safeStr(workloadContext, "repo_id"),
		"repo_name":    safeStr(workloadContext, "repo_name"),
	}
	for _, key := range []string{"materialization_status", "query_basis"} {
		if value := safeStr(workloadContext, key); value != "" {
			identity[key] = value
		}
	}
	if limitations := StringSliceVal(workloadContext, "limitations"); len(limitations) > 0 {
		identity["limitations"] = limitations
	}
	return identity
}

func buildServiceDossierAPISurface(workloadContext map[string]any) map[string]any {
	apiSurface := copyMap(mapValue(workloadContext, "api_surface"))
	if len(apiSurface) == 0 {
		return map[string]any{
			"endpoint_count": 0,
			"method_count":   0,
			"spec_count":     0,
			"endpoints":      []map[string]any{},
			"truncated":      false,
		}
	}
	endpoints, truncated := capMapRows(mapSliceValue(apiSurface, "endpoints"), serviceStoryItemLimit)
	apiSurface["endpoints"] = endpoints
	apiSurface["truncated"] = truncated
	if _, ok := apiSurface["endpoint_count"]; !ok {
		apiSurface["endpoint_count"] = len(endpoints)
	}
	if _, ok := apiSurface["method_count"]; !ok {
		apiSurface["method_count"] = 0
	}
	if _, ok := apiSurface["spec_count"]; !ok {
		apiSurface["spec_count"] = firstPositiveInt(apiSurface, "spec_files_count", "spec_path_count")
		if apiSurface["spec_count"] == 0 {
			apiSurface["spec_count"] = len(StringSliceVal(apiSurface, "spec_files"))
		}
		if apiSurface["spec_count"] == 0 {
			apiSurface["spec_count"] = len(StringSliceVal(apiSurface, "spec_paths"))
		}
	}
	return apiSurface
}

func buildServiceDeploymentLanes(workloadContext map[string]any) []map[string]any {
	lanes := map[string]map[string]any{}
	for _, instance := range mapSliceValue(workloadContext, "instances") {
		laneType := serviceLaneType(StringVal(instance, "platform_kind"), "")
		if laneType == "" {
			continue
		}
		lane := serviceLane(lanes, laneType)
		addUniqueStringField(lane, "environments", StringVal(instance, "environment"))
		addUniqueStringField(lane, "runtime_platforms", StringVal(instance, "platform_name"))
		addUniqueStringField(lane, "platform_kinds", StringVal(instance, "platform_kind"))
	}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		laneType := serviceLaneType(StringVal(artifact, "runtime_platform_kind"), StringVal(artifact, "artifact_family"))
		if laneType == "" {
			continue
		}
		lane := serviceLane(lanes, laneType)
		addUniqueStringField(lane, "environments", StringVal(artifact, "environment"))
		addUniqueStringField(lane, "source_repositories", StringVal(artifact, "source_repo_name"))
		addUniqueStringField(lane, "relationship_types", StringVal(artifact, "relationship_type"))
		addUniqueStringField(lane, "resolved_ids", StringVal(artifact, "resolved_id"))
		if confidence := relationshipFloatVal(artifact, "confidence"); confidence > 0 {
			lane["max_confidence"] = maxFloat(floatVal(lane, "max_confidence"), confidence)
		}
	}
	result := make([]map[string]any, 0, len(lanes))
	for _, lane := range lanes {
		sortStringFields(lane, "environments", "runtime_platforms", "platform_kinds", "source_repositories", "relationship_types", "resolved_ids")
		result = append(result, lane)
	}
	sort.Slice(result, func(i, j int) bool {
		return StringVal(result[i], "lane_type") < StringVal(result[j], "lane_type")
	})
	return result
}

func serviceLane(lanes map[string]map[string]any, laneType string) map[string]any {
	lane := lanes[laneType]
	if lane == nil {
		lane = map[string]any{"lane_type": laneType}
		lanes[laneType] = lane
	}
	return lane
}

func serviceLaneType(platformKind string, artifactFamily string) string {
	joined := strings.ToLower(strings.TrimSpace(platformKind + " " + artifactFamily))
	switch {
	case strings.Contains(joined, "ecs") || strings.Contains(joined, "terraform"):
		return "ecs_terraform"
	case strings.Contains(joined, "argocd") ||
		strings.Contains(joined, "k8s") ||
		strings.Contains(joined, "kubernetes") ||
		strings.Contains(joined, "helm") ||
		strings.Contains(joined, "kustomize"):
		return "k8s_gitops"
	default:
		return ""
	}
}

func buildServiceUpstreamDependencies(workloadContext map[string]any) []map[string]any {
	rows := serviceUpstreamDependencyRows(workloadContext)
	sort.Slice(rows, func(i, j int) bool {
		if StringVal(rows[i], "relationship_type") != StringVal(rows[j], "relationship_type") {
			return StringVal(rows[i], "relationship_type") < StringVal(rows[j], "relationship_type")
		}
		return StringVal(rows[i], "source") < StringVal(rows[j], "source")
	})
	capped, _ := capMapRows(rows, serviceStoryItemLimit)
	return capped
}

func serviceUpstreamDependencyRows(workloadContext map[string]any) []map[string]any {
	rows := make([]map[string]any, 0)
	seenArtifacts := map[string]map[string]any{}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		if StringVal(artifact, "direction") == "outgoing" {
			continue
		}
		row := map[string]any{
			"source":            StringVal(artifact, "source_repo_name"),
			"source_repo_id":    StringVal(artifact, "source_repo_id"),
			"target":            StringVal(artifact, "target_repo_name"),
			"target_repo_id":    StringVal(artifact, "target_repo_id"),
			"relationship_type": StringVal(artifact, "relationship_type"),
			"resolved_id":       StringVal(artifact, "resolved_id"),
			"confidence":        relationshipFloatVal(artifact, "confidence"),
			"evidence_count":    firstPositiveInt(artifact, "evidence_count"),
			"rationale":         StringVal(artifact, "rationale"),
		}
		key := serviceRelationshipKey(row)
		if existing := seenArtifacts[key]; existing != nil {
			mergeServiceRelationshipRow(existing, row)
			continue
		}
		seenArtifacts[key] = row
		rows = append(rows, row)
	}
	for _, dependency := range mapSliceValue(workloadContext, "dependencies") {
		rows = append(rows, map[string]any{
			"source":            safeStr(workloadContext, "name"),
			"target":            firstNonEmptyString(StringVal(dependency, "target_name"), StringVal(dependency, "name")),
			"target_id":         StringVal(dependency, "target_id"),
			"relationship_type": firstNonEmptyString(StringVal(dependency, "type"), StringVal(dependency, "relationship_type")),
			"confidence":        relationshipFloatVal(dependency, "confidence"),
		})
	}
	for _, chain := range mapSliceValue(workloadContext, "provisioning_source_chains") {
		rows = append(rows, map[string]any{
			"source":            StringVal(chain, "repository"),
			"source_repo_id":    StringVal(chain, "repo_id"),
			"relationship_type": "PROVISIONING_SOURCE_CHAIN",
			"modules":           StringSliceVal(chain, "modules"),
		})
	}
	return rows
}

func buildServiceDownstreamConsumers(workloadContext map[string]any) map[string]any {
	dependents, graphTruncated := capMapRows(mapSliceValue(workloadContext, "dependents"), serviceStoryItemLimit)
	consumers, contentTruncated := capMapRows(mapSliceValue(workloadContext, "consumer_repositories"), serviceStoryItemLimit)
	return map[string]any{
		"graph_dependent_count":  len(mapSliceValue(workloadContext, "dependents")),
		"content_consumer_count": len(mapSliceValue(workloadContext, "consumer_repositories")),
		"graph_dependents":       dependents,
		"content_consumers":      consumers,
		"truncated":              graphTruncated || contentTruncated,
	}
}

func buildServiceEvidenceGraph(workloadContext map[string]any) map[string]any {
	nodes := map[string]map[string]any{}
	serviceID := safeStr(workloadContext, "id")
	addEvidenceNode(nodes, serviceID, safeStr(workloadContext, "name"), "service", "service")
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		addEvidenceNode(nodes, StringVal(artifact, "source_repo_id"), StringVal(artifact, "source_repo_name"), "repository", "upstream")
		addEvidenceNode(nodes, StringVal(artifact, "target_repo_id"), StringVal(artifact, "target_repo_name"), "repository", "service")
	}
	for _, consumer := range mapSliceValue(workloadContext, "consumer_repositories") {
		addEvidenceNode(nodes, StringVal(consumer, "repo_id"), StringVal(consumer, "repository"), "repository", "downstream")
	}
	for _, dependent := range mapSliceValue(workloadContext, "dependents") {
		addEvidenceNode(nodes, StringVal(dependent, "repo_id"), StringVal(dependent, "repository"), "repository", "downstream")
	}
	edges, edgeCount, edgeTruncated := serviceEvidenceGraphEdges(workloadContext)
	return map[string]any{
		"nodes":      sortedEvidenceNodes(nodes),
		"edges":      edges,
		"edge_count": edgeCount,
		"truncated":  edgeTruncated,
	}
}

func serviceEvidenceGraphEdges(workloadContext map[string]any) ([]map[string]any, int, bool) {
	edges := make([]map[string]any, 0)
	seenEdges := map[string]map[string]any{}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		resolvedID := StringVal(artifact, "resolved_id")
		if resolvedID == "" {
			continue
		}
		edge := map[string]any{
			"id":                resolvedID,
			"source":            StringVal(artifact, "source_repo_id"),
			"target":            StringVal(artifact, "target_repo_id"),
			"relationship_type": StringVal(artifact, "relationship_type"),
			"confidence":        relationshipFloatVal(artifact, "confidence"),
			"evidence_count":    firstPositiveInt(artifact, "evidence_count"),
			"rationale":         StringVal(artifact, "rationale"),
			"resolved_id":       resolvedID,
		}
		if existing := seenEdges[resolvedID]; existing != nil {
			mergeServiceRelationshipRow(existing, edge)
			continue
		}
		seenEdges[resolvedID] = edge
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if StringVal(edges[i], "relationship_type") != StringVal(edges[j], "relationship_type") {
			return StringVal(edges[i], "relationship_type") < StringVal(edges[j], "relationship_type")
		}
		return StringVal(edges[i], "resolved_id") < StringVal(edges[j], "resolved_id")
	})
	capped, truncated := capMapRows(edges, serviceStoryItemLimit)
	return capped, len(edges), truncated
}

func buildServiceResultLimits(workloadContext map[string]any) map[string]any {
	apiSurface := buildServiceDossierAPISurface(workloadContext)
	endpointCount := IntVal(apiSurface, "endpoint_count")
	if endpointCount == 0 {
		endpointCount = len(mapSliceValue(apiSurface, "endpoints"))
	}
	upstreamCount := len(serviceUpstreamDependencyRows(workloadContext))
	dependentCount := len(mapSliceValue(workloadContext, "dependents"))
	contentConsumerCount := len(mapSliceValue(workloadContext, "consumer_repositories"))
	consumerCount := len(mapSliceValue(workloadContext, "dependents")) +
		len(mapSliceValue(workloadContext, "consumer_repositories"))
	return map[string]any{
		"limit":                serviceStoryItemLimit,
		"ordering":             "deterministic",
		"endpoint_count":       endpointCount,
		"upstream_count":       upstreamCount,
		"downstream_count":     consumerCount,
		"truncated":            endpointCount > serviceStoryItemLimit || upstreamCount > serviceStoryItemLimit || dependentCount > serviceStoryItemLimit || contentConsumerCount > serviceStoryItemLimit,
		"drilldown_basis":      "resolved_id",
		"relationship_tool":    "get_relationship_evidence",
		"service_context_path": "/api/v0/services/" + safeStr(workloadContext, "name") + "/context",
	}
}

func serviceDeploymentArtifacts(workloadContext map[string]any) []map[string]any {
	return mapSliceValue(mapValue(workloadContext, "deployment_evidence"), "artifacts")
}

func addEvidenceNode(nodes map[string]map[string]any, id string, label string, kind string, category string) {
	if id == "" {
		return
	}
	if label == "" {
		label = id
	}
	nodes[id] = map[string]any{
		"id":       id,
		"label":    label,
		"kind":     kind,
		"category": category,
	}
}

func sortedEvidenceNodes(nodes map[string]map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, node)
	}
	sort.Slice(result, func(i, j int) bool {
		return StringVal(result[i], "id") < StringVal(result[j], "id")
	})
	return result
}

func capMapRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

func copyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func addUniqueStringField(row map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	values := StringSliceVal(row, key)
	values = append(values, value)
	row[key] = uniqueSortedStrings(values)
}

func sortStringFields(row map[string]any, keys ...string) {
	for _, key := range keys {
		values := StringSliceVal(row, key)
		sort.Strings(values)
		row[key] = values
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxFloat(left float64, right float64) float64 {
	if left >= right {
		return left
	}
	return right
}
