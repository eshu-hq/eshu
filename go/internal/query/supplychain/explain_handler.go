// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h *SupplyChainHandler) explainImpact(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactExplanation,
		"GET /api/v0/supply-chain/impact/explain",
		SupplyChainImpactExplanationCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SupplyChainImpactExplanationCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact explanations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SupplyChainImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactExplanationCapability),
		)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.ImpactExplanationScannerFilters()) {
		return
	}
	// Resolve scoped-token grants before any repository-selector, reducer, or
	// readiness store read (#5167 W5). An empty grant returns the bounded
	// no-evidence explanation without touching those stores so a scoped
	// caller with no authorized repositories cannot probe cross-tenant
	// findings, mirroring writeEmptyImpactFindingsPage's sibling routes.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyImpactExplanation(w, r)
		return
	}
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), access, SupplyChainImpactExplanationCapability)
	if !ok {
		return
	}
	filter := impact.TrimSupplyChainImpactExplanationFilter(impact.SupplyChainImpactExplanationFilter{
		FindingID:     querycontract.QueryParam(r, "finding_id"),
		AdvisoryID:    querycontract.QueryParam(r, "advisory_id"),
		CVEID:         querycontract.QueryParam(r, "cve_id"),
		PackageID:     querycontract.QueryParam(r, "package_id"),
		RepositoryID:  repositoryID,
		SubjectDigest: querycontract.QueryParam(r, "subject_digest"),
		ImageRef:      querycontract.QueryParam(r, "image_ref"),
		WorkloadID:    querycontract.QueryParam(r, "workload_id"),
		ServiceID:     querycontract.QueryParam(r, "service_id"),
	})
	if access.Scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	}
	if !filter.HasBoundedScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "finding_id, or advisory_id/cve_id plus package_id, repository_id, subject_digest, image_ref, workload_id, or service_id is required")
		return
	}
	if h.ImpactExplanations == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact explanations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SupplyChainImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactExplanationCapability),
		)
		return
	}

	row, err := h.ImpactExplanations.ExplainSupplyChainImpact(r.Context(), filter)
	if errors.Is(err, impact.ErrSupplyChainImpactExplanationNotFound) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildSupplyChainImpactNoEvidenceExplanation(filter, readiness)
		querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
			h.profile(),
			SupplyChainImpactExplanationCapability,
			querycontract.TruthBasisSemanticFacts,
			"no reducer-owned impact finding matched the bounded explanation scope; readiness explains missing evidence",
		))
		return
	}
	if errors.Is(err, impact.ErrSupplyChainImpactExplanationAmbiguous) {
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildSupplyChainImpactAmbiguousExplanation(
			filter,
			readiness,
			impact.SupplyChainImpactExplanationAmbiguousCandidateCount(err),
		)
		querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
			h.profile(),
			SupplyChainImpactExplanationCapability,
			querycontract.TruthBasisSemanticFacts,
			"bounded explanation scope matched multiple reducer-owned impact findings; provide finding_id or a narrower advisory/package/repository/image/workload/service scope",
		))
		return
	}
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Resolve current, authorized runtime image evidence through the indexed
	// owner ledger before assembling the finding, so explain and list select the
	// same deployment and version-resolution tiers.
	rows := []impact.SupplyChainImpactFindingRow{row.Finding}
	if err := h.applySupplyChainCloudRuntimeEvidence(r.Context(), access, rows); err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "supply-chain impact runtime evidence probe failed")
		return
	}
	if err := h.applySupplyChainKubernetesRuntimeEvidence(r.Context(), access, rows); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, SupplyChainImpactExplanationCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, "supply-chain impact kubernetes runtime evidence probe failed")
		return
	}
	// Same shape, same root cause as the cloud-runtime probe above:
	// buildSupplyChainImpactFindingResult's SupplyChainImpactFindingResult(row)
	// conversion carries row.RuntimeContext straight through to the
	// runtime_context response field, but only the list route called
	// applySupplyChainRuntimeContext before assembling results -- explain
	// unconditionally omitted a finding's current workload/service/deployment
	// context even when list would show it. This probe type-asserts
	// h.ImpactFindings (the same store instance list uses, not
	// h.ImpactExplanations) for the optional supplyChainImpactRuntimeContextReader
	// capability and resolves at most one repository, well inside what list
	// already resolves for a full page.
	if err := h.applySupplyChainRuntimeContext(r.Context(), rows, access); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, SupplyChainImpactExplanationCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, "supply-chain impact runtime context probe failed")
		return
	}
	row.Finding = rows[0]

	scope := impact.FindingReadinessScope(row.Finding, filter)
	findingResult := impact.SupplyChainImpactFindingResult(row.Finding)
	readiness := h.readSupplyChainImpactReadinessForScope(r, scope, []impact.SupplyChainImpactFindingResult{findingResult}, false)
	body := impact.BuildSupplyChainImpactExplanation(filter, row, readiness)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		SupplyChainImpactExplanationCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from one reducer-owned impact finding and its bounded evidence fact ids; reachability and deployment anchors are reported only when evidence exists",
	))
}

func (h *SupplyChainHandler) readSupplyChainImpactReadinessForScope(
	r *http.Request,
	scope impact.SupplyChainImpactTargetScope,
	findings []impact.SupplyChainImpactFindingResult,
	truncated bool,
) impact.SupplyChainImpactReadinessEnvelope {
	snapshot, err := h.readSupplyChainImpactReadinessSnapshot(r, scope)
	if err != nil {
		return impact.BuildSupplyChainImpactReadinessUnavailable(scope, findings, truncated)
	}
	return impact.BuildSupplyChainImpactReadiness(scope, findings, truncated, snapshot)
}
