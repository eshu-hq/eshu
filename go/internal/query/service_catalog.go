package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	serviceCatalogCorrelationsCapability = "service_catalog.correlations.list"
	serviceCatalogCorrelationMaxLimit    = 200
)

// ServiceCatalogHandler exposes reducer-owned service catalog correlation reads.
type ServiceCatalogHandler struct {
	Content      ContentStore
	Correlations ServiceCatalogCorrelationStore
	Profile      QueryProfile
}

// ServiceCatalogCorrelationResult is one reducer-owned catalog correlation row.
type ServiceCatalogCorrelationResult struct {
	CorrelationID   string   `json:"correlation_id"`
	Provider        string   `json:"provider,omitempty"`
	EntityRef       string   `json:"entity_ref,omitempty"`
	EntityType      string   `json:"entity_type,omitempty"`
	DisplayName     string   `json:"display_name,omitempty"`
	RepositoryID    string   `json:"repository_id,omitempty"`
	ServiceID       string   `json:"service_id,omitempty"`
	WorkloadID      string   `json:"workload_id,omitempty"`
	OwnerRef        string   `json:"owner_ref,omitempty"`
	Lifecycle       string   `json:"lifecycle,omitempty"`
	Tier            string   `json:"tier,omitempty"`
	Outcome         string   `json:"outcome"`
	Reason          string   `json:"reason,omitempty"`
	ProvenanceOnly  bool     `json:"provenance_only"`
	DriftKind       string   `json:"drift_kind,omitempty"`
	DriftStatus     string   `json:"drift_status,omitempty"`
	EvidenceFactIDs []string `json:"evidence_fact_ids,omitempty"`
}

// Mount registers service catalog query routes.
func (h *ServiceCatalogHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/service-catalog/correlations", h.listCorrelations)
}

func (h *ServiceCatalogHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *ServiceCatalogHandler) listCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryServiceCatalogCorrelations,
		"GET /api/v0/service-catalog/correlations",
		serviceCatalogCorrelationsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), serviceCatalogCorrelationsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service catalog correlations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			serviceCatalogCorrelationsCapability,
			h.profile(),
			requiredProfile(serviceCatalogCorrelationsCapability),
		)
		return
	}
	limit, ok := requiredServiceCatalogCorrelationLimit(w, r)
	if !ok {
		return
	}
	repositoryID, ok := resolveRepositorySelectorForRequest(w, r, nil, h.Content, QueryParam(r, "repository_id"))
	if !ok {
		return
	}
	filter := ServiceCatalogCorrelationFilter{
		ScopeID:            QueryParam(r, "scope_id"),
		Provider:           QueryParam(r, "provider"),
		EntityRef:          QueryParam(r, "entity_ref"),
		RepositoryID:       repositoryID,
		ServiceID:          QueryParam(r, "service_id"),
		WorkloadID:         QueryParam(r, "workload_id"),
		OwnerRef:           QueryParam(r, "owner_ref"),
		Outcome:            QueryParam(r, "outcome"),
		DriftStatus:        QueryParam(r, "drift_status"),
		AfterCorrelationID: QueryParam(r, "after_correlation_id"),
		Limit:              limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, entity_ref, repository_id, service_id, workload_id, or owner_ref is required")
		return
	}
	if h.Correlations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"service catalog correlations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			serviceCatalogCorrelationsCapability,
			h.profile(),
			requiredProfile(serviceCatalogCorrelationsCapability),
		)
		return
	}

	rows, err := h.Correlations.ListServiceCatalogCorrelations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]ServiceCatalogCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, ServiceCatalogCorrelationResult(row))
	}
	body := map[string]any{
		"correlations": results,
		"count":        len(results),
		"limit":        limit,
		"truncated":    truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_correlation_id": results[len(results)-1].CorrelationID,
		}
	}
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		serviceCatalogCorrelationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned service catalog correlation facts; catalog declarations remain provenance until corroborated",
	))
}

func requiredServiceCatalogCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > serviceCatalogCorrelationMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", serviceCatalogCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}
