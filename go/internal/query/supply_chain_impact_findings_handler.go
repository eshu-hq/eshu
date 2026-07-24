// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

func (h *SupplyChainHandler) listImpactFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactFindings,
		"GET /api/v0/supply-chain/impact/findings",
		supplyChainImpactFindingsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), supplyChainImpactFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact findings require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			supplyChainImpactFindingsCapability,
			h.profile(),
			requiredProfile(supplyChainImpactFindingsCapability),
		)
		return
	}
	limit, ok := requiredSupplyChainImpactFindingLimit(w, r)
	if !ok {
		return
	}
	profile, ok := requestedSupplyChainImpactProfile(w, r)
	if !ok {
		return
	}
	if !rejectUnsupportedVulnerabilityScannerFilters(w, r, impactFindingsScannerFilters()) {
		return
	}
	advisoryID := QueryParam(r, "advisory_id")
	if advisoryID == "" {
		advisoryID = firstNonEmptyQueryParam(r, "ghsa_id", "osv_id")
	}
	severity, ok := parseSupplyChainScannerSeverity(w, r)
	if !ok {
		return
	}
	priorityBucket, minPriorityScore, sort, err := supplyChainImpactPriorityFilter(r)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	suppressionState := QueryParam(r, "suppression_state")
	if suppressionState != "" && !isSupportedSupplyChainSuppressionState(suppressionState) {
		WriteError(w, http.StatusBadRequest, "suppression_state must be one of active, not_affected, accepted_risk, false_positive, ignored, expired, provider_dismissed, scope_mismatch")
		return
	}
	includeSuppressed, ok := parseSupplyChainImpactIncludeSuppressed(w, r)
	if !ok {
		return
	}
	// Resolve scoped-token grants before any reducer or readiness store read.
	// An empty grant returns the bounded zero-findings page without touching
	// the impact, readiness, or repository-selector stores so a scoped caller
	// with no authorized repositories cannot probe cross-tenant evidence.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyImpactFindingsPage(w, r, limit, profile)
		return
	}
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, QueryParam(r, "repository_id"), access, supplyChainImpactFindingsCapability)
	if !ok {
		return
	}
	filter := SupplyChainImpactFindingFilter{
		CVEID:             QueryParam(r, "cve_id"),
		AdvisoryID:        advisoryID,
		PackageID:         QueryParam(r, "package_id"),
		RepositoryID:      repositoryID,
		SubjectDigest:     QueryParam(r, "subject_digest"),
		ImageRef:          QueryParam(r, "image_ref"),
		ImpactStatus:      QueryParam(r, "impact_status"),
		Ecosystem:         QueryParam(r, "ecosystem"),
		WorkloadID:        QueryParam(r, "workload_id"),
		ServiceID:         QueryParam(r, "service_id"),
		Environment:       QueryParam(r, "environment"),
		Severity:          severity,
		DetectionProfile:  filterProfile(profile),
		PriorityBucket:    priorityBucket,
		MinPriorityScore:  minPriorityScore,
		Sort:              sort,
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
		AfterFindingID:    QueryParam(r, "after_finding_id"),
		Limit:             limit + 1,
	}
	if access.scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.allowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.allowedScopeIDs...)
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, package_id, repository_id, subject_digest, image_ref, impact_status, ecosystem, workload_id, service_id, environment, severity, priority_bucket, or min_priority_score > 0 is required")
		return
	}
	if h.ImpactFindings == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact findings require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			supplyChainImpactFindingsCapability,
			h.profile(),
			requiredProfile(supplyChainImpactFindingsCapability),
		)
		return
	}

	rows, err := h.ImpactFindings.ListSupplyChainImpactFindings(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SupplyChainImpactFindingResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, buildSupplyChainImpactFindingResult(row))
	}
	scope := SupplyChainImpactTargetScope{
		CVEID:         filter.CVEID,
		AdvisoryID:    filter.AdvisoryID,
		PackageID:     filter.PackageID,
		RepositoryID:  filter.RepositoryID,
		SubjectDigest: filter.SubjectDigest,
		ImageRef:      filter.ImageRef,
		Ecosystem:     filter.Ecosystem,
		WorkloadID:    filter.WorkloadID,
		ServiceID:     filter.ServiceID,
		Environment:   filter.Environment,
		Severity:      filter.Severity,
		ImpactStatus:  filter.ImpactStatus,
	}
	snapshot, readinessErr := h.readSupplyChainImpactReadinessSnapshot(r, scope)
	var readiness SupplyChainImpactReadinessEnvelope
	if readinessErr != nil {
		// Readiness lookup failed (transient Postgres error, statement
		// timeout, etc.). Do not drop the already-fetched findings page:
		// return the findings with a `readiness_unavailable` envelope so
		// callers cannot misread zero findings as safe and can retry the
		// readiness lookup separately.
		readiness = BuildSupplyChainImpactReadinessUnavailable(scope, results, truncated)
	} else {
		readiness = BuildSupplyChainImpactReadiness(scope, results, truncated, snapshot)
	}
	body := map[string]any{
		"findings":          results,
		"count":             len(results),
		"limit":             limit,
		"truncated":         truncated,
		"detection_profile": profile,
		"readiness":         readiness,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_finding_id": results[len(results)-1].FindingID,
		}
	}
	truth := BuildTruthEnvelope(
		h.profile(),
		supplyChainImpactFindingsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned impact facts; CVSS, EPSS, KEV, reachability, missing evidence, and readiness coverage remain separate",
	)
	// When the list is served from the maintained winners read model (#3389
	// Phase 2 gate), report its freshness from the maintainer watermark so a
	// resweep cadence lag, an unpopulated table, or a probe failure is never
	// served as fresh truth. The legacy live read is always current and leaves
	// the envelope fresh; the probe costs nothing there.
	if reader, ok := h.ImpactFindings.(supplyChainImpactWinnersFreshnessReader); ok {
		watermark, freshnessErr := reader.SupplyChainImpactWinnersWatermark(r.Context())
		applyWinnersFreshness(truth, watermark, freshnessErr, time.Now())
		if freshnessErr != nil {
			// The findings page already succeeded; only the freshness probe
			// failed. Record it for triage but still serve the page (with an
			// unavailable freshness state rather than a false fresh).
			span.RecordError(freshnessErr)
		}
	}
	span.SetAttributes(attribute.String("eshu.query.freshness_state", string(truth.Freshness.State)))
	WriteSuccess(w, r, http.StatusOK, body, truth)
}

// supplyChainImpactWinnersFreshnessReader is the optional capability the
// impact-findings store implements when it can report the maintained winners
// read-model watermark. The handler type-asserts it so the legacy store (or a
// test double) that does not implement it simply keeps the fresh envelope.
type supplyChainImpactWinnersFreshnessReader interface {
	SupplyChainImpactWinnersWatermark(context.Context) (SupplyChainImpactWinnersFreshness, error)
}

// supplyChainImpactWinnersFreshnessWindow bounds how long after the last winners
// resweep the read model is still considered fresh. The reducer maintainer
// resweeps on a short cadence (~30s) and stamps every row with one
// materialized_at, so a healthy watermark is always within roughly one cadence of
// now. The window allows several cadences of headroom for a slow resweep or a
// transient lease handoff; a watermark older than this means the maintainer is
// not keeping the read model current, so the read is reported stale
// (reducer_backlog) instead of served as fresh truth.
const supplyChainImpactWinnersFreshnessWindow = 2 * time.Minute

// applyWinnersFreshness downgrades the truth envelope when the impact-findings
// list is served from the maintained winners read model and that model is behind,
// unpopulated, or could not be probed. It is a no-op on the legacy live read
// (always current) and when the model is fresh. now is injected for deterministic
// tests.
func applyWinnersFreshness(truth *TruthEnvelope, fr SupplyChainImpactWinnersFreshness, probeErr error, now time.Time) {
	if truth == nil || !fr.ServingFromWinners {
		return
	}
	if probeErr != nil {
		truth.Freshness = TruthFreshness{
			State:  FreshnessUnavailable,
			Detail: "could not determine supply-chain impact winners read-model freshness",
		}
		return
	}
	if !fr.Present {
		// No maintainer watermark at all: the reducer has never reswept the read
		// model. A resweep that produced zero winners still stamps the watermark,
		// so this is the genuine never-populated case, not a zero-findings corpus.
		truth.Freshness = TruthFreshness{
			State:  FreshnessBuilding,
			Detail: "supply-chain impact winners read model has not been materialized by the reducer maintainer yet",
		}
		WithFreshnessCause(truth, FreshnessCauseReducerBacklog)
		return
	}
	materializedAt := fr.MaterializedAt.UTC()
	observedAt := materializedAt.Format(time.RFC3339)
	if now.UTC().Sub(materializedAt) <= supplyChainImpactWinnersFreshnessWindow {
		// Fresh, but surface the watermark so consumers see when the read model
		// was last resweep'd.
		truth.Freshness.ObservedAt = observedAt
		return
	}
	truth.Freshness = TruthFreshness{
		State:      FreshnessStale,
		ObservedAt: observedAt,
		Detail:     "supply-chain impact winners read model is behind its maintainer resweep cadence",
	}
	WithFreshnessCause(truth, FreshnessCauseReducerBacklog)
}

func (h *SupplyChainHandler) readSupplyChainImpactReadinessSnapshot(
	r *http.Request,
	scope SupplyChainImpactTargetScope,
) (SupplyChainImpactReadinessSnapshot, error) {
	if h.Readiness == nil {
		return SupplyChainImpactReadinessSnapshot{}, nil
	}
	return h.Readiness.ReadSupplyChainImpactReadiness(r.Context(), SupplyChainImpactReadinessQuery(scope))
}
