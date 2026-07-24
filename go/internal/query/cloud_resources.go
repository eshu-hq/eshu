// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
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
	if h.Neo4j == nil || h.CloudResources == nil {
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
	cursor, ok := parseCloudResourceListCursor(w, r)
	if !ok {
		return
	}
	start := time.Now()
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		recordCloudResourceList(r.Context(), start, 0, 0, false, "ok")
		writeCloudResourceListResponse(w, r, h.profile(), filter, limit, nil, false)
		return
	}

	identities, err := h.CloudResources.ListCloudResourceIdentities(r.Context(), CloudResourceListPageFilter{
		Provider:             filter.Provider,
		ResourceType:         filter.ResourceType,
		Region:               filter.Region,
		AccountID:            filter.AccountID,
		AfterResourceType:    cursor.AfterResourceType,
		AfterID:              cursor.AfterID,
		Limit:                limit + 1,
		AllScopes:            !access.scoped(),
		AllowedRepositoryIDs: access.grantedRepositoryIDs(),
		AllowedScopeIDs:      access.grantedScopeIDs(),
	})
	if err != nil {
		recordCloudResourceList(r.Context(), start, 0, 0, false, "store_error")
		WriteError(w, http.StatusInternalServerError, "cloud resource page selection failed")
		return
	}

	selectedRows := len(identities)
	truncated := len(identities) > limit
	if truncated {
		identities = identities[:limit]
	}
	if len(identities) == 0 {
		recordCloudResourceList(r.Context(), start, selectedRows, 0, truncated, "ok")
		writeCloudResourceListResponse(w, r, h.profile(), filter, limit, nil, truncated)
		return
	}

	cypher, params := buildCloudResourceHydrationQuery(identities)
	rows, err := h.Neo4j.Run(r.Context(), cypher, params)
	if err != nil {
		// The "graph_error" outcome label is accurate regardless of the eventual
		// HTTP status (500 vs. the bounded-availability 503/504 below), so it
		// keeps recording before the guard runs.
		recordCloudResourceList(r.Context(), start, selectedRows, 0, truncated, "graph_error")
		if WriteGraphReadError(w, r, err, cloudResourceListCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, "cloud resource graph hydration failed")
		return
	}
	results, err := hydrateCloudResourcePage(identities, rows)
	if err != nil {
		recordCloudResourceList(r.Context(), start, selectedRows, 0, truncated, "parity_error")
		WriteError(w, http.StatusInternalServerError, "cloud resource graph projection is inconsistent")
		return
	}
	recordCloudResourceList(r.Context(), start, selectedRows, len(results), truncated, "ok")
	writeCloudResourceListResponse(w, r, h.profile(), filter, limit, results, truncated)
}

func writeCloudResourceListResponse(
	w http.ResponseWriter,
	r *http.Request,
	profile QueryProfile,
	filter cloudResourceListFilter,
	limit int,
	results []CloudResourceRow,
	truncated bool,
) {
	if results == nil {
		results = []CloudResourceRow{}
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
		profile,
		cloudResourceListCapability,
		TruthBasisAuthoritativeGraph,
		"authorized and keyset-paged from the current graph owner ledger, then hydrated from the authoritative cloud resource graph",
	))
}

// buildCloudResourceHydrationQuery fetches only the page identities already
// selected, authorized, filtered, and ordered by Postgres.
func buildCloudResourceHydrationQuery(identities []CloudResourceListIdentity) (string, map[string]any) {
	uids := make([]string, 0, len(identities))
	for _, identity := range identities {
		uids = append(uids, identity.UID)
	}
	cypher := `
		MATCH (n:CloudResource)
		WHERE n.uid IN $uids
		RETURN n.uid AS uid,
		       n.id AS id,
		       n.resource_type AS resource_type,
		       n.name AS name,
		       n.collector_kind AS provider,
		       n.region AS region,
		       n.account_id AS account_id,
		       n.arn AS arn,
		       n.service_name AS service_name,
		       n.state AS state
	`
	return cypher, map[string]any{"uids": uids}
}

func hydrateCloudResourcePage(
	identities []CloudResourceListIdentity,
	rows []map[string]any,
) ([]CloudResourceRow, error) {
	want := make(map[string]CloudResourceListIdentity, len(identities))
	for _, identity := range identities {
		if identity.UID == "" || identity.ResourceType == "" {
			return nil, fmt.Errorf("invalid owner-ledger identity")
		}
		if _, duplicate := want[identity.UID]; duplicate {
			return nil, fmt.Errorf("duplicate owner-ledger identity")
		}
		want[identity.UID] = identity
	}
	byUID := make(map[string]CloudResourceRow, len(rows))
	for _, graphRow := range rows {
		uid := StringVal(graphRow, "uid")
		identity, expected := want[uid]
		if !expected {
			return nil, fmt.Errorf("unexpected graph identity")
		}
		if _, duplicate := byUID[uid]; duplicate {
			return nil, fmt.Errorf("duplicate graph identity")
		}
		row := cloudResourceRowFromGraph(graphRow)
		if row.ID != uid || row.ResourceType != identity.ResourceType {
			return nil, fmt.Errorf("graph identity differs from owner ledger")
		}
		byUID[uid] = row
	}
	results := make([]CloudResourceRow, 0, len(identities))
	for _, identity := range identities {
		row, ok := byUID[identity.UID]
		if !ok {
			return nil, fmt.Errorf("graph identity is missing")
		}
		results = append(results, row)
	}
	return results, nil
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
