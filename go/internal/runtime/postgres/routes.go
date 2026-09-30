// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"net/http"
)

// RequiresCheckpoint selects mounted business routes after authentication.
// Unknown routes and explicitly pure or authoritative-control handlers avoid
// reader work. New mounted routes default to a checkpoint; a skipped route
// cannot execute guarded SQL without obtaining its own checkpoint.
func RequiresCheckpoint(mux *http.ServeMux, request *http.Request, pureNarration bool) bool {
	_, pattern := mux.Handler(request)
	if pattern == "" {
		return false
	}
	switch pattern {
	case "GET /api/v0/auth/admin/audit/events", "GET /api/v0/auth/admin/audit/summary":
		return true
	case "GET /api/v0/status/answer-narration":
		return !pureNarration
	case "GET /health", "GET /api/v0/openapi.json", "GET /api/v0/docs", "GET /api/v0/redoc",
		"GET /api/v0/capabilities", "GET /api/v0/surface-inventory",
		"GET /api/v0/admin/shared-projection/tuning-report", "GET /api/v0/metrics/timeseries",
		"POST /api/v0/supply-chain/impact/suppressions",
		"GET /api/v0/collector-extraction-readiness", "GET /api/v0/collector-extraction-readiness/{family}",
		"GET /api/v0/fact-schema-versions", "GET /api/v0/fact-schema-versions/{fact_kind}",
		"GET /api/v0/component-extensions", "GET /api/v0/component-extensions/{component_id}/diagnostics",
		"GET /api/v0/query-playbooks", "POST /api/v0/query-playbooks/resolve",
		"GET /api/v0/investigation-workflows", "POST /api/v0/investigation-workflows/resolve",
		"POST /api/v0/visualizations/derive", "POST /api/v0/ask":
		return false
	}
	return !authoritativeControlRoute(pattern)
}

// authoritativeControlRoute is an explicit list of mounted writer-backed control
// handlers. Future auth business routes remain checkpointed by default.
func authoritativeControlRoute(pattern string) bool {
	switch pattern {
	case "DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}",
		"DELETE /api/v0/auth/browser-session",
		"GET /api/v0/auth/admin/api-tokens",
		"GET /api/v0/auth/admin/idp-group-mappings",
		"GET /api/v0/auth/admin/idp-providers",
		"GET /api/v0/auth/admin/provider-configs",
		"GET /api/v0/auth/admin/provider-configs/{provider_config_id}",
		"GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions",
		"GET /api/v0/auth/admin/role-assignments",
		"GET /api/v0/auth/admin/roles",
		"GET /api/v0/auth/admin/sign-in-policy",
		"GET /api/v0/auth/browser-session",
		"GET /api/v0/auth/github/callback",
		"GET /api/v0/auth/github/login",
		"GET /api/v0/auth/local/api-tokens",
		"GET /api/v0/auth/local/invitations",
		"GET /api/v0/auth/oidc/callback",
		"GET /api/v0/auth/oidc/login",
		"GET /api/v0/auth/profile",
		"GET /api/v0/auth/providers",
		"GET /api/v0/auth/saml/providers/{provider_id}/login",
		"GET /api/v0/auth/saml/providers/{provider_id}/metadata",
		"GET /api/v0/auth/sessions",
		"GET /api/v0/auth/setup-state",
		"GET /api/v0/auth/sign-in-policy",
		"PATCH /api/v0/auth/admin/sign-in-policy",
		"PATCH /api/v0/auth/browser-session/context",
		"POST /api/v0/auth/admin/idp-group-mappings",
		"POST /api/v0/auth/admin/provider-configs",
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}",
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable",
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable",
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert",
		"POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection",
		"POST /api/v0/auth/admin/role-assignments",
		"POST /api/v0/auth/admin/role-assignments/revoke",
		"POST /api/v0/auth/browser-session",
		"POST /api/v0/auth/local/api-tokens",
		"POST /api/v0/auth/local/api-tokens/{token_id}/revoke",
		"POST /api/v0/auth/local/api-tokens/{token_id}/rotate",
		"POST /api/v0/auth/local/bootstrap",
		"POST /api/v0/auth/local/break-glass",
		"POST /api/v0/auth/local/break-glass/session",
		"POST /api/v0/auth/local/invitations",
		"POST /api/v0/auth/local/invitations/accept",
		"POST /api/v0/auth/local/invitations/{invite_id}/revoke",
		"POST /api/v0/auth/local/login",
		"POST /api/v0/auth/local/mfa/totp/begin",
		"POST /api/v0/auth/local/mfa/totp/confirm",
		"POST /api/v0/auth/local/password/rotate",
		"POST /api/v0/auth/local/users/{user_id}/disable",
		"POST /api/v0/auth/local/users/{user_id}/mfa-reset",
		"POST /api/v0/auth/local/users/{user_id}/password",
		"POST /api/v0/auth/saml/providers/{provider_id}/acs",
		"POST /api/v0/auth/setup/admin",
		"POST /api/v0/auth/setup/claim",
		"POST /api/v0/auth/setup/mfa":
		return true
	default:
		return false
	}
}
