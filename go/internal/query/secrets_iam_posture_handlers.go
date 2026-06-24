// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	secretsIAMPrivilegePostureObservationsCapability = "secrets_iam.privilege_posture_observations.list"
	secretsIAMSecretAccessPathsCapability            = "secrets_iam.secret_access_paths.list"
	secretsIAMPostureGapsCapability                  = "secrets_iam.posture_gaps.list"
)

// SecretsIAMPrivilegePostureObservationResult is one reducer-owned privilege
// posture observation row. The field order matches
// SecretsIAMPrivilegePostureObservationRow so the handler can convert rows
// directly. State is the six-state secrets/IAM contract value; risk_type and
// severity classify the broad or partial posture evidence.
type SecretsIAMPrivilegePostureObservationResult struct {
	ObservationID      string   `json:"observation_id"`
	RiskType           string   `json:"risk_type,omitempty"`
	Severity           string   `json:"severity,omitempty"`
	State              string   `json:"state"`
	Confidence         string   `json:"confidence,omitempty"`
	SubjectFingerprint string   `json:"subject_fingerprint,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	EvidenceFactIDs    []string `json:"evidence_fact_ids,omitempty"`
}

// SecretsIAMSecretAccessPathResult is one reducer-owned Vault policy-to-KV
// metadata access path reachable from an exact identity chain. The field order
// matches SecretsIAMSecretAccessPathRow so the handler can convert rows
// directly.
type SecretsIAMSecretAccessPathResult struct {
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

// SecretsIAMPostureGapResult is one reducer-owned posture gap row: missing,
// stale, hidden, or unsupported evidence that blocks exact trust-chain truth.
// The field order matches SecretsIAMPostureGapRow so the handler can convert
// rows directly.
type SecretsIAMPostureGapResult struct {
	GapID                 string   `json:"gap_id"`
	GapType               string   `json:"gap_type,omitempty"`
	State                 string   `json:"state"`
	Reason                string   `json:"reason,omitempty"`
	ServiceAccountJoinKey string   `json:"service_account_join_key,omitempty"`
	EvidenceFactIDs       []string `json:"evidence_fact_ids,omitempty"`
	MissingEvidence       []string `json:"missing_evidence,omitempty"`
	UnsupportedLayers     []string `json:"unsupported_layers,omitempty"`
}

func (h *SecretsIAMHandler) listPrivilegePostureObservations(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMPrivilegePostureObservations,
		"GET /api/v0/secrets-iam/privilege-posture-observations",
		secretsIAMPrivilegePostureObservationsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), secretsIAMPrivilegePostureObservationsCapability) {
		WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM privilege posture observations require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability, secretsIAMPrivilegePostureObservationsCapability,
			h.profile(), requiredProfile(secretsIAMPrivilegePostureObservationsCapability))
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := SecretsIAMPrivilegePostureObservationFilter{
		ScopeID:            QueryParam(r, "scope_id"),
		ObservationID:      QueryParam(r, "observation_id"),
		RiskType:           QueryParam(r, "risk_type"),
		Severity:           QueryParam(r, "severity"),
		State:              QueryParam(r, "state"),
		AfterObservationID: QueryParam(r, "after_observation_id"),
		Limit:              limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id or observation_id is required")
		return
	}
	if h.PrivilegePostureObservations == nil {
		WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM privilege posture observations require the Postgres reducer read model",
			ErrorCodeBackendUnavailable, secretsIAMPrivilegePostureObservationsCapability,
			h.profile(), requiredProfile(secretsIAMPrivilegePostureObservationsCapability))
		return
	}

	rows, err := h.PrivilegePostureObservations.ListSecretsIAMPrivilegePostureObservations(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SecretsIAMPrivilegePostureObservationResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SecretsIAMPrivilegePostureObservationResult(row))
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(), secretsIAMPrivilegePostureObservationsCapability, TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM privilege posture observations; risky broad or partial posture evidence that the reducer keeps provenance-only and never promotes to an exact path",
	))
}

func (h *SecretsIAMHandler) listSecretAccessPaths(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMSecretAccessPaths,
		"GET /api/v0/secrets-iam/secret-access-paths",
		secretsIAMSecretAccessPathsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), secretsIAMSecretAccessPathsCapability) {
		WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM secret access paths require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability, secretsIAMSecretAccessPathsCapability,
			h.profile(), requiredProfile(secretsIAMSecretAccessPathsCapability))
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := SecretsIAMSecretAccessPathFilter{
		ScopeID:           QueryParam(r, "scope_id"),
		PathID:            QueryParam(r, "path_id"),
		ChainID:           QueryParam(r, "chain_id"),
		VaultMountJoinKey: QueryParam(r, "vault_mount_join_key"),
		State:             QueryParam(r, "state"),
		AfterPathID:       QueryParam(r, "after_path_id"),
		Limit:             limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, path_id, chain_id, or vault_mount_join_key is required")
		return
	}
	if h.SecretAccessPaths == nil {
		WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM secret access paths require the Postgres reducer read model",
			ErrorCodeBackendUnavailable, secretsIAMSecretAccessPathsCapability,
			h.profile(), requiredProfile(secretsIAMSecretAccessPathsCapability))
		return
	}

	rows, err := h.SecretAccessPaths.ListSecretsIAMSecretAccessPaths(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SecretsIAMSecretAccessPathResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SecretsIAMSecretAccessPathResult(row))
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(), secretsIAMSecretAccessPathsCapability, TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM secret access paths; a Vault policy-to-KV metadata path is reported only as reachable from an exact identity chain, never as a secret value",
	))
}

func (h *SecretsIAMHandler) listPostureGaps(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMPostureGaps,
		"GET /api/v0/secrets-iam/posture-gaps",
		secretsIAMPostureGapsCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), secretsIAMPostureGapsCapability) {
		WriteContractError(w, r, http.StatusNotImplemented,
			"secrets/IAM posture gaps require the Postgres reducer read model",
			ErrorCodeUnsupportedCapability, secretsIAMPostureGapsCapability,
			h.profile(), requiredProfile(secretsIAMPostureGapsCapability))
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := SecretsIAMPostureGapFilter{
		ScopeID:               QueryParam(r, "scope_id"),
		GapID:                 QueryParam(r, "gap_id"),
		GapType:               QueryParam(r, "gap_type"),
		ServiceAccountJoinKey: QueryParam(r, "service_account_join_key"),
		State:                 QueryParam(r, "state"),
		AfterGapID:            QueryParam(r, "after_gap_id"),
		Limit:                 limit + 1,
	}
	if !filter.hasScope() {
		WriteError(w, http.StatusBadRequest, "scope_id, gap_id, or service_account_join_key is required")
		return
	}
	if h.PostureGaps == nil {
		WriteContractError(w, r, http.StatusServiceUnavailable,
			"secrets/IAM posture gaps require the Postgres reducer read model",
			ErrorCodeBackendUnavailable, secretsIAMPostureGapsCapability,
			h.profile(), requiredProfile(secretsIAMPostureGapsCapability))
		return
	}

	rows, err := h.PostureGaps.ListSecretsIAMPostureGaps(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]SecretsIAMPostureGapResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SecretsIAMPostureGapResult(row))
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
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(), secretsIAMPostureGapsCapability, TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM posture gaps; missing, stale, permission_hidden, or unsupported evidence that blocks exact trust-chain truth, surfaced rather than silently dropped",
	))
}
