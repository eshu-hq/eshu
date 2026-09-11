// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// IAMPrivilegePostureObservationsCapability identifies the privilege
	// posture observation list route.
	IAMPrivilegePostureObservationsCapability = "secrets_iam.privilege_posture_observations.list" // #nosec G101 -- capability name identifier, not a credential
	// IAMSecretAccessPathsCapability identifies the secret access path list
	// route.
	IAMSecretAccessPathsCapability = "secrets_iam.secret_access_paths.list"
	// IAMPostureGapsCapability identifies the posture gap list route.
	IAMPostureGapsCapability = "secrets_iam.posture_gaps.list"
)

// IAMPrivilegePostureObservationResult is one reducer-owned privilege posture
// observation row. The field order matches IAMPrivilegePostureObservationRow so
// the handler can convert rows directly. State is the six-state secrets/IAM
// contract value; risk_type and severity classify the broad or partial
// posture evidence.
type IAMPrivilegePostureObservationResult struct {
	ObservationID      string   `json:"observation_id"`
	RiskType           string   `json:"risk_type,omitempty"`
	Severity           string   `json:"severity,omitempty"`
	State              string   `json:"state"`
	Confidence         string   `json:"confidence,omitempty"`
	SubjectFingerprint string   `json:"subject_fingerprint,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	EvidenceFactIDs    []string `json:"evidence_fact_ids,omitempty"`
}

// IAMSecretAccessPathResult is one reducer-owned Vault policy-to-KV
// metadata access path reachable from an exact identity chain. The field
// order matches IAMSecretAccessPathRow so the handler can convert rows
// directly.
type IAMSecretAccessPathResult struct {
	PathID             string   `json:"path_id"`
	ChainID            string   `json:"chain_id,omitempty"`
	State              string   `json:"state"`
	Confidence         string   `json:"confidence,omitempty"`
	KVPathFingerprint  string   `json:"kv_path_fingerprint,omitempty"`
	VaultMountJoinKey  string   `json:"vault_mount_join_key,omitempty"`
	VaultPolicyJoinKey string   `json:"vault_policy_join_key,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`
	EvidenceFactIDs    []string `json:"evidence_fact_ids,omitempty"`
}

// IAMPostureGapResult is one reducer-owned posture gap row: missing,
// stale, hidden, or unsupported evidence that blocks exact trust-chain truth.
// The field order matches IAMPostureGapRow so the handler can convert rows
// directly.
type IAMPostureGapResult struct {
	GapID                 string   `json:"gap_id"`
	GapType               string   `json:"gap_type,omitempty"`
	State                 string   `json:"state"`
	Reason                string   `json:"reason,omitempty"`
	ServiceAccountJoinKey string   `json:"service_account_join_key,omitempty"`
	EvidenceFactIDs       []string `json:"evidence_fact_ids,omitempty"`
	MissingEvidence       []string `json:"missing_evidence,omitempty"`
	UnsupportedLayers     []string `json:"unsupported_layers,omitempty"`
}

func (h *Handler) listPrivilegePostureObservations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMPrivilegePostureObservations,
		"GET /api/v0/secrets-iam/privilege-posture-observations",
		IAMPrivilegePostureObservationsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), IAMPrivilegePostureObservationsCapability) {
		querycontract.WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM privilege posture observations require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability, IAMPrivilegePostureObservationsCapability,
			h.profile(), querycontract.RequiredProfile(IAMPrivilegePostureObservationsCapability))
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := IAMPrivilegePostureObservationFilter{
		ScopeID:            querycontract.QueryParam(r, "scope_id"),
		ObservationID:      querycontract.QueryParam(r, "observation_id"),
		RiskType:           querycontract.QueryParam(r, "risk_type"),
		Severity:           querycontract.QueryParam(r, "severity"),
		State:              querycontract.QueryParam(r, "state"),
		AfterObservationID: querycontract.QueryParam(r, "after_observation_id"),
		Limit:              limit + 1,
	}
	if !filter.hasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id or observation_id is required")
		return
	}
	if !authorizeSecretsIAMScopedScope(w, r, filter.ScopeID) {
		return
	}
	if h.PrivilegePostureObservations == nil {
		querycontract.WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM privilege posture observations require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable, IAMPrivilegePostureObservationsCapability,
			h.profile(), querycontract.RequiredProfile(IAMPrivilegePostureObservationsCapability))
		return
	}

	rows, err := h.PrivilegePostureObservations.ListSecretsIAMPrivilegePostureObservations(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]IAMPrivilegePostureObservationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, IAMPrivilegePostureObservationResult(row))
	}
	body := map[string]any{
		"privilege_posture_observations": results,
		"count":                          len(results),
		"limit":                          limit,
		"truncated":                      truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_observation_id": results[len(results)-1].ObservationID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(), IAMPrivilegePostureObservationsCapability, querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM privilege posture observations; risky broad or partial posture evidence that the reducer keeps provenance-only and never promotes to an exact path",
	))
}

func (h *Handler) listSecretAccessPaths(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMSecretAccessPaths,
		"GET /api/v0/secrets-iam/secret-access-paths",
		IAMSecretAccessPathsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), IAMSecretAccessPathsCapability) {
		querycontract.WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM secret access paths require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability, IAMSecretAccessPathsCapability,
			h.profile(), querycontract.RequiredProfile(IAMSecretAccessPathsCapability))
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := IAMSecretAccessPathFilter{
		ScopeID:           querycontract.QueryParam(r, "scope_id"),
		PathID:            querycontract.QueryParam(r, "path_id"),
		ChainID:           querycontract.QueryParam(r, "chain_id"),
		VaultMountJoinKey: querycontract.QueryParam(r, "vault_mount_join_key"),
		State:             querycontract.QueryParam(r, "state"),
		AfterPathID:       querycontract.QueryParam(r, "after_path_id"),
		Limit:             limit + 1,
	}
	if !filter.hasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, path_id, chain_id, or vault_mount_join_key is required")
		return
	}
	if !authorizeSecretsIAMScopedScope(w, r, filter.ScopeID) {
		return
	}
	if h.SecretAccessPaths == nil {
		querycontract.WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM secret access paths require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable, IAMSecretAccessPathsCapability,
			h.profile(), querycontract.RequiredProfile(IAMSecretAccessPathsCapability))
		return
	}

	rows, err := h.SecretAccessPaths.ListSecretsIAMSecretAccessPaths(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]IAMSecretAccessPathResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, IAMSecretAccessPathResult(row))
	}
	body := map[string]any{
		"secret_access_paths": results,
		"count":               len(results),
		"limit":               limit,
		"truncated":           truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_path_id": results[len(results)-1].PathID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(), IAMSecretAccessPathsCapability, querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM secret access paths; a Vault policy-to-KV metadata path is reported only as reachable from an exact identity chain, never as a secret value",
	))
}

func (h *Handler) listPostureGaps(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMPostureGaps,
		"GET /api/v0/secrets-iam/posture-gaps",
		IAMPostureGapsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), IAMPostureGapsCapability) {
		querycontract.WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM posture gaps require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability, IAMPostureGapsCapability,
			h.profile(), querycontract.RequiredProfile(IAMPostureGapsCapability))
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := IAMPostureGapFilter{
		ScopeID:               querycontract.QueryParam(r, "scope_id"),
		GapID:                 querycontract.QueryParam(r, "gap_id"),
		GapType:               querycontract.QueryParam(r, "gap_type"),
		ServiceAccountJoinKey: querycontract.QueryParam(r, "service_account_join_key"),
		State:                 querycontract.QueryParam(r, "state"),
		AfterGapID:            querycontract.QueryParam(r, "after_gap_id"),
		Limit:                 limit + 1,
	}
	if !filter.hasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, gap_id, or service_account_join_key is required")
		return
	}
	if !authorizeSecretsIAMScopedScope(w, r, filter.ScopeID) {
		return
	}
	if h.PostureGaps == nil {
		querycontract.WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM posture gaps require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable, IAMPostureGapsCapability,
			h.profile(), querycontract.RequiredProfile(IAMPostureGapsCapability))
		return
	}

	rows, err := h.PostureGaps.ListSecretsIAMPostureGaps(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]IAMPostureGapResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, IAMPostureGapResult(row))
	}
	body := map[string]any{
		"posture_gaps": results,
		"count":        len(results),
		"limit":        limit,
		"truncated":    truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_gap_id": results[len(results)-1].GapID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(), IAMPostureGapsCapability, querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM posture gaps; missing, stale, permission_hidden, or unsupported evidence that blocks exact trust-chain truth, surfaced rather than silently dropped",
	))
}
