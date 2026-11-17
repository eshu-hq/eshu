// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/extraction"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	collectorExtractionReadinessListCapability   = "collector_extraction_readiness.list"
	collectorExtractionReadinessFamilyCapability = "collector_extraction_readiness.family"
	collectorExtractionReadinessSchemaVersion    = "eshu.collector_extraction_readiness.v1"
	collectorExtractionReadinessDefaultLimit     = 100
	collectorExtractionReadinessMaxLimit         = 500
	collectorExtractionReadinessTruthReason      = "resolved from the static collector extraction policy catalog; advisory and does not move code"
)

// CollectorExtractionReadinessHandler exposes the advisory collector extraction
// readiness checklist over the query API. The data is static policy
// classification computed from documented repository evidence; the handler reads
// no runtime, graph, or registry state.
type CollectorExtractionReadinessHandler struct {
	Profile QueryProfile
	// Reads serves the readiness catalog through the versioned #9669 G2
	// admission contract, never a direct call into the extraction static
	// catalog. A nil Reads keeps the standing static satisfier, so the
	// cmd wiring (which sets only Profile) and every existing caller
	// keep serving the real catalog unchanged while the dest side cites
	// the pin.
	Reads ExtractionReadinessReadAdmission
}

// reads resolves the pin the legs admit through: the injected admission
// when set, the static satisfier otherwise. The static default keeps
// the route serving when the handler is built without an explicit
// admission; it never invents rows beyond the real catalog.
func (h *CollectorExtractionReadinessHandler) reads() ExtractionReadinessReadAdmission {
	if h != nil && h.Reads != nil {
		return h.Reads
	}
	return extractionReadinessStaticReader{}
}

// CollectorExtractionReadinessListResponse is the API response for the full
// extraction readiness catalog.
type CollectorExtractionReadinessListResponse struct {
	SchemaVersion string                 `json:"schema_version"`
	Status        string                 `json:"status"`
	Families      []extraction.Readiness `json:"families"`
	Count         int                    `json:"count"`
	TotalCount    int                    `json:"total_count"`
	Limit         int                    `json:"limit"`
	Truncated     bool                   `json:"truncated"`
}

// CollectorExtractionReadinessFamilyResponse is the API response for one
// collector family's extraction readiness drilldown.
type CollectorExtractionReadinessFamilyResponse struct {
	SchemaVersion string               `json:"schema_version"`
	Status        string               `json:"status"`
	Family        extraction.Readiness `json:"family"`
}

// Mount registers the collector extraction readiness routes.
func (h *CollectorExtractionReadinessHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/collector-extraction-readiness", h.list)
	mux.HandleFunc("GET /api/v0/collector-extraction-readiness/{family}", h.getFamily)
}

func (h *CollectorExtractionReadinessHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	families := h.reads().Catalog()
	totalCount := len(families)
	truncated := totalCount > limit
	if truncated {
		families = families[:limit]
	}
	response := CollectorExtractionReadinessListResponse{
		SchemaVersion: collectorExtractionReadinessSchemaVersion,
		Status:        "available",
		Families:      families,
		Count:         len(families),
		TotalCount:    totalCount,
		Limit:         limit,
		Truncated:     truncated,
	}
	WriteSuccess(w, r, http.StatusOK, response, h.truth(collectorExtractionReadinessListCapability))
}

func (h *CollectorExtractionReadinessHandler) getFamily(w http.ResponseWriter, r *http.Request) {
	family := strings.TrimSpace(r.PathValue("family"))
	if family == "" {
		WriteContractError(
			w,
			r,
			http.StatusBadRequest,
			"family is required",
			ErrorCodeInvalidArgument,
			collectorExtractionReadinessFamilyCapability,
			h.profile(),
			h.profile(),
		)
		return
	}
	readiness, ok := h.reads().Lookup(scope.CollectorKind(family))
	if !ok {
		WriteContractError(
			w,
			r,
			http.StatusNotFound,
			"collector family is not tracked by the extraction policy",
			ErrorCodeNotFound,
			collectorExtractionReadinessFamilyCapability,
			h.profile(),
			h.profile(),
		)
		return
	}
	response := CollectorExtractionReadinessFamilyResponse{
		SchemaVersion: collectorExtractionReadinessSchemaVersion,
		Status:        "available",
		Family:        readiness,
	}
	WriteSuccess(w, r, http.StatusOK, response, h.truth(collectorExtractionReadinessFamilyCapability))
}

func (h *CollectorExtractionReadinessHandler) limit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(QueryParam(r, "limit"))
	if raw == "" {
		return collectorExtractionReadinessDefaultLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > collectorExtractionReadinessMaxLimit {
		WriteContractError(
			w,
			r,
			http.StatusBadRequest,
			fmt.Sprintf("limit must be between 1 and %d", collectorExtractionReadinessMaxLimit),
			ErrorCodeInvalidArgument,
			collectorExtractionReadinessListCapability,
			h.profile(),
			h.profile(),
		)
		return 0, false
	}
	return limit, true
}

func (h *CollectorExtractionReadinessHandler) truth(capability string) *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		capability,
		TruthBasisRuntimeState,
		collectorExtractionReadinessTruthReason,
	)
}

func (h *CollectorExtractionReadinessHandler) profile() QueryProfile {
	if h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}
