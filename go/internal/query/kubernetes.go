// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	kubernetesCorrelationsCapability = "kubernetes.correlations.list"
	kubernetesCorrelationMaxLimit    = 200
)

// KubernetesHandler exposes reducer-owned Kubernetes correlation reads (issue
// #388, PR2). It is a bounded, paginated, read-only surface over the durable
// reducer_kubernetes_correlation facts produced by PR1; it performs no graph
// writes and adds no reducer logic.
type KubernetesHandler struct {
	Correlations KubernetesCorrelationStore
	Profile      QueryProfile
}

// KubernetesCorrelationResult is one reducer-owned Kubernetes correlation row.
// The field order matches KubernetesCorrelationRow so the handler can convert
// rows directly. Outcome is the issue #388 six-outcome contract value (exact,
// derived, ambiguous, unresolved, stale, rejected).
type KubernetesCorrelationResult struct {
	CorrelationID          string   `json:"correlation_id"`
	ClusterID              string   `json:"cluster_id,omitempty"`
	WorkloadObjectID       string   `json:"workload_object_id,omitempty"`
	Namespace              string   `json:"namespace,omitempty"`
	WorkloadName           string   `json:"workload_name,omitempty"`
	WorkloadUID            string   `json:"workload_uid,omitempty"`
	ImageRef               string   `json:"image_ref,omitempty"`
	SourceDigest           string   `json:"source_digest,omitempty"`
	JoinMode               string   `json:"join_mode,omitempty"`
	IdentityEdgeKey        string   `json:"identity_edge_key,omitempty"`
	RelationshipType       string   `json:"relationship_type,omitempty"`
	Outcome                string   `json:"outcome"`
	DriftKind              string   `json:"drift_kind,omitempty"`
	Reason                 string   `json:"reason,omitempty"`
	NonPromotion           string   `json:"non_promotion,omitempty"`
	ProvenanceOnly         bool     `json:"provenance_only"`
	CandidateSourceDigests []string `json:"candidate_source_digests,omitempty"`
	Warnings               []string `json:"warnings,omitempty"`
	EvidenceFactIDs        []string `json:"evidence_fact_ids,omitempty"`
}

// Mount registers Kubernetes query routes.
func (h *KubernetesHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/kubernetes/correlations", h.listCorrelations)
}

func (h *KubernetesHandler) profile() QueryProfile {
	if h == nil || h.Profile == "" {
		return ProfileProduction
	}
	return h.Profile
}

func (h *KubernetesHandler) listCorrelations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryKubernetesCorrelations,
		"GET /api/v0/kubernetes/correlations",
		kubernetesCorrelationsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), kubernetesCorrelationsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"kubernetes correlations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability,
			kubernetesCorrelationsCapability,
			h.profile(),
			requiredProfile(kubernetesCorrelationsCapability),
		)
		return
	}
	limit, ok := requiredKubernetesCorrelationLimit(w, r)
	if !ok {
		return
	}
	filter := KubernetesCorrelationFilter{
		ScopeID:            QueryParam(r, "scope_id"),
		ClusterID:          QueryParam(r, "cluster_id"),
		WorkloadObjectID:   QueryParam(r, "workload_object_id"),
		Namespace:          QueryParam(r, "namespace"),
		ImageRef:           QueryParam(r, "image_ref"),
		SourceDigest:       QueryParam(r, "source_digest"),
		Outcome:            QueryParam(r, "outcome"),
		DriftKind:          QueryParam(r, "drift_kind"),
		AfterCorrelationID: QueryParam(r, "after_correlation_id"),
		Limit:              limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, cluster_id, workload_object_id, namespace, image_ref, or source_digest is required")
		return
	}
	// Access scoping (#5167 Group B): reducer_kubernetes_correlation facts
	// carry no repository grant of their own, and hasScope() above accepts
	// any single anchor (e.g. namespace alone), so an unscoped filter could
	// otherwise fan out across every tenant's ingestion scope. A scoped
	// caller with no granted repository or ingestion scope never reaches the
	// store (#5137 LiveActivityStore precedent); a granted scoped caller's
	// rows are additionally bound to its grant in ListKubernetesCorrelations.
	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		h.writeEmptyKubernetesCorrelations(w, r, limit)
		return
	}
	filter.AllScopes = !access.scoped()
	filter.AllowedRepositoryIDs = access.grantedRepositoryIDs()
	filter.AllowedScopeIDs = access.grantedScopeIDs()
	if h.Correlations == nil {
		WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"kubernetes correlations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable,
			kubernetesCorrelationsCapability,
			h.profile(),
			requiredProfile(kubernetesCorrelationsCapability),
		)
		return
	}

	rows, err := h.Correlations.ListKubernetesCorrelations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]KubernetesCorrelationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, KubernetesCorrelationResult(row))
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
		kubernetesCorrelationsCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned Kubernetes correlation facts; a live workload stays provenance-only unless the image digest or owner edge is exact",
	))
}

// writeEmptyKubernetesCorrelations returns the bounded empty correlations page
// for a scoped caller with no granted repository or ingestion scope, without
// querying Postgres (#5167 Group B, #5137 LiveActivityStore precedent).
func (h *KubernetesHandler) writeEmptyKubernetesCorrelations(w http.ResponseWriter, r *http.Request, limit int) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"correlations": []KubernetesCorrelationResult{},
		"count":        0,
		"limit":        limit,
		"truncated":    false,
	}, BuildTruthEnvelope(
		h.profile(),
		kubernetesCorrelationsCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; kubernetes correlations are empty",
	))
}

func requiredKubernetesCorrelationLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > kubernetesCorrelationMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", kubernetesCorrelationMaxLimit))
		return 0, false
	}
	return limit, true
}
