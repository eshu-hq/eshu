// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
	"github.com/eshu-hq/eshu/go/internal/query/queryspan"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	serviceCatalogCorrelationsCapability = "service_catalog.correlations.list"
)

// CatalogHandler exposes reducer-owned service catalog correlation reads.
type CatalogHandler struct {
	Content      querycontract.ContentStore
	Correlations CatalogCorrelationStore
	Profile      querycontract.QueryProfile
}

// CatalogCorrelationResult is one reducer-owned catalog correlation row.
type CatalogCorrelationResult struct {
	CorrelationID          string   `json:"correlation_id"`
	Provider               string   `json:"provider,omitempty"`
	EntityRef              string   `json:"entity_ref,omitempty"`
	EntityType             string   `json:"entity_type,omitempty"`
	DisplayName            string   `json:"display_name,omitempty"`
	RepositoryID           string   `json:"repository_id,omitempty"`
	ServiceID              string   `json:"service_id,omitempty"`
	WorkloadID             string   `json:"workload_id,omitempty"`
	OwnerRef               string   `json:"owner_ref,omitempty"`
	Lifecycle              string   `json:"lifecycle,omitempty"`
	Tier                   string   `json:"tier,omitempty"`
	Outcome                string   `json:"outcome"`
	Reason                 string   `json:"reason,omitempty"`
	ProvenanceOnly         bool     `json:"provenance_only"`
	DriftKind              string   `json:"drift_kind,omitempty"`
	DriftStatus            string   `json:"drift_status,omitempty"`
	CandidateRepositoryIDs []string `json:"candidate_repository_ids,omitempty"`
	EvidenceFactIDs        []string `json:"evidence_fact_ids,omitempty"`
	RequiredAnchorKeys     []string `json:"required_anchor_keys,omitempty"`
}

// CatalogMissingEvidence explains why an anchored service-catalog read
// could not return a matching reducer correlation row.
type CatalogMissingEvidence struct {
	Class  string `json:"class"`
	Reason string `json:"reason"`
}

// Mount registers service catalog query routes.
func (h *CatalogHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/service-catalog/correlations", h.listCorrelations)
}

func (h *CatalogHandler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *CatalogHandler) listCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := queryspan.StartHandlerSpanWith(
		queryspan.HandlerTracer(),
		r,
		telemetry.SpanQueryServiceCatalogCorrelations,
		"GET /api/v0/service-catalog/correlations",
		serviceCatalogCorrelationsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), serviceCatalogCorrelationsCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service catalog correlations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			serviceCatalogCorrelationsCapability,
			h.profile(),
			querycontract.RequiredProfile(serviceCatalogCorrelationsCapability),
		)
		return
	}
	limit, ok := requiredServiceCatalogCorrelationLimit(w, r)
	if !ok {
		return
	}
	filter := CatalogCorrelationFilter{
		ScopeID:            querycontract.QueryParam(r, "scope_id"),
		Provider:           querycontract.QueryParam(r, "provider"),
		EntityRef:          querycontract.QueryParam(r, "entity_ref"),
		RepositoryID:       querycontract.QueryParam(r, "repository_id"),
		ServiceID:          querycontract.QueryParam(r, "service_id"),
		WorkloadID:         querycontract.QueryParam(r, "workload_id"),
		OwnerRef:           querycontract.QueryParam(r, "owner_ref"),
		Outcome:            querycontract.QueryParam(r, "outcome"),
		DriftStatus:        querycontract.QueryParam(r, "drift_status"),
		AfterCorrelationID: querycontract.QueryParam(r, "after_correlation_id"),
		Limit:              limit + 1,
	}
	if !filter.HasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, entity_ref, repository_id, service_id, workload_id, or owner_ref is required")
		return
	}
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyServiceCatalogCorrelationPage(w, r, limit)
		return
	}
	repositoryID, ok := queryselector.ResolveForRequestWithAccess(
		w,
		r,
		nil,
		h.Content,
		filter.RepositoryID,
		access,
		serviceCatalogCorrelationsCapability,
	)
	if !ok {
		return
	}
	filter.RepositoryID = repositoryID
	filter = serviceCatalogCorrelationFilterWithRepositoryAccess(filter, access)
	if h.Correlations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"service catalog correlations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			serviceCatalogCorrelationsCapability,
			h.profile(),
			querycontract.RequiredProfile(serviceCatalogCorrelationsCapability),
		)
		return
	}

	rows, err := h.Correlations.ListServiceCatalogCorrelations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]CatalogCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, CatalogCorrelationResult(row))
	}
	body := map[string]any{
		"correlations":     results,
		"count":            len(results),
		"limit":            limit,
		"truncated":        truncated,
		"evidence_summary": h.serviceCatalogEvidenceSummary(r.Context(), repositoryID, results, truncated),
	}
	if missing := serviceCatalogMissingEvidence(filter, len(results)); len(missing) > 0 {
		body["missing_evidence"] = missing
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": results[len(results)-1].CorrelationID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		serviceCatalogCorrelationsCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned service catalog correlation facts; catalog declarations remain provenance until corroborated",
	))
}

func (h *CatalogHandler) writeEmptyServiceCatalogCorrelationPage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	body := map[string]any{
		"correlations": []CatalogCorrelationResult{},
		"count":        0,
		"limit":        limit,
		"truncated":    false,
		"evidence_summary": CatalogEvidenceSummary{
			LocalDescriptors: CatalogLocalDescriptorEvidence{
				State:  "not_checked",
				Reason: "repository_scope_required",
			},
			ExternalCatalogConfirmation: CatalogExternalCatalogEvidence{
				State:  "missing",
				Reason: "repository_scope_required",
			},
			Reason: "repository_scope_required",
		},
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		serviceCatalogCorrelationsCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned service catalog correlation facts; catalog declarations remain provenance until corroborated",
	))
}

func requiredServiceCatalogCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > querycontract.ServiceCatalogCorrelationMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", querycontract.ServiceCatalogCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}

func serviceCatalogCorrelationFilterWithRepositoryAccess(
	filter CatalogCorrelationFilter,
	access querycontract.RepositoryAccessFilter,
) CatalogCorrelationFilter {
	if !access.Scoped() {
		return filter
	}
	filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
	filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	return filter
}

func serviceCatalogMissingEvidence(
	filter CatalogCorrelationFilter,
	resultCount int,
) []CatalogMissingEvidence {
	if resultCount > 0 || filter.AfterCorrelationID != "" || filter.Outcome != "" || filter.DriftStatus != "" {
		return nil
	}
	missing := make([]CatalogMissingEvidence, 0, 3)
	appendMissing := func(class, reason string) {
		missing = append(missing, CatalogMissingEvidence{Class: class, Reason: reason})
	}
	if filter.RepositoryID != "" {
		appendMissing(
			"repository_service_catalog_correlation",
			"repository-scoped service catalog correlation evidence missing after repository selector resolution",
		)
	}
	if filter.ServiceID != "" {
		appendMissing("service_catalog_correlation", "service-scoped service catalog correlation evidence missing")
	}
	if filter.WorkloadID != "" {
		appendMissing("workload_service_catalog_correlation", "workload-scoped service catalog correlation evidence missing")
	}
	if len(missing) > 0 {
		return missing
	}
	if filter.EntityRef != "" {
		appendMissing("entity_service_catalog_correlation", "catalog entity correlation evidence missing")
	}
	if filter.OwnerRef != "" {
		appendMissing("owner_service_catalog_correlation", "catalog owner correlation evidence missing")
	}
	if len(missing) > 0 {
		return missing
	}
	if filter.ScopeID != "" {
		appendMissing("scope_service_catalog_correlation", "scope-scoped service catalog correlation evidence missing")
	}
	return missing
}
