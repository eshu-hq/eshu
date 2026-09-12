// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// IAMIdentityTrustChainsCapability is the identity trust-chain list
	// route's capability ID. Root package query's contract_secrets_iam.go
	// reads this exported const (aliased there as
	// secretsIAMIdentityTrustChainsCapability) to register the capability
	// with the shared matrix; this package's own TestMain registers it too,
	// since a leaf package's test binary never links root.
	IAMIdentityTrustChainsCapability = "secrets_iam.identity_trust_chains.list"
	// secretsIAMTrustChainMaxLimit bounds the page size for every trust-chain-shaped
	// list route in this family.
	secretsIAMTrustChainMaxLimit = 200
)

// Handler exposes reducer-owned secrets/IAM trust-chain reads (issue #25). It
// is a bounded, paginated, read-only surface over the durable
// reducer_secrets_iam_* facts produced by the trust-chain reducer, plus the
// canonical GRANTS_ACCESS_TO grant edges read by GrantPosture (issue #5643);
// it performs no graph writes and adds no reducer logic. Rows are
// fingerprints, join keys, states, and evidence IDs only, so no secret value,
// raw path, or token claim crosses the wire.
type Handler struct {
	IdentityTrustChains          IAMIdentityTrustChainStore
	PrivilegePostureObservations IAMPrivilegePostureObservationStore
	SecretAccessPaths            IAMSecretAccessPathStore
	PostureGaps                  IAMPostureGapStore
	Summary                      IAMPostureSummaryStore
	// GrantPosture reads the S3 external-principal grant section of the
	// posture summary from the canonical graph edges. When nil (a deployment
	// without a graph reader wired), the summary omits the grant section
	// instead of failing the whole rollup.
	GrantPosture IAMGrantPostureStore
	Profile      querycontract.QueryProfile
}

// IAMIdentityTrustChainResult is one reducer-owned identity trust-chain row. The
// field order matches IAMIdentityTrustChainRow so the handler can convert rows
// directly. State is the six-state secrets/IAM contract value (exact,
// partial, unresolved, stale, permission_hidden, unsupported).
type IAMIdentityTrustChainResult struct {
	ChainID               string   `json:"chain_id"`
	State                 string   `json:"state"`
	Confidence            string   `json:"confidence,omitempty"`
	ServiceAccountJoinKey string   `json:"service_account_join_key,omitempty"`
	WorkloadObjectID      string   `json:"workload_object_id,omitempty"`
	WorkloadKind          string   `json:"workload_kind,omitempty"`
	IAMRoleFingerprint    string   `json:"iam_role_fingerprint,omitempty"`
	VaultRoleJoinKey      string   `json:"vault_role_join_key,omitempty"`
	VaultMountJoinKey     string   `json:"vault_mount_join_key,omitempty"`
	VaultPolicyJoinKeys   []string `json:"vault_policy_join_keys,omitempty"`
	EvidenceFactIDs       []string `json:"evidence_fact_ids,omitempty"`
	MissingEvidence       []string `json:"missing_evidence,omitempty"`
	SourceScopes          []string `json:"source_scopes,omitempty"`
	SourceGenerations     []string `json:"source_generations,omitempty"`
}

// Mount registers secrets/IAM query routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/secrets-iam/identity-trust-chains", h.listIdentityTrustChains)
	mux.HandleFunc("GET /api/v0/secrets-iam/privilege-posture-observations", h.listPrivilegePostureObservations)
	mux.HandleFunc("GET /api/v0/secrets-iam/secret-access-paths", h.listSecretAccessPaths)
	mux.HandleFunc("GET /api/v0/secrets-iam/posture-gaps", h.listPostureGaps)
	mux.HandleFunc("GET /api/v0/secrets-iam/posture-summary", h.summary)
}

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil || h.Profile == "" {
		return querycontract.ProfileProduction
	}
	return h.Profile
}

func (h *Handler) listIdentityTrustChains(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQuerySecretsIAMIdentityTrustChains,
		"GET /api/v0/secrets-iam/identity-trust-chains",
		IAMIdentityTrustChainsCapability,
	)
	defer span.End()

	if querycontract.CapabilityUnsupported(h.profile(), IAMIdentityTrustChainsCapability) {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"secrets/IAM identity trust chains require the Postgres reducer read model",
			querycontract.ErrorCodeUnsupportedCapability,
			IAMIdentityTrustChainsCapability,
			h.profile(),
			querycontract.RequiredProfile(IAMIdentityTrustChainsCapability),
		)
		return
	}
	limit, ok := requiredSecretsIAMTrustChainLimit(w, r)
	if !ok {
		return
	}
	filter := IAMIdentityTrustChainFilter{
		ScopeID:               querycontract.QueryParam(r, "scope_id"),
		ChainID:               querycontract.QueryParam(r, "chain_id"),
		WorkloadObjectID:      querycontract.QueryParam(r, "workload_object_id"),
		ServiceAccountJoinKey: querycontract.QueryParam(r, "service_account_join_key"),
		IAMRoleFingerprint:    querycontract.QueryParam(r, "iam_role_fingerprint"),
		State:                 querycontract.QueryParam(r, "state"),
		AfterChainID:          querycontract.QueryParam(r, "after_chain_id"),
		Limit:                 limit + 1,
	}
	if !filter.hasScope() {
		querycontract.WriteError(w, http.StatusBadRequest, "scope_id, chain_id, workload_object_id, service_account_join_key, or iam_role_fingerprint is required")
		return
	}
	if !authorizeSecretsIAMScopedScope(w, r, filter.ScopeID) {
		return
	}
	if h.IdentityTrustChains == nil {
		querycontract.WriteContractError(
			w,
			r,
			http.StatusServiceUnavailable,
			"secrets/IAM identity trust chains require the Postgres reducer read model",
			querycontract.ErrorCodeBackendUnavailable,
			IAMIdentityTrustChainsCapability,
			h.profile(),
			querycontract.RequiredProfile(IAMIdentityTrustChainsCapability),
		)
		return
	}

	rows, err := h.IdentityTrustChains.ListSecretsIAMIdentityTrustChains(r.Context(), filter)
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := make([]IAMIdentityTrustChainResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, IAMIdentityTrustChainResult(row))
	}
	body := map[string]any{
		"identity_trust_chains": results,
		"count":                 len(results),
		"limit":                 limit,
		"truncated":             truncated,
	}
	if truncated && len(results) > 0 {
		body["next_cursor"] = map[string]string{
			"after_chain_id": results[len(results)-1].ChainID,
		}
	}
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(
		h.profile(),
		IAMIdentityTrustChainsCapability,
		querycontract.TruthBasisSemanticFacts,
		"resolved from reducer-owned secrets/IAM identity trust-chain facts; an exact chain requires every hop resolved with explicit evidence, otherwise the chain stays provenance-only as partial, unresolved, stale, permission_hidden, or unsupported",
	))
}

func requiredSecretsIAMTrustChainLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > secretsIAMTrustChainMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", secretsIAMTrustChainMaxLimit))
		return 0, false
	}
	return limit, true
}
