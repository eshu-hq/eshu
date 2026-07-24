// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

// RepositoryFreshnessReader reads the per-repository commit-receipt and
// build-completeness evidence backing GET
// /api/v0/repositories/{id}/freshness (#5143). It is a narrow port, separate
// from status.Reader, because it is keyed by a single already-resolved
// canonical repository id rather than composing the fixed RawSnapshot shape.
type RepositoryFreshnessReader interface {
	ReadRepositoryFreshness(ctx context.Context, repoID string) (status.RepositoryFreshnessSnapshot, error)
}

// getRepositoryFreshness answers "did eshu pick up my latest commit, and is
// the evidence fully built" for one repository (#5143). Repository
// selector resolution and scoped-token row filtering are handled by
// resolveRepositoryPathSelector, the same helper getRepositoryStats and
// getRepositoryStory use: a scoped caller with no grant on the resolved
// repository id gets the same 404 sibling repository routes already return,
// never a 403 that would confirm the repository's existence.
func (h *RepositoryHandler) getRepositoryFreshness(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Freshness == nil {
		WriteError(w, http.StatusServiceUnavailable, "repository freshness reader not configured")
		return
	}

	repoID, ok := h.resolveRepositoryPathSelector(w, r, "repository_freshness.status")
	if !ok {
		return
	}

	timer := startRepositoryQueryStage(r.Context(), h.Logger, "repository_freshness", repoID, "freshness_read")
	snapshot, err := h.Freshness.ReadRepositoryFreshness(r.Context(), repoID)
	timer.Done(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "read repository freshness failed: "+err.Error())
		return
	}

	expectedCommit := strings.TrimSpace(r.URL.Query().Get("expected_commit"))
	verdict := status.ComputeRepositoryFreshnessVerdict(snapshot, expectedCommit)

	repoRef, _, err := h.repositoryStatsRepositoryRef(r.Context(), repoID)
	if err != nil {
		if WriteGraphReadError(w, r, err, "repository_freshness.status") {
			return
		}
		WriteError(w, http.StatusInternalServerError, "query repository failed: "+err.Error())
		return
	}
	if repoRef == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	asOf := time.Now().UTC()
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		repositoryFreshnessToMap(repoRef, snapshot, verdict, asOf, scopedAuthContext(r.Context())),
		repositoryFreshnessTruth(h.profile(), verdict, asOf),
	)
}

// repositoryFreshnessTruth builds the truth envelope directly, mirroring
// freshnessCausalityTruth (freshness_causality_handler.go): this is a
// status-surface read composed from Postgres runtime state, not a
// graph/content capability gated by the capabilityMatrix BuildTruthEnvelope
// checks, so it is never registered there.
func repositoryFreshnessTruth(profile QueryProfile, verdict status.RepositoryFreshnessVerdict, asOf time.Time) *TruthEnvelope {
	return &TruthEnvelope{
		Level:      TruthLevelExact,
		Capability: "repository_freshness.status",
		Profile:    profile,
		Basis:      TruthBasisRuntimeState,
		Freshness: TruthFreshness{
			State:      FreshnessFresh,
			ObservedAt: asOf.Format(time.RFC3339),
		},
		Reason: "resolved from the repository's current active generation and queue state; verdict=" + string(verdict),
	}
}

// repositoryFreshnessToMap renders the freshness read model to the wire
// shape documented in issue #5143. Every field the underlying snapshot did
// not resolve is rendered explicitly (empty string, null, or false) rather
// than omitted, so the contract shape is stable across current, building,
// behind, unobserved, and unknown verdicts.
func repositoryFreshnessToMap(
	repo any,
	snapshot status.RepositoryFreshnessSnapshot,
	verdict status.RepositoryFreshnessVerdict,
	asOf time.Time,
	scoped bool,
) map[string]any {
	return map[string]any{
		"repository":           repo,
		"scope_id":             snapshot.ScopeID,
		"verdict":              string(verdict),
		"observed_commit":      snapshot.ObservedCommit,
		"observed_at":          nullableRFC3339(snapshot.ObservedAt),
		"generation":           repositoryFreshnessGenerationToMap(snapshot),
		"stages":               repositoryFreshnessStagesToMap(snapshot.Stages),
		"outstanding_by_stage": repositoryFreshnessOutstandingToSlice(snapshot.Outstanding),
		"shared_enrichment":    repositoryFreshnessSharedEnrichmentToMap(snapshot.SharedEnrichment),
		"unobserved_push":      repositoryFreshnessUnobservedPushToMap(snapshot.UnobservedPush),
		"as_of":                asOf.Format(time.RFC3339),
		"scoped":               scoped,
	}
}

func repositoryFreshnessGenerationToMap(snapshot status.RepositoryFreshnessSnapshot) map[string]any {
	if !snapshot.HasGeneration {
		return nil
	}
	return map[string]any{
		"id":           snapshot.Generation.ID,
		"status":       snapshot.Generation.Status,
		"trigger_kind": snapshot.Generation.TriggerKind,
		"is_delta":     snapshot.Generation.IsDelta,
		"activated_at": nullableRFC3339(snapshot.Generation.ActivatedAt),
	}
}

func repositoryFreshnessStagesToMap(stages status.RepositoryFreshnessStages) map[string]any {
	return map[string]any{
		"collected":    stages.Collected,
		"reduced":      stages.Reduced,
		"projected":    stages.Projected,
		"materialized": stages.Materialized,
	}
}

func repositoryFreshnessOutstandingToSlice(rows []status.RepositoryFreshnessOutstanding) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"stage":  row.Stage,
			"status": row.Status,
			"count":  row.Count,
		})
	}
	return result
}

func repositoryFreshnessSharedEnrichmentToMap(enrichment status.RepositoryFreshnessSharedEnrichment) map[string]any {
	domains := make([]map[string]any, 0, len(enrichment.PendingDomains))
	for _, domain := range enrichment.PendingDomains {
		domains = append(domains, map[string]any{
			"domain": domain.Domain,
			"count":  domain.Count,
		})
	}
	return map[string]any{
		"pending":         enrichment.Pending,
		"pending_domains": domains,
	}
}

func repositoryFreshnessUnobservedPushToMap(push *status.RepositoryFreshnessUnobservedPush) map[string]any {
	if push == nil {
		return nil
	}
	return map[string]any{
		"target_sha":  push.TargetSHA,
		"ref":         push.Ref,
		"received_at": nullableRFC3339(push.ReceivedAt),
	}
}
