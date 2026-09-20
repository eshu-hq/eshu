// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// ResourcesCapability is the capability key for the bounded Terraform/IaC
	// resource list read. It resolves against the authoritative graph, so it
	// requires the local-authoritative profile or higher (see capabilityMatrix).
	ResourcesCapability = "iac_inventory.resources.list"

	// resourcesDefaultLimit and resourcesMaxLimit bound the page size. The
	// list is a hot graph read, so limit is required and capped.
	resourcesDefaultLimit = 50
	resourcesMaxLimit     = 200
)

// resourceKind selects which Terraform/IaC graph label the list endpoint
// scans. The kinds map to a single canonical label each so the read stays
// anchored on one indexed label rather than a broad multi-label scan.
type resourceKind string

const (
	resourceKindResource   resourceKind = "resource"
	resourceKindModule     resourceKind = "module"
	resourceKindDataSource resourceKind = "data-source"
)

// resourceKindLabels maps the public kind selector to the graph label it
// scans. Only these closed values are accepted, so the label interpolated into
// Cypher never comes from raw user input.
var resourceKindLabels = map[resourceKind]string{
	resourceKindResource:   "TerraformResource",
	resourceKindModule:     "TerraformModule",
	resourceKindDataSource: "TerraformDataSource",
}

// resourceRow is one row in the bounded IaC resource list. Candidates for
// this list always come from the current-inventory Postgres CTE
// (inventory_postgres.go), which filters to fact_kind = 'content_entity'
// -- the config-side generic parser/entity pipeline only, never
// terraform_state_resource facts -- so kind=resource has only ever hydrated
// config-declared TerraformResource nodes, both before and after #5443 split
// state-observed resources onto their own TerraformStateResource label.
// Optional fields are still omitted when empty because not every
// canonical-sourced node carries provider, service, category, and repository
// attribution (e.g. a resource type Eshu has not classified yet).
type resourceRow struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	ResourceName string `json:"resource_name,omitempty"`
	Type         string `json:"type,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Service      string `json:"resource_service,omitempty"`
	Category     string `json:"resource_category,omitempty"`
	Module       string `json:"module,omitempty"`
	RepoID       string `json:"repo_id,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	LineNumber   int    `json:"line_number,omitempty"`
}

// resourceFilter holds the normalized, bounded query for one list call.
type resourceFilter struct {
	Kind         resourceKind
	Type         string
	Provider     string
	Module       string
	Repository   string
	AfterName    string
	AfterID      string
	CandidateIDs []string
	Limit        int
}

// listResources serves the bounded Terraform/IaC resource browse read.
//
// GET /api/v0/iac/resources?kind=&q=&type=&provider=&module=&repository=&include_facets=&limit=&after_name=&after_id=
func (h *Handler) listResources(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryIaCResources, "GET /api/v0/iac/resources", ResourcesCapability)
	defer span.End()

	metrics := resourceMetrics()
	kind, ok := parseIaCResourceKind(querycontract.QueryParam(r, "kind"))
	if !ok {
		metrics.recordError(r.Context(), "unknown", "invalid_kind")
		querycontract.WriteError(w, http.StatusBadRequest, "kind must be one of: resource, module, data-source")
		return
	}

	if querycontract.CapabilityUnsupported(h.profile(), ResourcesCapability) {
		metrics.recordError(r.Context(), string(kind), "unsupported_capability")
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"IaC resource inventory requires the authoritative graph",
			querycontract.ErrorCodeUnsupportedCapability,
			ResourcesCapability,
			h.profile(),
			querycontract.RequiredProfile(ResourcesCapability),
		)
		return
	}

	limit, ok := requiredIaCResourceLimit(w, r)
	if !ok {
		metrics.recordError(r.Context(), string(kind), "invalid_limit")
		return
	}

	if h == nil || h.Graph == nil || h.Inventory == nil {
		metrics.recordError(r.Context(), string(kind), "backend_unavailable")
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"IaC resource inventory requires the authoritative graph and current-generation store",
			querycontract.ErrorCodeBackendUnavailable,
			ResourcesCapability,
			h.profile(),
			querycontract.RequiredProfile(ResourcesCapability),
		)
		return
	}

	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	// Empty-grant scoped tokens (no granted repository or ingestion scope) can
	// match nothing, so return a bounded empty page without touching the
	// authoritative graph.
	if access.Empty() {
		metrics.recordDuration(r.Context(), string(kind), time.Since(start).Seconds())
		writeIaCResourceEmptyPage(w, r, h.profile(), kind, limit)
		return
	}

	filter := resourceFilter{
		Kind:       kind,
		Type:       querycontract.QueryParam(r, "type"),
		Provider:   querycontract.QueryParam(r, "provider"),
		Module:     querycontract.QueryParam(r, "module"),
		Repository: querycontract.QueryParam(r, "repository"),
		AfterName:  querycontract.QueryParam(r, "after_name"),
		AfterID:    querycontract.QueryParam(r, "after_id"),
		// limit+1 truncation: fetch one extra row to detect more pages without
		// a second count round trip.
		Limit: limit + 1,
	}
	query := querycontract.QueryParam(r, "q")
	candidates, err := searchActiveIaCInventory(r.Context(), h.Inventory, kind, query, filter, access)
	if err != nil {
		metrics.recordError(r.Context(), string(kind), "inventory_search_error")
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filter.CandidateIDs = make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		filter.CandidateIDs = append(filter.CandidateIDs, candidate.ID)
	}

	var rows []map[string]any
	if len(candidates) > 0 {
		cypher, params := buildIaCResourceQuery(filter)
		rows, err = h.Graph.Run(r.Context(), cypher, params)
		if err != nil {
			metrics.recordError(r.Context(), string(kind), "graph_error")
			if querycontract.WriteGraphReadError(w, r, err, ResourcesCapability) {
				return
			}
			querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !searchHydrationMatches(candidates, rows) {
			metrics.recordError(r.Context(), string(kind), "inventory_graph_mismatch")
			querycontract.WriteError(w, http.StatusInternalServerError, "current inventory and graph projection disagree")
			return
		}
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]resourceRow, 0, len(rows))
	for _, row := range rows {
		results = append(results, resourceRowFromGraph(kind, row))
	}

	body := map[string]any{
		"kind":      string(kind),
		"resources": results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
	}
	if querycontract.QueryParam(r, "include_facets") == "true" {
		summary, err := h.Inventory.Summary(r.Context(), access, inventoryFacetLimit)
		if err != nil {
			metrics.recordError(r.Context(), string(kind), "inventory_summary_error")
			querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		body["summary"] = summary
	}
	if truncated && len(results) > 0 {
		last := results[len(results)-1]
		body["next_cursor"] = map[string]string{
			"after_name": last.Name,
			"after_id":   last.ID,
		}
	}

	metrics.recordDuration(r.Context(), string(kind), time.Since(start).Seconds())
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		ResourcesCapability,
		querycontract.TruthBasisHybrid,
		"current active-generation identities resolved from Postgres and hydrated from the authoritative Terraform/IaC graph; bounded list ordered by name then id",
	))
}

// searchHydrationMatches checks the graph hydration against the candidate
// identities already selected, filtered, authorized, and paginated by
// Postgres. Candidates carrying a generation (the active-inventory CTE path)
// must match it exactly, so a retained historical graph node never leaks into
// current truth. Candidates from infra_resource_entities carry no generation
// provenance (the backfill records scope_id and generation_id as "", see
// storage/postgres/infra/inventory MirrorRepo), so they match on identity and
// name only; their currency comes from the table's derive, which mirrors
// current content rows. A candidate with an empty ID or name never matches.
func searchHydrationMatches(candidates []InventoryCandidate, rows []map[string]any) bool {
	if len(candidates) != len(rows) {
		return false
	}
	type expectedHydration struct {
		name string
		// generationID is empty when the candidate carries no generation
		// provenance (table path); only a non-empty generation is checked.
		generationID string
	}
	want := make(map[string]expectedHydration, len(candidates))
	for _, candidate := range candidates {
		if candidate.ID == "" || candidate.Name == "" {
			return false
		}
		want[candidate.ID] = expectedHydration{name: candidate.Name, generationID: candidate.GenerationID}
	}
	if len(want) != len(candidates) {
		return false
	}
	for _, row := range rows {
		id := querycontract.StringVal(row, "id")
		expected, ok := want[id]
		if !ok || expected.name != querycontract.StringVal(row, "name") {
			return false
		}
		if expected.generationID != "" && expected.generationID != querycontract.StringVal(row, "generation_id") {
			return false
		}
		delete(want, id)
	}
	return len(want) == 0
}

// parseIaCResourceKind maps the optional kind selector to a closed enum,
// defaulting to resource when empty. The closed enum keeps the label
// interpolated into Cypher free of raw user input.
func parseIaCResourceKind(raw string) (resourceKind, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "resource", "resources":
		return resourceKindResource, true
	case "module", "modules":
		return resourceKindModule, true
	case "data-source", "data_source", "datasource", "data-sources":
		return resourceKindDataSource, true
	default:
		return "", false
	}
}

// requiredIaCResourceLimit enforces an explicit 1..200 limit. The list is a hot
// graph read, so an unbounded or oversize page is rejected rather than clamped
// silently.
func requiredIaCResourceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		return resourcesDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > resourcesMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", resourcesMaxLimit))
		return 0, false
	}
	return limit, true
}

// buildIaCResourceQuery hydrates the exact current identities already selected,
// filtered, authorized, and keyset-paginated by Postgres. The closed graph label
// and measured uid-only predicate preserve NornicDB's property-index seek. The
// legacy n.id-plus-generation shape was not index-backed on the retained
// backend; current generation remains an application-level exactness check so
// no unmeasured graph predicate is added to the selected query. The hydration
// is checked against candidate identity, name, and generation before any row is
// returned.
func buildIaCResourceQuery(filter resourceFilter) (string, map[string]any) {
	label := resourceKindLabels[filter.Kind]
	params := map[string]any{
		"candidate_ids": append([]string(nil), filter.CandidateIDs...),
		"limit":         filter.Limit,
	}

	cypher := "MATCH (n:" + label + ") WHERE n.uid IN $candidate_ids" +
		" RETURN coalesce(n.id, '') AS id," +
		" coalesce(n.name, '') AS name," +
		" coalesce(n.resource_name, '') AS resource_name," +
		" coalesce(n.resource_type, n.data_type, '') AS type," +
		" coalesce(n.provider, '') AS provider," +
		" coalesce(n.resource_service, '') AS resource_service," +
		" coalesce(n.resource_category, '') AS resource_category," +
		" coalesce(n.repo_id, '') AS repo_id," +
		" coalesce(n.relative_path, '') AS relative_path," +
		" coalesce(n.line_number, 0) AS line_number," +
		" coalesce(n.generation_id, '') AS generation_id" +
		" ORDER BY n.name, n.id" +
		" LIMIT $limit"
	return cypher, params
}

// resourceRowFromGraph maps one graph row to the wire shape and derives the
// module name from the resource/data-source name prefix when present.
func resourceRowFromGraph(kind resourceKind, row map[string]any) resourceRow {
	name := querycontract.StringVal(row, "name")
	out := resourceRow{
		ID:           querycontract.StringVal(row, "id"),
		Kind:         string(kind),
		Name:         name,
		ResourceName: querycontract.StringVal(row, "resource_name"),
		Type:         querycontract.StringVal(row, "type"),
		Provider:     querycontract.StringVal(row, "provider"),
		Service:      querycontract.StringVal(row, "resource_service"),
		Category:     querycontract.StringVal(row, "resource_category"),
		RepoID:       querycontract.StringVal(row, "repo_id"),
		RelativePath: querycontract.StringVal(row, "relative_path"),
		LineNumber:   querycontract.IntVal(row, "line_number"),
	}
	if kind == resourceKindModule {
		out.Module = name
	} else if module := moduleNameFromResourceName(name); module != "" {
		out.Module = module
	}
	return out
}

// moduleNameFromResourceName extracts the module instance name from a
// Terraform resource/data-source address of the form
// `module."<name>".aws_x.y` or `module.<name>.aws_x.y`. It returns "" when the
// address is not module-scoped.
func moduleNameFromResourceName(name string) string {
	const prefix = "module."
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	rest := name[len(prefix):]
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		// Quoted form: module."api-node".aws_x.y
		if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
			return rest[1 : 1+end]
		}
		return ""
	}
	// Bare form: module.foo.aws_x.y, or for_each/count form module.foo["k"].x.
	// Cut at the first '.' or '[' so the for_each index is not folded into the
	// module name.
	end := len(rest)
	if dot := strings.IndexByte(rest, '.'); dot >= 0 && dot < end {
		end = dot
	}
	if br := strings.IndexByte(rest, '['); br >= 0 && br < end {
		end = br
	}
	return rest[:end]
}
