// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *CloudRuntimeDriftHandler) getDriftPacket(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCloudRuntimeDriftFindings,
		"GET /api/v0/investigations/drift/packet",
		cloudRuntimeDriftReadbackCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), cloudRuntimeDriftReadbackCapability) {
		h.writeContractError(
			w, r,
			http.StatusNotImplemented,
			"drift packets require reducer-materialized provider-neutral drift facts",
			ErrorCodeUnsupportedCapability,
		)
		return
	}
	req := cloudRuntimeDriftRequest{
		ScopeID:          QueryParam(r, "scope_id"),
		AccountID:        QueryParam(r, "account_id"),
		ProjectID:        QueryParam(r, "project_id"),
		SubscriptionID:   QueryParam(r, "subscription_id"),
		Provider:         QueryParam(r, "provider"),
		CloudResourceUID: QueryParam(r, "cloud_resource_uid"),
		Limit:            cloudRuntimeDriftMaxLimit,
	}
	filter, err := normalizeCloudRuntimeDriftRequest(req)
	if err != nil {
		h.writeContractError(w, r, http.StatusBadRequest, err.Error(), ErrorCodeInvalidArgument)
		return
	}
	// Drift findings key on a cloud ingestion scope_id (AWS account, GCP
	// project, or Azure subscription), not a git repository (#5167 W5): there
	// is no repository-to-cloud-scope map on this path, the same reason
	// GET /api/v0/replatforming/selectors binds AllowedScopeIDs directly
	// (replatforming_selectors_handler.go). A scoped token must carry an
	// exact grant for filter.ScopeID; an empty grant or a scope_id outside
	// the grant returns the same scope_not_found refusal packet the sibling
	// investigation packet routes use for an unresolved anchor
	// (getImpactPacket, getDeployableUnitPacket), without reading the drift
	// finding store.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.scoped() && !access.allowsDirectScopeID(filter.ScopeID) {
		packet, err := refusalPacketForAPI(InvestigationFamilyDrift, PacketRefusalScopeNotFound)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeInvestigationPacket(w, r, packet)
		return
	}
	if h == nil || h.Store == nil {
		h.writeContractError(
			w, r,
			http.StatusNotImplemented,
			"drift packets require the reducer drift finding read model",
			ErrorCodeReadModelUnavailable,
		)
		return
	}
	rows, err := h.Store.ListActiveMultiCloudRuntimeDriftFindings(r.Context(), filter)
	if err != nil {
		h.writeContractError(w, r, http.StatusInternalServerError, "drift packet readback failed", ErrorCodeInternalError)
		return
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		cloudRuntimeDriftReadbackCapability,
		TruthBasisSemanticFacts,
		"resolved from active reducer-materialized provider-neutral runtime drift findings (reducer_multi_cloud_runtime_drift_finding)",
	)
	subject := map[string]string{"scope_id": filter.ScopeID}
	addSubjectKey(subject, "provider", filter.Provider)
	addSubjectKey(subject, "cloud_resource_uid", filter.CloudResourceUID)
	packet, err := BuildDriftPacket(cloudRuntimeDriftFindingViews(rows), subject, truth, packetBoundsFromRequest(r))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeInvestigationPacket(w, r, packet)
}
