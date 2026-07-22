// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// graphEntityInventoryCapability gates the graph entity inventory readback. It
// reuses the platform context-overview capability because the inventory answers
// the same "what entities exist in the graph" question as the ecosystem
// overview, just browsably and per-kind.
const graphEntityInventoryCapability = "platform_impact.context_overview"

const (
	graphEntityInventoryDefaultLimit = 50
	graphEntityInventoryMaxLimit     = 200
)

// graphEntityKind describes one browsable node-kind facet on the Nodes page.
// Each facet maps a stable, console-facing key to one real graph label plus the
// node property the human-readable name and (optional) account/scope come from.
// Only labels that exist in the Go-owned graph schema are listed so the count
// and list queries never scan for a label the writer never creates.
type graphEntityKind struct {
	// key is the stable console-facing facet id (e.g. "services").
	key string
	// label is the single graph label the facet counts and lists.
	label string
	// nameProp is the node property used for the display name and name search.
	nameProp string
	// accountProp is the node property surfaced as the account/scope column, or
	// empty when the label carries no account-like field.
	accountProp string
}

// graphEntityKinds is the ordered, curated facet catalog. The order is the
// display order of the KIND filter chips. "all" is implicit (no kind filter).
//
// Mapping note: the console calls Workload nodes "Services" because Eshu has no
// separate Service label; workloads are the first-class deployable unit. Module
// nodes back the "Libraries" facet. Both choices match the existing Catalog and
// ecosystem-overview surfaces.
var graphEntityKinds = []graphEntityKind{
	{key: "services", label: "Workload", nameProp: "name", accountProp: "repo_id"},
	{key: "repositories", label: "Repository", nameProp: "name", accountProp: ""},
	{key: "libraries", label: "Module", nameProp: "name", accountProp: ""},
	{key: "container_images", label: "ContainerImage", nameProp: "name", accountProp: ""},
	{key: "environments", label: "Environment", nameProp: "name", accountProp: ""},
	{key: "cloud_resources", label: "CloudResource", nameProp: "name", accountProp: "account_id"},
	{key: "identity_iam", label: "ExternalPrincipal", nameProp: "principal_value", accountProp: "principal_account_id"},
	{key: "networking", label: "SecurityGroupRule", nameProp: "name", accountProp: ""},
}

func graphEntityKindByKey(key string) (graphEntityKind, bool) {
	for _, kind := range graphEntityKinds {
		if kind.key == key {
			return kind, true
		}
	}
	return graphEntityKind{}, false
}

// GraphEntityInventoryHandler serves the browsable graph entity inventory that
// backs the console Nodes page: per-kind counts plus a bounded, name-searchable
// list of first-class entities.
type GraphEntityInventoryHandler struct {
	Neo4j   GraphQuery
	Profile QueryProfile
}

// Mount registers the graph entity inventory route on the given mux.
func (h *GraphEntityInventoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/graph/entities", h.listEntities)
}

func (h *GraphEntityInventoryHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// graphEntityKindListCypher builds the bounded list query for one facet. The
// match is anchored on a single label, name search uses a case-insensitive
// CONTAINS, and ORDER BY + LIMIT keep the read bounded for the few-seconds SLA.
// Labels with a name index (Workload, Module, Environment) keep ORDER BY cheap;
// the remaining labels are small populations where a bounded scan is acceptable.
func graphEntityKindListCypher(kind graphEntityKind, hasSearch bool) string {
	var b strings.Builder
	b.WriteString("MATCH (n:" + kind.label + ")")
	if hasSearch {
		b.WriteString(" WHERE toLower(coalesce(n." + kind.nameProp + ", '')) CONTAINS $q")
	}
	b.WriteString(" RETURN coalesce(n.id, '') AS id, coalesce(n." + kind.nameProp + ", '') AS name")
	if kind.accountProp != "" {
		b.WriteString(", coalesce(n." + kind.accountProp + ", '') AS account")
	} else {
		b.WriteString(", '' AS account")
	}
	b.WriteString(" ORDER BY name SKIP $offset LIMIT $limit")
	return b.String()
}

// listEntities serves GET /api/v0/graph/entities.
//
// Without a kind filter it returns the per-kind facet counts and an empty entity
// list (the chip row + stat tiles). With kind=<facet> it returns a bounded,
// name-searchable, paginated slice of that kind's entities for the table. The
// per-kind counts always accompany the response so the console can render the
// filter chips with live counts on every request.
func (h *GraphEntityInventoryHandler) listEntities(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryGraphEntityInventory,
		"GET /api/v0/graph/entities",
		graphEntityInventoryCapability,
	)
	roundTrips := 0
	facetRows := 0
	resultCount := 0
	truncated := false
	defer func() {
		span.SetAttributes(
			attribute.Int("eshu.query.graph_entity_inventory.round_trip_count", roundTrips),
			attribute.Int("eshu.query.graph_entity_inventory.facet_row_count", facetRows),
			attribute.Int("eshu.query.graph_entity_inventory.result_count", resultCount),
			attribute.Bool("eshu.query.graph_entity_inventory.truncated", truncated),
		)
		span.End()
	}()

	if capabilityUnsupported(h.profile(), graphEntityInventoryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"graph entity inventory requires authoritative platform context truth",
			ErrorCodeUnsupportedCapability,
			graphEntityInventoryCapability,
			h.profile(),
			requiredProfile(graphEntityInventoryCapability),
		)
		return
	}

	kindKey := QueryParam(r, "kind")
	var selected *graphEntityKind
	if kindKey != "" {
		match, ok := graphEntityKindByKey(kindKey)
		if !ok {
			WriteError(w, http.StatusBadRequest, "unsupported kind")
			return
		}
		selected = &match
	}

	limit := graphEntityInventoryLimit(r)
	offset := graphEntityInventoryOffset(r)
	search := strings.ToLower(QueryParam(r, "q"))

	countRows, err := h.Neo4j.Run(r.Context(), graphEntityKindCountsCypher(graphEntityKinds), nil)
	roundTrips++
	if err != nil {
		span.RecordError(err)
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	facetRows = len(countRows)
	kindCounts, total, err := decodeGraphEntityKindCounts(countRows)
	if err != nil {
		span.RecordError(err)
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entities := make([]map[string]any, 0)
	if selected != nil {
		params := map[string]any{
			"limit":  limit + 1,
			"offset": offset,
		}
		if search != "" {
			params["q"] = search
		}
		rows, err := h.Neo4j.Run(r.Context(), graphEntityKindListCypher(*selected, search != ""), params)
		roundTrips++
		if err != nil {
			span.RecordError(err)
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, row := range rows {
			if len(entities) >= limit {
				truncated = true
				break
			}
			entities = append(entities, map[string]any{
				"id":      StringVal(row, "id"),
				"name":    StringVal(row, "name"),
				"kind":    selected.key,
				"account": StringVal(row, "account"),
			})
		}
	}
	resultCount = len(entities)

	body := map[string]any{
		"kinds":     kindCounts,
		"total":     total,
		"entities":  entities,
		"count":     len(entities),
		"limit":     limit,
		"offset":    offset,
		"truncated": truncated,
	}
	if selected != nil {
		body["kind"] = selected.key
	}

	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		graphEntityInventoryCapability,
		TruthBasisHybrid,
		"resolved from one label-bounded facet-count read and an optional bounded list",
	))
}

func graphEntityInventoryLimit(r *http.Request) int {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return graphEntityInventoryDefaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return graphEntityInventoryDefaultLimit
	}
	if n > graphEntityInventoryMaxLimit {
		return graphEntityInventoryMaxLimit
	}
	return n
}

func graphEntityInventoryOffset(r *http.Request) int {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
