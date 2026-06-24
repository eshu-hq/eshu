// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintelhttp

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// ReportHandler serves the service intelligence report route. It reuses the
// existing EntityHandler to build the service-story dossier and truth (via the
// BuildServiceStoryEnvelope seam), then composes the report with the deterministic
// serviceintel composer. It holds the EntityHandler by pointer so it inherits the
// same dependencies, profile, scoped-token, and capability semantics as the
// service-story route.
type ReportHandler struct {
	// Entities builds the underlying service-story dossier and truth.
	Entities *query.EntityHandler
	// Incidents is the optional durable incident evidence source. When nil the
	// incidents_support section stays unsupported with its fallback next call;
	// when wired the handler appends a sourced incidents section. The source owns
	// its own operator logging.
	Incidents IncidentEvidenceSource
	// SupplyChain is the optional durable supply-chain evidence source. When nil
	// the supply_chain section stays unsupported with its fallback next call;
	// when wired the handler appends a sourced supply-chain section. The source
	// owns its own operator logging.
	SupplyChain SupplyChainEvidenceSource
}

// Mount registers the service intelligence report route.
func (h *ReportHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/services/{service_name}/intelligence-report", h.getServiceIntelligenceReport)
}

// getServiceIntelligenceReport composes and returns a service intelligence report
// for the named service. It runs no LLM path: it builds the service-story dossier
// through the query seam, adapts it with serviceintel.FromServiceStory, and
// composes the report. Resolution failures (capability gate, no repository access,
// ambiguous or missing service, internal errors) are returned as the same error
// envelope the service-story route returns, so the surfaces stay consistent.
//
// When durable evidence sources are wired, the handler appends the supply_chain
// and incidents_support sections from their own evidence lanes. Unwired, empty,
// or failed lanes stay unsupported with their fallback next calls. The composer
// keeps every section visible rather than hiding the unsourced ones.
func (h *ReportHandler) getServiceIntelligenceReport(w http.ResponseWriter, r *http.Request) {
	if h.Entities == nil {
		query.WriteError(w, http.StatusServiceUnavailable, "service intelligence report handler not configured")
		return
	}

	serviceName := query.PathParam(r, "service_name")
	dossier, truth, status, errEnv := h.Entities.BuildServiceStoryEnvelope(r.Context(), query.ServiceWorkloadSelector{
		ServiceName: serviceName,
		ServiceID:   query.QueryParam(r, "service_id"),
		Repository:  query.QueryParam(r, "repo"),
		Environment: query.QueryParam(r, "environment"),
	}, "service_intelligence_report")
	if errEnv != nil {
		query.WriteErrorEnvelope(w, r, status, errEnv)
		return
	}

	report := serviceintel.Compose(buildReportInput(r.Context(), dossier, truth, h.Incidents, h.SupplyChain))
	query.WriteSuccess(w, r, http.StatusOK, report, report.Truth)
}
