// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

func (h *SupplyChainHandler) listImpactFindings(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySupplyChainImpactFindings,
		"GET /api/v0/supply-chain/impact/findings",
		SupplyChainImpactFindingsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), SupplyChainImpactFindingsCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"supply-chain impact findings require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			SupplyChainImpactFindingsCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactFindingsCapability),
		)
		return
	}
	limit, ok := impact.RequiredSupplyChainImpactFindingLimit(w, r)
	if !ok {
		return
	}
	profile, ok := impact.RequestedSupplyChainImpactProfile(w, r)
	if !ok {
		return
	}
	if !impact.RejectUnsupportedVulnerabilityScannerFilters(w, r, impact.ImpactFindingsScannerFilters()) {
		return
	}
	advisoryID := querycontract.QueryParam(r, "advisory_id")
	if advisoryID == "" {
		advisoryID = impact.FirstNonEmptyQueryParam(r, "ghsa_id", "osv_id")
	}
	severity, ok := impact.ParseSupplyChainScannerSeverity(w, r)
	if !ok {
		return
	}
	priorityBucket, minPriorityScore, sort, err := impact.SupplyChainImpactPriorityFilter(r)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	suppressionState := querycontract.QueryParam(r, "suppression_state")
	if suppressionState != "" && !impact.IsSupportedSupplyChainSuppressionState(suppressionState) {
		querycontract.WriteError(w, http.StatusBadRequest, "suppression_state must be one of active, not_affected, accepted_risk, false_positive, ignored, expired, provider_dismissed, scope_mismatch")
		return
	}
	includeSuppressed, ok := impact.ParseSupplyChainImpactIncludeSuppressed(w, r)
	if !ok {
		return
	}
	// Resolve scoped-token grants before any reducer or readiness store read.
	// An empty grant returns the bounded zero-findings page without touching
	// the impact, readiness, or repository-selector stores so a scoped caller
	// with no authorized repositories cannot probe cross-tenant evidence.
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if access.Empty() {
		h.writeEmptyImpactFindingsPage(w, r, limit, profile)
		return
	}
	repositoryID, ok := h.resolveSupplyChainImpactRepositorySelector(w, r, querycontract.QueryParam(r, "repository_id"), access, SupplyChainImpactFindingsCapability)
	if !ok {
		return
	}
	filter := impact.SupplyChainImpactFindingFilter{
		CVEID:             querycontract.QueryParam(r, "cve_id"),
		AdvisoryID:        advisoryID,
		PackageID:         querycontract.QueryParam(r, "package_id"),
		RepositoryID:      repositoryID,
		SubjectDigest:     querycontract.QueryParam(r, "subject_digest"),
		ImageRef:          querycontract.QueryParam(r, "image_ref"),
		ImpactStatus:      querycontract.QueryParam(r, "impact_status"),
		Ecosystem:         querycontract.QueryParam(r, "ecosystem"),
		WorkloadID:        querycontract.QueryParam(r, "workload_id"),
		ServiceID:         querycontract.QueryParam(r, "service_id"),
		Environment:       querycontract.QueryParam(r, "environment"),
		Severity:          severity,
		DetectionProfile:  impact.FilterProfile(profile),
		PriorityBucket:    priorityBucket,
		MinPriorityScore:  minPriorityScore,
		Sort:              sort,
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
		AfterFindingID:    querycontract.QueryParam(r, "after_finding_id"),
		Limit:             limit + 1,
	}
	if access.Scoped() {
		filter.AllowedRepositoryIDs = append([]string(nil), access.AllowedRepositoryIDs...)
		filter.AllowedScopeIDs = append([]string(nil), access.AllowedScopeIDs...)
	}
	if !filter.HasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "cve_id, advisory_id, package_id, repository_id, subject_digest, image_ref, impact_status, ecosystem, workload_id, service_id, environment, severity, priority_bucket, or min_priority_score > 0 is required")
		return
	}
	if h.ImpactFindings == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"supply-chain impact findings require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			SupplyChainImpactFindingsCapability,
			h.profile(),
			querycontract.RequiredProfile(SupplyChainImpactFindingsCapability),
		)
		return
	}

	rows, err := h.ImpactFindings.ListSupplyChainImpactFindings(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	// #5452: promote findings whose subject digest is observed running on a
	// live cloud resource (ECS task / image-package Lambda) to the
	// runtime_confirmed deployment_truth_tier, naming the running resource. The
	// indexed owner-ledger read is current-inventory + scope authorized (stale
	// or cross-scope resources never promote a finding) and bounded to the
	// page's digests. A probe error fails the request rather than serving a
	// false config_only tier for a vulnerability that is actually running.
	if err := h.applySupplyChainCloudRuntimeEvidence(r.Context(), access, rows); err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "supply-chain impact runtime evidence probe failed")
		return
	}
	// #5834: independently probe exact RUNS_IMAGE digest edges and gate each
	// graph candidate through current, caller-authorized workload-owner and edge
	// generations before allowing it to become live deployment evidence.
	if err := h.applySupplyChainKubernetesRuntimeEvidence(r.Context(), access, rows); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, SupplyChainImpactFindingsCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, "supply-chain impact kubernetes runtime evidence probe failed")
		return
	}
	// #5746: resolve each finding's runtime context (workloads, services,
	// deployments, environments, catalog refs) from its repository_id at READ
	// time and attach it as a labeled `runtime_context` block — never by
	// backfilling the baked workload_ids/service_ids/environments fields.
	// Filters resolve the same current mappings independently in SQL (#5747).
	// The probe fails loud on error: graph sentinels map to the
	// bounded retryable envelope (503/504) via querycontract.WriteGraphReadError, while
	// Postgres store errors fall to a plain 500 — serving an empty context
	// after a failed read would be indistinguishable from "nothing runs this"
	// on a security surface, so no failure path returns a false empty.
	if err := h.applySupplyChainRuntimeContext(r.Context(), rows, access); err != nil {
		if querycontract.WriteGraphReadError(w, r, err, SupplyChainImpactFindingsCapability) {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, "supply-chain impact runtime context probe failed")
		return
	}
	resolvedContextCount := 0
	resolvedWorkloadCount := 0
	for _, row := range rows {
		if row.RuntimeContext == nil {
			continue
		}
		resolvedContextCount++
		resolvedWorkloadCount += len(row.RuntimeContext.WorkloadIDs)
	}
	span.SetAttributes(
		attribute.Int("eshu.query.runtime_context_findings", resolvedContextCount),
		attribute.Int("eshu.query.runtime_context_workloads", resolvedWorkloadCount),
	)
	results := make([]impact.SupplyChainImpactFindingResult, 0, len(rows))
	for i := range rows {
		results = append(results, impact.BuildSupplyChainImpactFindingResult(&rows[i]))
	}
	scope := impact.SupplyChainImpactTargetScope{
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
	var readiness impact.SupplyChainImpactReadinessEnvelope
	if readinessErr != nil {
		// Readiness lookup failed (transient Postgres error, statement
		// timeout, etc.). Do not drop the already-fetched findings page:
		// return the findings with a `readiness_unavailable` envelope so
		// callers cannot misread zero findings as safe and can retry the
		// readiness lookup separately.
		readiness = impact.BuildSupplyChainImpactReadinessUnavailable(scope, results, truncated)
	} else {
		readiness = impact.BuildSupplyChainImpactReadiness(scope, results, truncated, snapshot)
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
	truth := querycontract.BuildTruthEnvelope(
		h.profile(),
		SupplyChainImpactFindingsCapability,
		querycontract.TruthBasisSemanticFacts,
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
	querycontract.WriteSuccess(w, r, http.StatusOK, body, truth)
}

// supplyChainImpactWinnersFreshnessReader is the optional capability the
// impact-findings store implements when it can report the maintained winners
// read-model watermark. The handler type-asserts it so the legacy store (or a
// test double) that does not implement it simply keeps the fresh envelope.
type supplyChainImpactWinnersFreshnessReader interface {
	SupplyChainImpactWinnersWatermark(context.Context) (impact.SupplyChainImpactWinnersFreshness, error)
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
func applyWinnersFreshness(truth *querycontract.TruthEnvelope, fr impact.SupplyChainImpactWinnersFreshness, probeErr error, now time.Time) {
	if truth == nil || !fr.ServingFromWinners {
		return
	}
	if probeErr != nil {
		truth.Freshness = querycontract.TruthFreshness{
			State:  querycontract.FreshnessUnavailable,
			Detail: "could not determine supply-chain impact winners read-model freshness",
		}
		return
	}
	if !fr.Present {
		// No maintainer watermark at all: the reducer has never reswept the read
		// model. A resweep that produced zero winners still stamps the watermark,
		// so this is the genuine never-populated case, not a zero-findings corpus.
		truth.Freshness = querycontract.TruthFreshness{
			State:  querycontract.FreshnessBuilding,
			Detail: "supply-chain impact winners read model has not been materialized by the reducer maintainer yet",
		}
		querycontract.WithFreshnessCause(truth, querycontract.FreshnessCauseReducerBacklog)
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
	truth.Freshness = querycontract.TruthFreshness{
		State:      querycontract.FreshnessStale,
		ObservedAt: observedAt,
		Detail:     "supply-chain impact winners read model is behind its maintainer resweep cadence",
	}
	querycontract.WithFreshnessCause(truth, querycontract.FreshnessCauseReducerBacklog)
}

func (h *SupplyChainHandler) readSupplyChainImpactReadinessSnapshot(
	r *http.Request,
	scope impact.SupplyChainImpactTargetScope,
) (impact.SupplyChainImpactReadinessSnapshot, error) {
	if h.Readiness == nil {
		return impact.SupplyChainImpactReadinessSnapshot{}, nil
	}
	return h.Readiness.ReadSupplyChainImpactReadiness(r.Context(), impact.SupplyChainImpactReadinessQuery(scope))
}
