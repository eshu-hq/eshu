// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	cloudResourceListCapability   = "platform_impact.cloud_resource_list"
	cloudResourceListMaxLimit     = 200
	cloudResourceListDefaultLimit = 50
)

// CloudResourceRow is one bounded cloud-provider resource projected from a
// CloudResource graph node. Only fields that exist on the node are projected;
// provider mirrors collector_kind because the raw provider property is not
// populated by the AWS collector. Empty optional fields are omitted from the
// wire payload.
type CloudResourceRow struct {
	ID           string `json:"id"`
	ResourceType string `json:"resource_type,omitempty"`
	Name         string `json:"name,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Region       string `json:"region,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	ARN          string `json:"arn,omitempty"`
	ServiceName  string `json:"service_name,omitempty"`
	State        string `json:"state,omitempty"`
}

// cloudResourceListFilter holds the optional, bounded filters for the cloud
// resource list query. Empty values are treated as "no filter" and are not
// added to the Cypher WHERE clause.
type cloudResourceListFilter struct {
	Provider     string
	ResourceType string
	Region       string
	AccountID    string
}

// cloudResourceListCursor is the keyset anchor for the next page. It pairs the
// resource_type and id of the last returned row, matching the ORDER BY so the
// next page resumes deterministically without a deep SKIP scan.
type cloudResourceListCursor struct {
	AfterResourceType string
	AfterID           string
}

// mountCloudResourceRoutes registers the bounded cloud resource list route on
// the infra handler. CloudResource nodes are the "cloud" infra category, so the
// route lives with the rest of the infra graph reads and reuses the same
// GraphQuery port.
func (h *InfraHandler) mountCloudResourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/cloud/resources", h.listCloudResources)
}

// listCloudResources serves a bounded, filterable, keyset-paged list of cloud
// provider resources from the authoritative graph.
//
// GET /api/v0/cloud/resources?provider=&resource_type=&region=&account_id=&limit=&after_resource_type=&after_id=
func (h *InfraHandler) listCloudResources(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCloudResourceList,
		"GET /api/v0/cloud/resources",
		cloudResourceListCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cloudResourceListCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"cloud resource list requires the authoritative graph",
			ErrorCodeUnsupportedCapability,
			cloudResourceListCapability,
			h.profile(),
			requiredProfile(cloudResourceListCapability),
		)
		return
	}
	if h.Neo4j == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"cloud resource list requires the authoritative graph",
			ErrorCodeBackendUnavailable,
			cloudResourceListCapability,
			h.profile(),
			requiredProfile(cloudResourceListCapability),
		)
		return
	}

	limit, ok := parseCloudResourceListLimit(w, r)
	if !ok {
		return
	}
	filter := cloudResourceListFilterFromRequest(r)
	cursor := cloudResourceListCursorFromRequest(r)

	cypher, params := buildCloudResourceListQuery(filter, cursor, limit+1)

	start := time.Now()
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		recordCloudResourceList(r.Context(), start, true)
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	recordCloudResourceList(r.Context(), start, false)

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	results := make([]CloudResourceRow, 0, len(rows))
	for _, row := range rows {
		results = append(results, cloudResourceRowFromGraph(row))
	}

	body := map[string]any{
		"resources": results,
		"count":     len(results),
		"limit":     limit,
		"truncated": truncated,
		"scope":     cloudResourceListScope(filter),
	}
	if truncated && len(results) > 0 {
		last := results[len(results)-1]
		body["next_cursor"] = map[string]string{
			"after_resource_type": last.ResourceType,
			"after_id":            last.ID,
		}
	}

	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		cloudResourceListCapability,
		TruthBasisAuthoritativeGraph,
		"resolved from the authoritative cloud resource graph; ordered by resource_type then id, keyset-paged",
	))
}

// buildCloudResourceListQuery assembles the bounded Cypher list query and its
// parameters. The query anchors on the CloudResource label, applies optional
// equality filters and the keyset cursor predicate as bound parameters, orders
// deterministically by resource_type then id, and returns a narrow projection.
// resource_type and arn are indexed; id is unique, so the keyset resume stays
// selective.
func buildCloudResourceListQuery(filter cloudResourceListFilter, cursor cloudResourceListCursor, fetchLimit int) (string, map[string]any) {
	params := map[string]any{"limit": fetchLimit}
	var conditions []string

	if filter.Provider != "" {
		conditions = append(conditions, "n.collector_kind = $provider")
		params["provider"] = filter.Provider
	}
	if filter.ResourceType != "" {
		conditions = append(conditions, "n.resource_type = $resource_type")
		params["resource_type"] = filter.ResourceType
	}
	if filter.Region != "" {
		conditions = append(conditions, "n.region = $region")
		params["region"] = filter.Region
	}
	if filter.AccountID != "" {
		conditions = append(conditions, "n.account_id = $account_id")
		params["account_id"] = filter.AccountID
	}
	if cursor.AfterID != "" {
		conditions = append(conditions,
			"(n.resource_type > $after_resource_type OR (n.resource_type = $after_resource_type AND n.id > $after_id))")
		params["after_resource_type"] = cursor.AfterResourceType
		params["after_id"] = cursor.AfterID
	}

	var where string
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ") + "\n"
	}

	cypher := fmt.Sprintf(`
		MATCH (n:CloudResource)
		%sRETURN n.id AS id,
		       n.resource_type AS resource_type,
		       n.name AS name,
		       n.collector_kind AS provider,
		       n.region AS region,
		       n.account_id AS account_id,
		       n.arn AS arn,
		       n.service_name AS service_name,
		       n.state AS state
		ORDER BY n.resource_type, n.id
		LIMIT $limit
	`, where)
	return cypher, params
}

// cloudResourceRowFromGraph converts a graph result row into the wire shape.
// service_name carries a known collector placeholder ("row.service_name") for
// some nodes; that placeholder is dropped so the API never surfaces it.
func cloudResourceRowFromGraph(row map[string]any) CloudResourceRow {
	serviceName := StringVal(row, "service_name")
	if serviceName == "row.service_name" {
		serviceName = ""
	}
	return CloudResourceRow{
		ID:           StringVal(row, "id"),
		ResourceType: StringVal(row, "resource_type"),
		Name:         StringVal(row, "name"),
		Provider:     StringVal(row, "provider"),
		Region:       StringVal(row, "region"),
		AccountID:    StringVal(row, "account_id"),
		ARN:          StringVal(row, "arn"),
		ServiceName:  serviceName,
		State:        StringVal(row, "state"),
	}
}
