package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// InfraHandler serves HTTP endpoints for querying infrastructure resources
// and relationships from the Neo4j canonical graph.
type InfraHandler struct {
	Neo4j   GraphQuery
	Profile QueryProfile
}

var infraCategoryLabels = map[string][]string{
	"k8s": {
		"K8sResource",
		"KustomizeOverlay",
	},
	"terraform": {
		"TerraformResource",
		"TerraformModule",
		"TerraformVariable",
		"TerraformOutput",
		"TerraformDataSource",
		"TerraformProvider",
		"TerraformLocal",
		"TerraformBlock",
		"TerragruntConfig",
		"TerragruntDependency",
		"CloudFormationResource",
	},
	"argocd": {
		"ArgoCDApplication",
		"ArgoCDApplicationSet",
	},
	"crossplane": {
		"CrossplaneXRD",
		"CrossplaneComposition",
		"CrossplaneClaim",
	},
	"helm": {
		"HelmChart",
		"HelmValues",
	},
}

var allInfraLabels = []string{
	"K8sResource",
	"KustomizeOverlay",
	"TerraformResource",
	"TerraformModule",
	"TerraformVariable",
	"TerraformOutput",
	"TerraformDataSource",
	"TerraformProvider",
	"TerraformLocal",
	"TerraformBlock",
	"TerragruntConfig",
	"TerragruntDependency",
	"CloudFormationResource",
	"ArgoCDApplication",
	"ArgoCDApplicationSet",
	"CrossplaneXRD",
	"CrossplaneComposition",
	"CrossplaneClaim",
	"HelmChart",
	"HelmValues",
}

// Mount registers infrastructure query routes on the given mux.
func (h *InfraHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/infra/resources/search", h.searchResources)
	mux.HandleFunc("POST /api/v0/infra/relationships", h.getRelationships)
	mux.HandleFunc("GET /api/v0/ecosystem/overview", h.getEcosystemOverview)
}

func (h *InfraHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// searchResources searches infrastructure resources by name or ID.
// POST /api/v0/infra/resources/search
// Body: {"query": "...", "kind": "...", "limit": 50}
func (h *InfraHandler) searchResources(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryInfraResourceSearch,
		"POST /api/v0/infra/resources/search",
		"platform_impact.deployment_chain",
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), "platform_impact.deployment_chain") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"infrastructure search requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.deployment_chain",
			h.profile(),
			requiredProfile("platform_impact.deployment_chain"),
		)
		return
	}

	var req struct {
		Query            string `json:"query"`
		Kind             string `json:"kind"`
		Category         string `json:"category"`
		Provider         string `json:"provider"`
		ResourceService  string `json:"resource_service"`
		ResourceCategory string `json:"resource_category"`
		Limit            int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Query == "" {
		WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	kind := strings.TrimSpace(req.Kind)
	provider := strings.TrimSpace(req.Provider)
	resourceService := strings.TrimSpace(req.ResourceService)
	resourceCategory := strings.TrimSpace(req.ResourceCategory)

	labels := allInfraLabels
	if category := strings.TrimSpace(req.Category); category != "" {
		mapped, ok := infraCategoryLabels[strings.ToLower(category)]
		if !ok {
			WriteError(w, http.StatusBadRequest, "unsupported category")
			return
		}
		labels = mapped
	}

	cypher := `
		MATCH (n)
		WHERE ` + infraLabelPredicate(labels)
	if strings.Contains(req.Query, "::") {
		cypher += `
		  AND coalesce(n.resource_type, n.data_type, '') = $resource_type_query
	`
	} else {
		cypher += `
		  AND (
		       n.name CONTAINS $query
		       OR n.id CONTAINS $query
		       OR coalesce(n.kind, '') CONTAINS $query
		       OR coalesce(n.resource_type, n.data_type, '') = $resource_type_query
		       OR coalesce(n.resource_type, n.data_type, '') CONTAINS $resource_type_query
		       OR coalesce(n.source, '') CONTAINS $query
		       OR coalesce(n.config_path, '') CONTAINS $query
		)
	`
	}

	if kind != "" {
		cypher += " AND (n.kind = $kind OR coalesce(n.resource_type, n.data_type, '') = $kind)"
	}
	if provider != "" {
		cypher += " AND coalesce(n.provider, '') = $provider"
	}
	if resourceService != "" {
		cypher += " AND coalesce(n.resource_service, '') = $resource_service"
	}
	if resourceCategory != "" {
		cypher += " AND coalesce(n.resource_category, '') = $resource_category"
	}

	cypher += `
		RETURN n.id as id, n.name as name, labels(n) as labels,
		       n.kind as kind, n.provider as provider, n.environment as environment,
		       n.source as source, n.config_path as config_path,
		       coalesce(n.resource_type, n.data_type, '') as resource_type,
		       n.resource_service as resource_service,
		       n.resource_category as resource_category
		ORDER BY n.name
		LIMIT $limit
	`

	params := map[string]any{
		"query":               req.Query,
		"resource_type_query": req.Query,
		"limit":               req.Limit,
	}
	if kind != "" {
		params["kind"] = kind
	}
	if provider != "" {
		params["provider"] = provider
	}
	if resourceService != "" {
		params["resource_service"] = resourceService
	}
	if resourceCategory != "" {
		params["resource_category"] = resourceCategory
	}

	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"id":          StringVal(row, "id"),
			"name":        StringVal(row, "name"),
			"labels":      StringSliceVal(row, "labels"),
			"kind":        StringVal(row, "kind"),
			"provider":    StringVal(row, "provider"),
			"environment": StringVal(row, "environment"),
			"source":      StringVal(row, "source"),
			"config_path": StringVal(row, "config_path"),
		}
		if resourceType := StringVal(row, "resource_type"); resourceType != "" {
			result["resource_type"] = resourceType
		}
		if resourceService := StringVal(row, "resource_service"); resourceService != "" {
			result["resource_service"] = resourceService
		}
		if resourceCategory := StringVal(row, "resource_category"); resourceCategory != "" {
			result["resource_category"] = resourceCategory
		}
		results = append(results, result)
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"results": results,
		"count":   len(results),
	}, BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from infrastructure graph search"))
}

// infraLabelPredicate renders fixed internal label choices as direct label
// predicates so graph backends can use label matching without list functions.
func infraLabelPredicate(labels []string) string {
	if len(labels) == 0 {
		return "false"
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, "n:"+label)
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// getRelationships returns all relationships for a given entity.
// POST /api/v0/infra/relationships
// Body: {"entity_id": "..."}
func (h *InfraHandler) getRelationships(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.deployment_chain") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"infrastructure relationship analysis requires authoritative platform truth",
			"unsupported_capability",
			"platform_impact.deployment_chain",
			h.profile(),
			requiredProfile("platform_impact.deployment_chain"),
		)
		return
	}

	var req struct {
		EntityID string `json:"entity_id"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.EntityID == "" {
		WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	cypher := `
		MATCH (n) WHERE n.id = $entity_id
		OPTIONAL MATCH (n)-[r]->(target)
		OPTIONAL MATCH (source)-[r2]->(n)
		RETURN n.id as id, n.name as name, labels(n) as labels,
		       collect(DISTINCT {
		           direction: 'outgoing',
		           type: type(r),
		           target_name: target.name,
		           target_id: target.id,
		           target_labels: labels(target)
		       }) as outgoing,
		       collect(DISTINCT {
		           direction: 'incoming',
		           type: type(r2),
		           source_name: source.name,
		           source_id: source.id,
		           source_labels: labels(source)
		       }) as incoming
	`

	params := map[string]any{
		"entity_id": req.EntityID,
	}

	row, err := h.Neo4j.RunSingle(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	// Extract relationships, filtering out null entries
	outgoing := filterNullRelationships(row["outgoing"])
	incoming := filterNullRelationships(row["incoming"])

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"id":       StringVal(row, "id"),
		"name":     StringVal(row, "name"),
		"labels":   StringSliceVal(row, "labels"),
		"outgoing": outgoing,
		"incoming": incoming,
	}, BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from infrastructure relationship graph"))
}

// getEcosystemOverview returns high-level counts of graph entities.
// GET /api/v0/ecosystem/overview
func (h *InfraHandler) getEcosystemOverview(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"ecosystem overview requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			requiredProfile("platform_impact.context_overview"),
		)
		return
	}

	cypher := `
		MATCH (r:Repository) WITH count(r) as repo_count
		MATCH (w:Workload) WITH repo_count, count(w) as workload_count
		MATCH (p:Platform) WITH repo_count, workload_count, count(p) as platform_count
		OPTIONAL MATCH (i:WorkloadInstance)
		WITH repo_count, workload_count, platform_count, count(i) as instance_count
		RETURN repo_count, workload_count, platform_count, instance_count
	`

	row, err := h.Neo4j.RunSingle(r.Context(), cypher, nil)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		// No data yet, return zeros
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"repo_count":     0,
			"workload_count": 0,
			"platform_count": 0,
			"instance_count": 0,
		}, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisHybrid, "resolved from ecosystem summary counters"))
		return
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_count":     IntVal(row, "repo_count"),
		"workload_count": IntVal(row, "workload_count"),
		"platform_count": IntVal(row, "platform_count"),
		"instance_count": IntVal(row, "instance_count"),
	}, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisHybrid, "resolved from ecosystem summary counters"))
}

// filterNullRelationships removes entries where type is nil (from OPTIONAL MATCH with no matches).
func filterNullRelationships(v any) []map[string]any {
	switch slice := v.(type) {
	case []map[string]any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if item["type"] == nil {
				continue
			}
			result = append(result, item)
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Skip entries where type is nil (no relationship matched)
			if m["type"] == nil {
				continue
			}
			result = append(result, m)
		}
		return result
	default:
		return nil
	}
}
