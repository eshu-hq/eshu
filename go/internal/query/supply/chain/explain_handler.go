// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

// supplyChainImpactExplanationOperation names the log.Operation attribute on
// every stage event this route emits (query_timing.go).
const supplyChainImpactExplanationOperation = "supply_chain_impact_explanation_read"

// failExplanationRead answers a failed explanation store read or runtime
// probe: a reader-fence verdict becomes the retryable 503, and any other
// error becomes a handler-owned 500 carrying msg with the stage_failed
// signal (#7549).
func failExplanationRead(w http.ResponseWriter, r *http.Request, span trace.Span, timer supplyChainQueryStageTimer, err error, msg string) {
	if querycontract.WriteGraphReadError(w, r, err, ImpactExplanationCapability) {
		return
	}
	failStage(r.Context(), span, timer, err)
	querycontract.WriteError(w, http.StatusInternalServerError, msg)
}

func (h *Handler) explainImpact(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactExplanation,
		"GET /api/v0/supply-chain/impact/explain",
		ImpactExplanationCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), ImpactExplanationCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact explanations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			ImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(ImpactExplanationCapability),
		)
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.ExplanationScannerFilters()) {
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
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), access, ImpactExplanationCapability)
	if !ok {
		return
	}
	filter := impact.TrimExplanationFilter(impact.ExplanationFilter{
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
			ImpactExplanationCapability,
			h.profile(),
			querycontract.RequiredProfile(ImpactExplanationCapability),
		)
		return
	}

	explanationTimer := startSupplyChainQueryStage(r.Context(), h.Logger, supplyChainImpactExplanationOperation, filter.RepositoryID, "impact_explanation_query")
	row, err := h.ImpactExplanations.ExplainSupplyChainImpact(r.Context(), filter)
	if errors.Is(err, impact.ErrExplanationNotFound) {
		explanationTimer.Done(r.Context(), slog.Bool("error", false))
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildNoEvidenceExplanation(filter, readiness)
		querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
			h.profile(),
			ImpactExplanationCapability,
			querycontract.TruthBasisSemanticFacts,
			"no reducer-owned impact finding matched the bounded explanation scope; readiness explains missing evidence",
		))
		return
	}
	if errors.Is(err, impact.ErrExplanationAmbiguous) {
		explanationTimer.Done(r.Context(), slog.Bool("error", false))
		readiness := h.readSupplyChainImpactReadinessForScope(r, filter.ReadinessScope(), nil, false)
		body := impact.BuildAmbiguousExplanation(
			filter,
			readiness,
			impact.ExplanationAmbiguousCandidateCount(err),
		)
		querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
			h.profile(),
			ImpactExplanationCapability,
			querycontract.TruthBasisSemanticFacts,
			"bounded explanation scope matched multiple reducer-owned impact findings; provide finding_id or a narrower advisory/package/repository/image/workload/service scope",
		))
		return
	}
	explanationTimer.Done(r.Context(), slog.Bool("error", err != nil))
	if err != nil {
		// #7549: a stale or timed-out guarded PostgreSQL reader answers the
		// retryable 503 envelope, like the sibling probes below. The mapped
		// verdict is not a handler-owned 500, so it returns before failStage.
		failExplanationRead(w, r, span, explanationTimer, err, err.Error())
		return
	}

	// Resolve current, authorized runtime image evidence through the indexed
	// owner ledger before assembling the finding, so explain and list select the
	// same deployment and version-resolution tiers.
	rows := []impact.FindingRow{row.Finding}
	cloudRuntimeTimer := startSupplyChainQueryStage(r.Context(), h.Logger, supplyChainImpactExplanationOperation, filter.RepositoryID, "cloud_runtime_evidence")
	cloudRuntimeErr := h.applySupplyChainCloudRuntimeEvidence(r.Context(), access, rows)
	cloudRuntimeTimer.Done(r.Context(), slog.Bool("error", cloudRuntimeErr != nil))
	if cloudRuntimeErr != nil {
		// #7549: this probe never called WriteGraphReadError, unlike the
		// findings route's cloud-runtime branch (#7548). Route it the same
		// way so a reader fence failure answers the retryable 503.
		failExplanationRead(w, r, span, cloudRuntimeTimer, cloudRuntimeErr, "supply-chain impact runtime evidence probe failed")
		return
	}
	k8sRuntimeTimer := startSupplyChainQueryStage(r.Context(), h.Logger, supplyChainImpactExplanationOperation, filter.RepositoryID, "kubernetes_runtime_evidence")
	k8sRuntimeErr := h.applySupplyChainKubernetesRuntimeEvidence(r.Context(), access, rows)
	k8sRuntimeTimer.Done(r.Context(), slog.Bool("error", k8sRuntimeErr != nil))
	if k8sRuntimeErr != nil {
		failExplanationRead(w, r, span, k8sRuntimeTimer, k8sRuntimeErr, "supply-chain impact kubernetes runtime evidence probe failed")
		return
	}
	// Same shape, same root cause as the cloud-runtime probe above:
	// buildSupplyChainImpactFindingResult's FindingResult(row)
	// conversion carries row.RuntimeContext straight through to the
	// runtime_context response field, but only the list route called
	// applySupplyChainRuntimeContext before assembling results -- explain
	// unconditionally omitted a finding's current workload/service/deployment
	// context even when list would show it. This probe type-asserts
	// h.ImpactFindings (the same store instance list uses, not
	// h.ImpactExplanations) for the optional supplyChainImpactRuntimeContextReader
	// capability and resolves at most one repository, well inside what list
	// already resolves for a full page.
	runtimeContextTimer := startSupplyChainQueryStage(r.Context(), h.Logger, supplyChainImpactExplanationOperation, filter.RepositoryID, "runtime_context")
	runtimeContextErr := h.applySupplyChainRuntimeContext(r.Context(), rows, access)
	runtimeContextTimer.Done(r.Context(), slog.Bool("error", runtimeContextErr != nil))
	if runtimeContextErr != nil {
		failExplanationRead(w, r, span, runtimeContextTimer, runtimeContextErr, "supply-chain impact runtime context probe failed")
		return
	}
	row.Finding = rows[0]

	scope := impact.FindingReadinessScope(row.Finding, filter)
	findingResult := impact.FindingResult(row.Finding)
	readiness := h.readSupplyChainImpactReadinessForScope(r, scope, []impact.FindingResult{findingResult}, false)
	body := impact.BuildExplanation(filter, row, readiness)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		ImpactExplanationCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from one reducer-owned impact finding and its bounded evidence fact ids; reachability and deployment anchors are reported only when evidence exists",
	))
}

func (h *Handler) readSupplyChainImpactReadinessForScope(
	r *http.Request,
	scope impact.TargetScope,
	findings []impact.FindingResult,
	truncated bool,
) impact.ReadinessEnvelope {
	snapshot, err := h.readSupplyChainImpactReadinessSnapshot(r, scope)
	if err != nil {
		return impact.BuildReadinessUnavailable(scope, findings, truncated)
	}
	return impact.BuildReadiness(scope, findings, truncated, snapshot)
}
