// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const infraSearchMaxLimit = 200

// InfraHandler serves HTTP endpoints for querying infrastructure resources
// and relationships from the Neo4j canonical graph.
type InfraHandler struct {
	Neo4j          GraphQuery
	Aggregates     InfraResourceAggregateStore
	CloudResources CloudResourceListStore
	Profile        QueryProfile
	Instruments    *telemetry.Instruments

	relationshipBreakdownOnce  sync.Once
	relationshipBreakdownSlots chan struct{}
}

var infraCategoryLabels = map[string][]string{
	"k8s": {
		"K8sResource",
		"KustomizeOverlay",
	},
	"terraform": {
		"TerraformResource",
		"TerraformStateResource",
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
	// A Crossplane Claim is edge-only (issue #5347): it stays a K8sResource
	// node and the SATISFIED_BY edge to its CrossplaneXRD is the
	// classification, so no node ever carries a CrossplaneClaim label. The
	// crossplane category therefore lists only the labels a Claim search can
	// actually match (issue #5478); it is not itself dead — XRDs and
	// Compositions are live materialized labels.
	"crossplane": {
		"CrossplaneXRD",
		"CrossplaneComposition",
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
	"TerraformStateResource",
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
	"HelmChart",
	"HelmValues",
}

// infraLabelSet indexes allInfraLabels for O(1) membership tests.
var infraLabelSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(allInfraLabels))
	for _, label := range allInfraLabels {
		set[label] = struct{}{}
	}
	return set
}()

// infraLabelAllowed reports whether label is a known infrastructure node label.
// It gates any label interpolated into a Cypher pattern so the label text is
// never attacker-influenced.
func infraLabelAllowed(label string) bool {
	_, ok := infraLabelSet[label]
	return ok
}

// infraSearchReturnColumns is the single source of truth for
// searchResources's result columns. Both the per-label CALL branch's inner
// RETURN and the outer RETURN's column list are generated from this slice
// (see infraSearchReturnExprs), so they cannot drift out of sync — a
// mismatch would make NornicDB fail the whole query at runtime with
// "Variable not defined", a failure the unit tests using
// recordingInfraGraphReader cannot catch since they assert on the generated
// query text without executing it against a backend.
var infraSearchReturnColumns = []string{
	"id", "name", "labels", "kind", "provider", "source_system", "environment",
	"source", "config_path", "resource_type", "resource_service",
	"resource_category", "resource_id", "arn", "account_id", "region", "service_kind",
}

var infraSearchReturnColumnExprs = map[string]string{
	"id":                "coalesce(n.id, '')",
	"name":              "coalesce(n.name, '')",
	"labels":            "labels(n)",
	"kind":              "coalesce(n.kind, '')",
	"provider":          "coalesce(n.provider, '')",
	"source_system":     "coalesce(n.source_system, '')",
	"environment":       "coalesce(n.environment, '')",
	"source":            "coalesce(n.source, n.source_system, '')",
	"config_path":       "coalesce(n.config_path, '')",
	"resource_type":     "coalesce(n.resource_type, n.data_type, '')",
	"resource_service":  "coalesce(n.resource_service, n.service_kind, '')",
	"resource_category": "coalesce(n.resource_category, '')",
	"resource_id":       "coalesce(n.resource_id, '')",
	"arn":               "coalesce(n.arn, '')",
	"account_id":        "coalesce(n.account_id, '')",
	"region":            "coalesce(n.region, '')",
	"service_kind":      "coalesce(n.service_kind, '')",
}

// infraSearchReturnExprs renders infraSearchReturnColumns as "expr as alias"
// pairs for the per-label CALL branch's inner RETURN clause, in the same
// column order the outer RETURN references by alias.
func infraSearchReturnExprs() []string {
	exprs := make([]string, len(infraSearchReturnColumns))
	for i, col := range infraSearchReturnColumns {
		exprs[i] = infraSearchReturnColumnExprs[col] + " as " + col
	}
	return exprs
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

	// The candidate label set is matched with one MATCH(n:Label) branch per
	// label, unioned, instead of a single MATCH(n) scan filtered by an
	// (n:A OR n:B OR ...) predicate. An unlabeled MATCH(n) forces a
	// whole-graph scan on every call regardless of how selective the later
	// AND-conditions are (see docs/public/reference/cypher-performance.md).
	// A MATCH(n:A|B|C) label disjunction is not a safe substitute either:
	// docs/public/reference/nornicdb-pitfalls.md documents that node-label
	// disjunction inside a single MATCH matches zero rows on the pinned
	// NornicDB backend. UNION of single-label branches uses an indexed label
	// scan per branch and sidesteps both defects. Plain UNION (not UNION
	// ALL) is used deliberately: it de-duplicates identical rows, so the
	// result stays exactly one row per node even if a future schema change
	// ever allows a node to carry more than one of these labels.
	//
	// The whole union is wrapped in a CALL {...} subquery, which is load
	// bearing, not stylistic. See docs/public/reference/nornicdb-pitfalls.md
	// ("A Bare Top-Level UNION Returns Nothing When Its First Branch Is
	// Empty"): a bare top-level UNION chain returns zero rows for the ENTIRE
	// query whenever its FIRST branch matches zero rows, even though later
	// branches have real matches. allInfraLabels starts with CloudResource,
	// so any search on a corpus with zero matching CloudResource nodes would
	// have silently returned an empty result for every other label without
	// this wrapper.
	whereExtra := ""
	if query != "" {
		if strings.Contains(query, "::") {
			whereExtra += `
			  AND (
			       coalesce(n.resource_type, n.data_type, '') = $resource_type_query
			       OR coalesce(n.arn, '') = $query
			       OR coalesce(n.resource_id, '') = $query
			)
	`
		} else {
			whereExtra += `
			  AND ` + infraResourceFreeTextPredicate + `
	`
		}
	}

	if kind != "" {
		whereExtra += " AND (n.kind = $kind OR n.resource_type = $kind OR n.data_type = $kind OR n.service_kind = $kind)"
	}
	if provider != "" {
		whereExtra += " AND " + infraSearchProviderFilterPredicate(labels)
	}
	if environment != "" {
		whereExtra += " AND n.environment = $environment"
	}
	if resourceService != "" {
		whereExtra += " AND " + infraSearchResourceServiceFilterPredicate(labels)
	}
	if resourceCategory != "" {
		whereExtra += " AND n.resource_category = $resource_category"
	}
	whereExtra += infraSearchScopeClause(access)

	// infraSearchReturnClause (the CALL block's inner RETURN, per branch) and
	// the outer RETURN's column list are both derived from
	// infraSearchReturnColumns so they cannot drift apart. A mismatch
	// between them would make NornicDB fail the whole query at runtime with
	// "Variable not defined" — a failure the recordingInfraGraphReader-based
	// unit tests cannot catch, since they assert on the generated string
	// without executing it against a backend (flagged in PR #5278 review).
	infraSearchReturnClause := "\n\t\tRETURN " + strings.Join(infraSearchReturnExprs(), ",\n\t\t       ") + "\n\t"
	branches := make([]string, 0, len(labels))
	for _, label := range labels {
		branches = append(branches, `
		MATCH (n:`+label+`)
		WHERE true`+whereExtra+infraSearchReturnClause)
	}
	cypher := "CALL {" + strings.Join(branches, "\nUNION") + `
	}
	RETURN ` + strings.Join(infraSearchReturnColumns, ", ") + `
		ORDER BY name
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

	var rows []map[string]any
	var err error
	if isArgoCDCategoryOnly(
		query,
		kind,
		category,
		provider,
		environment,
		resourceService,
		resourceCategory,
	) {
		rows, err = h.searchArgoCDCategoryRows(r.Context(), access, req.Limit+1)
	} else {
		rows, err = h.Neo4j.Run(r.Context(), cypher, params)
	}
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
