// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// iacResourcesCapability is the capability key for the bounded Terraform/IaC
	// resource list read. It resolves against the authoritative graph, so it
	// requires the local-authoritative profile or higher (see capabilityMatrix).
	iacResourcesCapability = "iac_inventory.resources.list"

	// iacResourcesDefaultLimit and iacResourcesMaxLimit bound the page size. The
	// list is a hot graph read, so limit is required and capped.
	iacResourcesDefaultLimit = 50
	iacResourcesMaxLimit     = 200
)

// iacResourceKind selects which Terraform/IaC graph label the list endpoint
// scans. The kinds map to a single canonical label each so the read stays
// anchored on one indexed label rather than a broad multi-label scan.
type iacResourceKind string

const (
	iacResourceKindResource   iacResourceKind = "resource"
	iacResourceKindModule     iacResourceKind = "module"
	iacResourceKindDataSource iacResourceKind = "data-source"
)

// iacResourceKindLabels maps the public kind selector to the graph label it
// scans. Only these closed values are accepted, so the label interpolated into
// Cypher never comes from raw user input.
var iacResourceKindLabels = map[iacResourceKind]string{
	iacResourceKindResource:   "TerraformResource",
	iacResourceKindModule:     "TerraformModule",
	iacResourceKindDataSource: "TerraformDataSource",
}

// iacResourceRow is one row in the bounded IaC resource list. Optional fields
// are omitted when empty because tfstate-sourced TerraformResource nodes carry
// only identity and type, while canonical-sourced nodes also carry provider,
// service, category, and repository attribution.
type iacResourceRow struct {
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

// iacResourceFilter holds the normalized, bounded query for one list call.
type iacResourceFilter struct {
	Kind      iacResourceKind
	Type      string
	Provider  string
	Module    string
	AfterName string
	AfterID   string
	Limit     int
}

// listResources serves the bounded Terraform/IaC resource browse read.
//
// GET /api/v0/iac/resources?kind=&type=&provider=&module=&limit=&after_name=&after_id=
func (h *IaCHandler) listResources(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryIaCResources, "GET /api/v0/iac/resources", iacResourcesCapability)
	defer span.End()

	metrics := iacResourceMetrics()
	kind, ok := parseIaCResourceKind(QueryParam(r, "kind"))
	if !ok {
		metrics.recordError(r.Context(), "unknown", "invalid_kind")
		WriteError(w, http.StatusBadRequest, "kind must be one of: resource, module, data-source")
		return
	}

	if capabilityUnsupported(h.profile(), iacResourcesCapability) {
		metrics.recordError(r.Context(), string(kind), "unsupported_capability")
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"IaC resource inventory requires the authoritative graph",
			ErrorCodeUnsupportedCapability,
			iacResourcesCapability,
			h.profile(),
			requiredProfile(iacResourcesCapability),
		)
		return
	}

	limit, ok := requiredIaCResourceLimit(w, r)
	if !ok {
		metrics.recordError(r.Context(), string(kind), "invalid_limit")
		return
	}

	if h == nil || h.Graph == nil {
		metrics.recordError(r.Context(), string(kind), "backend_unavailable")
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"IaC resource inventory requires the authoritative graph",
			ErrorCodeBackendUnavailable,
			iacResourcesCapability,
			h.profile(),
			requiredProfile(iacResourcesCapability),
		)
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	// Empty-grant scoped tokens (no granted repository or ingestion scope) can
	// match nothing, so return a bounded empty page without touching the
	// authoritative graph.
	if access.empty() {
		metrics.recordDuration(r.Context(), string(kind), time.Since(start).Seconds())
		writeIaCResourceEmptyPage(w, r, h.profile(), kind, limit)
		return
	}

	filter := iacResourceFilter{
		Kind:      kind,
		Type:      QueryParam(r, "type"),
		Provider:  QueryParam(r, "provider"),
		Module:    QueryParam(r, "module"),
		AfterName: QueryParam(r, "after_name"),
		AfterID:   QueryParam(r, "after_id"),
		// limit+1 truncation: fetch one extra row to detect more pages without
		// a second count round trip.
		Limit: limit + 1,
	}

	cypher, params := buildIaCResourceQuery(filter, access)
	rows, err := h.Graph.Run(r.Context(), cypher, params)
	if err != nil {
		metrics.recordError(r.Context(), string(kind), "graph_error")
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]iacResourceRow, 0, len(rows))
	for _, row := range rows {
		results = append(results, iacResourceRowFromGraph(kind, row))
	}

	body := map[string]any{
		"kind":      string(kind),
		"resources": results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
	}
	if truncated && len(results) > 0 {
		last := results[len(results)-1]
		body["next_cursor"] = map[string]string{
			"after_name": last.Name,
			"after_id":   last.ID,
		}
	}

	metrics.recordDuration(r.Context(), string(kind), time.Since(start).Seconds())
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		iacResourcesCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from the authoritative Terraform/IaC graph projection; bounded list ordered by name then id",
	))
}

// parseIaCResourceKind maps the optional kind selector to a closed enum,
// defaulting to resource when empty. The closed enum keeps the label
// interpolated into Cypher free of raw user input.
func parseIaCResourceKind(raw string) (iacResourceKind, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "resource", "resources":
		return iacResourceKindResource, true
	case "module", "modules":
		return iacResourceKindModule, true
	case "data-source", "data_source", "datasource", "data-sources":
		return iacResourceKindDataSource, true
	default:
		return "", false
	}
}

// requiredIaCResourceLimit enforces an explicit 1..200 limit. The list is a hot
// graph read, so an unbounded or oversize page is rejected rather than clamped
// silently.
func requiredIaCResourceLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return iacResourcesDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > iacResourcesMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", iacResourcesMaxLimit))
		return 0, false
	}
	return limit, true
}

// buildIaCResourceQuery renders the bounded list query. The label is one of the
// closed iacResourceKindLabels values; every filter and cursor value flows
// through bound parameters. The query anchors on the single label, applies
// indexed equality on resource_type/data_type and provider where present, and
// keyset-paginates on (name, id) so ORDER BY is deterministic and the cursor is
// stable across pages.
func buildIaCResourceQuery(filter iacResourceFilter, access repositoryAccessFilter) (string, map[string]any) {
	label := iacResourceKindLabels[filter.Kind]
	params := map[string]any{"limit": filter.Limit}

	clauses := make([]string, 0, 5)
	if filter.Type != "" {
		if filter.Kind == iacResourceKindDataSource {
			clauses = append(clauses, "n.data_type = $type")
		} else {
			clauses = append(clauses, "n.resource_type = $type")
		}
		params["type"] = filter.Type
	}
	if filter.Provider != "" {
		clauses = append(clauses, "n.provider = $provider")
		params["provider"] = filter.Provider
	}
	if filter.Module != "" {
		if filter.Kind == iacResourceKindModule {
			// Module nodes carry the module name directly.
			clauses = append(clauses, "n.name = $module")
			params["module"] = filter.Module
		} else {
			// Resource/data-source nodes embed the module path as a prefix on
			// n.name. Terraform writes bare `module.<name>.`, quoted
			// `module."<name>".`, and for_each/count indexed forms. Match those
			// prefixes through parameters so quoted module names are not silently
			// invisible.
			clauses = append(clauses, "("+
				"n.name STARTS WITH $module_prefix_dot OR "+
				"n.name STARTS WITH $module_prefix_idx OR "+
				"n.name STARTS WITH $module_prefix_quoted_dot OR "+
				"n.name STARTS WITH $module_prefix_quoted_idx)")
			params["module_prefix_dot"] = "module." + filter.Module + "."
			params["module_prefix_idx"] = "module." + filter.Module + "["
			params["module_prefix_quoted_dot"] = `module."` + filter.Module + `".`
			params["module_prefix_quoted_idx"] = `module."` + filter.Module + `"[`
		}
	}
	// Keyset cursor: rows strictly after (after_name, after_id) in (name, id)
	// order. The compound predicate keeps the page boundary exact when many
	// rows share a name.
	if filter.AfterName != "" || filter.AfterID != "" {
		clauses = append(clauses, "(n.name > $after_name OR (n.name = $after_name AND n.id > $after_id))")
		params["after_name"] = filter.AfterName
		params["after_id"] = filter.AfterID
	}

	// Scoped tokens bound the list to nodes whose durable repo_id is in the
	// granted repository / ingestion-scope set. The clause and its parameters
	// render only in scoped mode, so the unscoped query is byte-identical.
	if clause := iacResourceScopeClause(access); clause != "" {
		clauses = append(clauses, clause)
		iacResourceScopeParams(access, params)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	cypher := "MATCH (n:" + label + ")" + where +
		" RETURN coalesce(n.id, '') AS id," +
		" coalesce(n.name, '') AS name," +
		" coalesce(n.resource_name, '') AS resource_name," +
		" coalesce(n.resource_type, n.data_type, '') AS type," +
		" coalesce(n.provider, '') AS provider," +
		" coalesce(n.resource_service, '') AS resource_service," +
		" coalesce(n.resource_category, '') AS resource_category," +
		" coalesce(n.repo_id, '') AS repo_id," +
		" coalesce(n.relative_path, '') AS relative_path," +
		" coalesce(n.line_number, 0) AS line_number" +
		" ORDER BY n.name, n.id" +
		" LIMIT $limit"
	return cypher, params
}

// iacResourceRowFromGraph maps one graph row to the wire shape and derives the
// module name from the resource/data-source name prefix when present.
func iacResourceRowFromGraph(kind iacResourceKind, row map[string]any) iacResourceRow {
	name := StringVal(row, "name")
	out := iacResourceRow{
		ID:           StringVal(row, "id"),
		Kind:         string(kind),
		Name:         name,
		ResourceName: StringVal(row, "resource_name"),
		Type:         StringVal(row, "type"),
		Provider:     StringVal(row, "provider"),
		Service:      StringVal(row, "resource_service"),
		Category:     StringVal(row, "resource_category"),
		RepoID:       StringVal(row, "repo_id"),
		RelativePath: StringVal(row, "relative_path"),
		LineNumber:   IntVal(row, "line_number"),
	}
	if kind == iacResourceKindModule {
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
