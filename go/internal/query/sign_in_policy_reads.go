// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"log/slog"
	"net/http"
)

// SignInPolicyReadHandler serves the tenant sign-in policy read routes
// (epic #4962, issue #4968):
//
//   - GET /api/v0/auth/sign-in-policy is PUBLIC (pre-auth), matching
//     AuthProviderListHandler's pattern: the login page must know whether to
//     hide the local password form BEFORE the user is authenticated. It
//     exposes only require_sso — never MFA/session/SSO-proof detail — scoped
//     by the required tenant_id query parameter, mirroring
//     AuthProviderListHandler exactly (absent/empty tenant_id returns the
//     zero-configuration default rather than a cross-tenant scan).
//   - GET /api/v0/auth/admin/sign-in-policy is admin-only and returns the
//     full policy, including SSO-admin-proof metadata (a hash-free
//     provider_config_id and timestamp — never a secret) needed for the
//     console guardrail-state display.
//
// Neither route emits a governance audit event. Issue #4968's acceptance
// criterion is "all policy changes are durable audit events" — reads are
// deliberately out of scope. This matches the sibling read-handler
// convention (AdminProviderConfigReadHandler also does not audit reads):
// a read exposes no secret (the admin route strips every credential field;
// the public route exposes only the require_sso boolean) and auditing every
// read would be volume noise with no operator value. Governance audit
// coverage for sign-in policy lives on the mutation path
// (SignInPolicyMutationHandler.audit in sign_in_policy_mutations.go) and on
// the require-SSO login gate's allow/deny decision
// (LocalIdentityHandler.auditLocalIdentity, reason
// local_login_denied_require_sso_policy, in local_identity_handler.go) —
// not on this handler's reads.
type SignInPolicyReadHandler struct {
	Store SignInPolicyReadStore
}

// Mount registers the public and admin sign-in policy read routes.
func (h *SignInPolicyReadHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/sign-in-policy", h.handlePublicGet)
	mux.HandleFunc("GET /api/v0/auth/admin/sign-in-policy", h.handleAdminGet)
}

func (h *SignInPolicyReadHandler) storeReady(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "sign-in policy store is unavailable")
		return false
	}
	return true
}

func (h *SignInPolicyReadHandler) handlePublicGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=30")
	if h == nil || h.Store == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"require_sso": false})
		return
	}
	tenantID := QueryParam(r, "tenant_id")
	if tenantID == "" {
		// No tenant can be resolved pre-auth without an explicit tenant_id.
		// Default to require_sso=false rather than a cross-tenant scan, so an
		// unresolvable tenant never hides the local login form (fails open on
		// the UI hint only — the server-side login gate in
		// LocalIdentityHandler.handleLogin is the real enforcement boundary
		// and is unaffected by this default).
		WriteJSON(w, http.StatusOK, map[string]any{"require_sso": false})
		return
	}
	policy, err := h.Store.GetSignInPolicy(r.Context(), tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "public sign-in policy read failed", "err", err)
		WriteJSON(w, http.StatusOK, map[string]any{"require_sso": false})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"require_sso": policy.RequireSSO})
}

func (h *SignInPolicyReadHandler) handleAdminGet(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "identity_admin.sign_in_policy_read", permissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := signInPolicyAdminScope(w, r)
	if !ok {
		return
	}
	policy, err := h.Store.GetSignInPolicy(r.Context(), tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin sign-in policy read failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to read sign-in policy")
		return
	}
	WriteJSON(w, http.StatusOK, signInPolicyDetailJSON(policy))
}

// signInPolicyAdminScope resolves the all-scope admin caller's tenant,
// mirroring AdminProviderConfigReadHandler.adminScope.
func signInPolicyAdminScope(w http.ResponseWriter, r *http.Request) (tenantID string, ok bool) {
	auth, found := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", false
	}
	if auth.TenantID == "" {
		WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", false
	}
	return auth.TenantID, true
}

// signInPolicyDetailJSON projects SignInPolicy into the admin API response
// shape. sso_admin_verified_provider_config_id is an operator-assigned
// config id, not a secret; sso_admin_verified_at is a proof timestamp. No
// field on this type is ever a credential.
func signInPolicyDetailJSON(policy SignInPolicy) map[string]any {
	row := map[string]any{
		"tenant_id":                 policy.TenantID,
		"require_sso":               policy.RequireSSO,
		"allow_local_user_creation": policy.AllowLocalUserCreation,
		"require_mfa_for_all_users": policy.RequireMFAForAllUsers,
		"idle_timeout_seconds":      policy.IdleTimeoutSeconds,
		"absolute_timeout_seconds":  policy.AbsoluteTimeoutSeconds,
		"policy_revision_hash":      policy.PolicyRevisionHash,
		"updated_at":                policy.UpdatedAt,
	}
	if !policy.SSOAdminVerifiedAt.IsZero() {
		row["sso_admin_verified_at"] = policy.SSOAdminVerifiedAt.UTC()
	}
	if policy.SSOAdminVerifiedProviderConfigID != "" {
		row["sso_admin_verified_provider_config_id"] = policy.SSOAdminVerifiedProviderConfigID
	}
	return row
}
