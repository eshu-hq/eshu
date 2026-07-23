// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) explainImpact(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactExplanation,
		"GET /api/v0/supply-chain/impact/explain",
		supplyChainImpactExplanationCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), supplyChainImpactExplanationCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact explanations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			supplyChainImpactExplanationCapability,
			h.profile(),
			requiredProfile(supplyChainImpactExplanationCapability),
		)
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, impactExplanationScannerFilters()) {
		return
	}
	// Resolve scoped-token grants before any repository-selector, reducer, or
	// readiness store read (#5167 W5). An empty grant returns the bounded
	// no-evidence explanation without touching those stores so a scoped
	// caller with no authorized repositories cannot probe cross-tenant
	// findings, mirroring writeEmptyImpactFindingsPage's sibling routes.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyImpactExplanation(w, r)
		return
	}
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, QueryParam(r, "repository_id"), access, supplyChainImpactExplanationCapability)
	if !ok {
		return
	}
	filter := trimSupplyChainImpactExplanationFilter(SupplyChainImpactExplanationFilter{
		FindingID:     QueryParam(r, "finding_id"),
		AdvisoryID:    QueryParam(r, "advisory_id"),
		CVEID:         QueryParam(r, "cve_id"),
		PackageID:     QueryParam(r, "package_id"),
		RepositoryID:  repositoryID,
		SubjectDigest: QueryParam(r, "subject_digest"),
		ImageRef:      QueryParam(r, "image_ref"),
		WorkloadID:    QueryParam(r, "workload_id"),
		ServiceID:     QueryParam(r, "service_id"),
	})
	if access.scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	}
	if !filter.hasBoundedScope() {
		WriteError(w, http.StatusBadRequest, "finding_id, or advisory_id/cve_id plus package_id, repository_id, subject_digest, image_ref, workload_id, or service_id is required")
		return
	}
	if h.ImpactExplanations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact explanations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			supplyChainImpactExplanationCapability,
			h.profile(),
			requiredProfile(supplyChainImpactExplanationCapability),
		)
		return
	}

	row, err := h.ImpactExplanations.ExplainSupplyChainImpact(r.Context(), filter)
	if errors.Is(err, ErrSupplyChainImpactExplanationNotFound) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.readinessScope(), nil, false)
		body := BuildSupplyChainImpactNoEvidenceExplanation(filter, readiness)
		WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
			h.profile(),
			supplyChainImpactExplanationCapability,
			TruthBasisSemanticFacts,
			"no reducer-owned impact finding matched the bounded explanation scope; readiness explains missing evidence",
		))
		return
	}
	if errors.Is(err, ErrSupplyChainImpactExplanationAmbiguous) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.readinessScope(), nil, false)
		body := BuildSupplyChainImpactAmbiguousExplanation(
			filter,
			readiness,
			supplyChainImpactExplanationAmbiguousCandidateCount(err),
		)
		WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
			h.profile(),
			supplyChainImpactExplanationCapability,
			TruthBasisSemanticFacts,
			"bounded explanation scope matched multiple reducer-owned impact findings; provide finding_id or a narrower advisory/package/repository/image/workload/service scope",
		))
		return
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	scope := findingReadinessScope(row.Finding, filter)
	findingResult := SupplyChainImpactFindingResult(row.Finding)
	readiness := h.readSupplyChainImpactReadinessForScope(r, scope, []SupplyChainImpactFindingResult{findingResult}, false)
	body := BuildSupplyChainImpactExplanation(filter, row, readiness)
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactExplanationCapability,
		TruthBasisSemanticFacts,
		"resolved from one reducer-owned impact finding and its bounded evidence fact ids; reachability and deployment anchors are reported only when evidence exists",
	))
}

func (h *SupplyChainHandler) readSupplyChainImpactReadinessForScope(
	r *http.Request,
	scope SupplyChainImpactTargetScope,
	findings []SupplyChainImpactFindingResult,
	truncated bool,
) SupplyChainImpactReadinessEnvelope {
	snapshot, err := h.readSupplyChainImpactReadinessSnapshot(r, scope)
	if err != nil {
		return BuildSupplyChainImpactReadinessUnavailable(scope, findings, truncated)
	}
	return BuildSupplyChainImpactReadiness(scope, findings, truncated, snapshot)
}
