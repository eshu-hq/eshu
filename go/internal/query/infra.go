// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const infraSearchMaxLimit = 200

// InfraHandler serves HTTP endpoints for querying infrastructure resources
// and relationships from the Neo4j canonical graph.
type InfraHandler struct {
	Neo4j      GraphQuery
	Aggregates InfraResourceAggregateStore
	Profile    QueryProfile
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
		"TerraformBackend",
		"TerraformImport",
		"TerraformMovedBlock",
		"TerraformRemovedBlock",
		"TerraformCheck",
		"TerraformLockProvider",
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
	"cloud": {
		"CloudResource",
	},
}

var allInfraLabels = []string{
	"CloudResource",
	"K8sResource",
	"KustomizeOverlay",
	"TerraformResource",
	"TerraformModule",
	"TerraformVariable",
	"TerraformOutput",
	"TerraformDataSource",
	"TerraformProvider",
	"TerraformLocal",
	"TerraformBackend",
	"TerraformImport",
	"TerraformMovedBlock",
	"TerraformRemovedBlock",
	"TerraformCheck",
	"TerraformLockProvider",
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
	mux.HandleFunc("POST /api/v0/ecosystem/graph-summary", h.getGraphSummaryPacket)
	mux.HandleFunc("POST /api/v0/relationships/catalog", h.getRelationshipsCatalog)
	mux.HandleFunc("POST /api/v0/relationships/edges", h.getRelationshipEdges)
	h.infraResourceAggregateRoutes(mux)
	h.mountCloudResourceRoutes(mux)
}

func (h *InfraHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// searchResources searches infrastructure resources by name, ID, or bounded
// structured filters.
// POST /api/v0/infra/resources/search
// Body: {"query": "...", "kind": "...", "category": "...", "limit": 50}
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
		Environment      string `json:"environment"`
		ResourceService  string `json:"resource_service"`
		ResourceCategory string `json:"resource_category"`
		Limit            int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > infraSearchMaxLimit {
		req.Limit = infraSearchMaxLimit
	}
	query := strings.TrimSpace(req.Query)
	kind := strings.TrimSpace(req.Kind)
	category := strings.TrimSpace(req.Category)
	provider := strings.TrimSpace(req.Provider)
	environment := strings.TrimSpace(req.Environment)
	resourceService := strings.TrimSpace(req.ResourceService)
	resourceCategory := strings.TrimSpace(req.ResourceCategory)
	if !infraSearchHasScope(query, kind, category, provider, environment, resourceService, resourceCategory) {
		WriteError(w, http.StatusBadRequest, "query or structured filter is required")
		return
	}

	labels := allInfraLabels
	if category != "" {
		mapped, ok := infraCategoryLabels[strings.ToLower(category)]
		if !ok {
			WriteError(w, http.StatusBadRequest, "unsupported category")
			return
		}
		labels = mapped
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyInfraSearch(w, r, req.Limit)
		return
	}

	cypher := `
		MATCH (n)
		WHERE ` + infraLabelPredicate(labels)
	if query != "" {
		if strings.Contains(query, "::") {
			cypher += `
			  AND (
			       coalesce(n.resource_type, n.data_type, '') = $resource_type_query
			       OR coalesce(n.arn, '') = $query
			       OR coalesce(n.resource_id, '') = $query
			)
	`
		} else {
			cypher += `
			  AND ` + infraResourceFreeTextPredicate + `
	`
		}
	}

	if kind != "" {
		cypher += " AND (n.kind = $kind OR n.resource_type = $kind OR n.data_type = $kind OR n.service_kind = $kind)"
	}
	if provider != "" {
		cypher += " AND " + infraSearchProviderFilterPredicate(labels)
	}
	if environment != "" {
		cypher += " AND n.environment = $environment"
	}
	if resourceService != "" {
		cypher += " AND " + infraSearchResourceServiceFilterPredicate(labels)
	}
	if resourceCategory != "" {
		cypher += " AND n.resource_category = $resource_category"
	}
	cypher += infraSearchScopeClause(access)

	cypher += `
		RETURN coalesce(n.id, '') as id, coalesce(n.name, '') as name, labels(n) as labels,
		       coalesce(n.kind, '') as kind, coalesce(n.provider, '') as provider, coalesce(n.source_system, '') as source_system, coalesce(n.environment, '') as environment,
		       coalesce(n.source, n.source_system, '') as source, coalesce(n.config_path, '') as config_path,
		       coalesce(n.resource_type, n.data_type, '') as resource_type,
		       coalesce(n.resource_service, n.service_kind, '') as resource_service,
		       coalesce(n.resource_category, '') as resource_category,
		       coalesce(n.resource_id, '') as resource_id, coalesce(n.arn, '') as arn,
		       coalesce(n.account_id, '') as account_id, coalesce(n.region, '') as region,
		       coalesce(n.service_kind, '') as service_kind
		ORDER BY n.name
		LIMIT $limit
	`

	params := map[string]any{"limit": req.Limit + 1}
	if query != "" {
		params["query"] = query
		params["resource_type_query"] = query
	}
	if kind != "" {
		params["kind"] = kind
	}
	if provider != "" {
		params["provider"] = provider
	}
	if environment != "" {
		params["environment"] = environment
	}
	if resourceService != "" {
		params["resource_service"] = resourceService
	}
	if resourceCategory != "" {
		params["resource_category"] = resourceCategory
	}
	access.graphParams(params)

	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if len(results) >= req.Limit {
			break
		}
		labels := StringSliceVal(row, "labels")
		result := map[string]any{
			"id":          StringVal(row, "id"),
			"name":        StringVal(row, "name"),
			"labels":      labels,
			"kind":        StringVal(row, "kind"),
			"provider":    infraSearchProviderFromRow(row, labels),
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
		if resourceID := StringVal(row, "resource_id"); resourceID != "" {
			result["resource_id"] = resourceID
		}
		if arn := StringVal(row, "arn"); arn != "" {
			result["arn"] = arn
		}
		if accountID := StringVal(row, "account_id"); accountID != "" {
			result["account_id"] = accountID
		}
		if region := StringVal(row, "region"); region != "" {
			result["region"] = region
		}
		if serviceKind := StringVal(row, "service_kind"); serviceKind != "" {
			result["service_kind"] = serviceKind
		}
		results = append(results, result)
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"results":   results,
		"count":     len(results),
		"limit":     req.Limit,
		"truncated": len(rows) > req.Limit,
	}, BuildTruthEnvelope(h.profile(), "platform_impact.deployment_chain", TruthBasisHybrid, "resolved from infrastructure graph search"))
}

func infraSearchHasScope(values ...string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
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

func infraSearchProviderFilterPredicate(labels []string) string {
	if infraLabelsAreCloudOnly(labels) {
		return "n.source_system = $provider"
	}
	if infraLabelsInclude(labels, "CloudResource") {
		return "(n.provider = $provider OR (n:CloudResource AND n.source_system = $provider))"
	}
	return "n.provider = $provider"
}

func infraSearchResourceServiceFilterPredicate(labels []string) string {
	if infraLabelsAreCloudOnly(labels) {
		return "n.service_kind = $resource_service"
	}
	if infraLabelsInclude(labels, "CloudResource") {
		return "(n.resource_service = $resource_service OR n.service_kind = $resource_service)"
	}
	return "n.resource_service = $resource_service"
}

func infraSearchProviderFromRow(row map[string]any, labels []string) string {
	if provider := StringVal(row, "provider"); provider != "" {
		return provider
	}
	if infraLabelsInclude(labels, "CloudResource") {
		return StringVal(row, "source_system")
	}
	return ""
}

func infraLabelsAreCloudOnly(labels []string) bool {
	return len(labels) == 1 && labels[0] == "CloudResource"
}

func infraLabelsInclude(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
