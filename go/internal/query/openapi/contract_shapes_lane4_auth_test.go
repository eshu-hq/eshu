// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import "testing"

func TestContractShapesLane4Auth(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("admin lists name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/identity/reads.go projects each list row from its
		// store item and always emits top-level truncated.
		invitations := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/invitations", "get", "200"), "invitations")
		lane4RequireExactKeys(t, invitations, "invitations", "invitations", "truncated")
		invitationRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, invitations, "invitations", "invitations"), "rows"), "rows[]")
		lane4RequireExactKeys(t, invitationRows, "invitations[]",
			"invite_id", "role_id", "status", "expires_at", "accepted_at",
			"revoked_at", "created_at", "updated_at", "tenant_id", "workspace_id")
		assignments := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/role-assignments", "get", "200"), "assignments")
		lane4RequireExactKeys(t, assignments, "assignments", "role_assignments", "truncated")
		assignmentRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, assignments, "role_assignments", "assignments"), "rows"), "rows[]")
		lane4RequireExactKeys(t, assignmentRows, "assignments[]",
			"user_id", "role_id", "assignment_source", "status",
			"effective_at", "expires_at", "tenant_id", "workspace_id")
		roles := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/roles", "get", "200"), "roles")
		lane4RequireExactKeys(t, roles, "roles", "roles", "truncated")
		// Owner: admin/identity/mutations.go handleGrantRoleAssignment and
		// handleRevokeRoleAssignment emit user_id, role_id, status, changed.
		grant := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/role-assignments", "post", "200"), "grant")
		lane4RequireExactKeys(t, grant, "grant", "user_id", "role_id", "status", "changed")
		lane4RequireDescribed(t, grant, "grant", "user_id", "role_id", "status", "changed")
		deleteMapping := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/idp-group-mappings/{mapping_ref}", "delete", "200"), "delete-mapping")
		lane4RequireExactKeys(t, deleteMapping, "delete-mapping", "mapping_ref", "deleted")
		lane4RequireDescribed(t, deleteMapping, "delete-mapping", "mapping_ref", "deleted")
		providers := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/idp-providers", "get", "200"), "providers")
		lane4RequireExactKeys(t, providers, "providers", "providers", "truncated")
		mappings := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/idp-group-mappings", "get", "200"), "mappings")
		lane4RequireExactKeys(t, mappings, "mappings", "group_mappings", "truncated", "next_after_ref")
		tokens := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/api-tokens", "get", "200"), "admin-tokens")
		lane4RequireExactKeys(t, tokens, "admin-tokens", "tokens", "truncated")
		tokenRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, tokens, "tokens", "tokens"), "rows"), "rows[]")
		lane4RequireExactKeys(t, tokenRows, "admin-tokens[]",
			"token_id", "token_class", "user_id", "service_principal_id",
			"status", "display_label", "issued_at", "expires_at",
			"revoked_at", "tenant_id", "workspace_id")
		// Owner: admin/identity/audit.go handleListAuditEvents emits events
		// plus truncated; handleAuditSummary emits the aggregate counts.
		events := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/audit/events", "get", "200"), "audit-events")
		lane4RequireExactKeys(t, events, "audit-events", "events", "truncated")
		eventRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, events, "events", "events"), "rows"), "rows[]")
		lane4RequireExactKeys(t, eventRows, "audit-events[]",
			"event_type", "actor_class", "scope_class", "decision",
			"reason_code", "occurred_at", "service_principal_id", "correlation_id")
		summary := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/audit/summary", "get", "200"), "audit-summary")
		lane4RequireExactKeys(t, summary, "audit-summary",
			"total", "allowed", "denied", "unavailable", "last_occurred_at",
			"event_type_counts", "decision_counts", "reason_counts",
			"actor_class_counts", "scope_class_counts")
	})

	t.Run("invitation revoke names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: mutations.go handleRevokeInvitation emits invite_id,
		// status, revoked.
		revoke := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/invitations/{invite_id}/revoke", "post", "200"), "revoke")
		lane4RequireExactKeys(t, revoke, "revoke", "invite_id", "status", "revoked")
		lane4RequireDescribed(t, revoke, "revoke", "invite_id", "status", "revoked")
	})

	t.Run("provider configs and setup name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/provider/config/reads.go lists revisions newest
		// first; the test-connection result echoes provider_config_id.
		revisions := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t,
			lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/provider-configs/{provider_config_id}/revisions", "get", "200"), "revisions"),
			"revisions", "revisions"), "rows"), "rows[]")
		lane4RequireExactKeys(t, revisions, "revisions[]",
			"revision_id", "status", "has_secret", "created_at",
			"activated_at", "superseded_at")
		lane4RequireDescribed(t, revisions, "revisions[]",
			"revision_id", "status", "has_secret", "created_at",
			"activated_at", "superseded_at")
		testConn := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection", "post", "200"), "test-connection")
		lane4RequireExactKeys(t, testConn, "test-connection", "provider_config_id", "ok", "detail")
		lane4RequireDescribed(t, testConn, "test-connection", "provider_config_id", "ok", "detail")
		// Owner: setup_handler.go SetupStateResponse and the claim/admin
		// step maps; setup_mfa_handler.go SetupCompleteResponse.
		setupState := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/setup-state", "get", "200"), "setup-state")
		lane4RequireExactKeys(t, setupState, "setup-state", "needs_setup", "bootstrap_mode")
		lane4RequireDescribed(t, setupState, "setup-state", "needs_setup", "bootstrap_mode")
		claim := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/setup/claim", "post", "200"), "claim")
		lane4RequireExactKeys(t, claim, "claim", "status")
		lane4RequireDescribed(t, claim, "claim", "status")
		setupAdmin := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/setup/admin", "post", "200"), "setup-admin")
		lane4RequireExactKeys(t, setupAdmin, "setup-admin", "status", "tenant_id", "workspace_id")
		lane4RequireDescribed(t, setupAdmin, "setup-admin", "status", "tenant_id", "workspace_id")
		setupMFA := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/setup/mfa", "post", "200"), "setup-mfa")
		lane4RequireExactKeys(t, setupMFA, "setup-mfa",
			"status", "recovery_codes", "auth", "csrf_token",
			"idle_expires_at", "absolute_expires_at")
		lane4RequireDescribed(t, setupMFA, "setup-mfa",
			"status", "auth", "csrf_token", "idle_expires_at", "absolute_expires_at")
		policy := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/sign-in-policy", "get", "200"), "sign-in-policy")
		lane4RequireExactKeys(t, policy, "sign-in-policy", "require_sso")
		lane4RequireDescribed(t, policy, "sign-in-policy", "require_sso")
		// Owner: local/totp.go localIdentityTOTPBeginResponse; the secret
		// is returned once only, like otpauth_uri.
		totp := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/mfa/totp/begin", "post", "201"), "totp")
		lane4RequireExactKeys(t, totp, "totp-begin",
			"factor_id", "otpauth_uri", "secret", "issuer", "digits", "period_seconds")
		lane4RequireDescribed(t, totp, "totp-begin",
			"factor_id", "otpauth_uri", "issuer", "digits", "period_seconds")
		selfTokens := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/api-tokens", "get", "200"), "self-tokens")
		tokenRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, selfTokens, "tokens", "self-tokens"), "rows"), "rows[]")
		lane4RequireExactKeys(t, tokenRows, "self-tokens[]",
			"token_id", "token_class", "display_label", "issued_at", "expires_at", "revoked_at")
		lane4RequireDescribed(t, tokenRows, "self-tokens[]",
			"token_id", "token_class", "issued_at", "expires_at", "revoked_at")
	})

	t.Run("profile and sessions name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: profile_handler.go profileJSON aggregates the caller's own
		// auth context; memberships carry only the active tenant/workspace.
		profile := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/profile", "get", "200"), "profile")
		lane4RequireExactKeys(t, profile, "profile",
			"external_provider_config_id", "active_tenant_id",
			"active_workspace_id", "role_ids", "allowed_permission_features",
			"permission_catalog_enforced", "mfa", "memberships")
		lane4RequireDescribed(t, profile, "profile",
			"external_provider_config_id", "active_tenant_id",
			"active_workspace_id", "role_ids", "allowed_permission_features",
			"permission_catalog_enforced", "mfa", "memberships")
		mfa := lane4PropsOf(t, lane4Child(t, profile, "mfa", "profile"), "profile.mfa")
		lane4RequireExactKeys(t, mfa, "profile.mfa", "has_active_mfa", "factor_kind")
		lane4RequireDescribed(t, mfa, "profile.mfa", "has_active_mfa", "factor_kind")
		memberships := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, profile, "memberships", "profile"), "memberships"), "memberships[]")
		lane4RequireExactKeys(t, memberships, "profile.memberships[]", "tenant_id", "workspace_id")
		// Owner: browser_session_list.go sessionJSON plus conditional
		// prev/next page links.
		sessions := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/sessions", "get", "200"), "sessions")
		lane4RequireKeys(t, sessions, "sessions", "sessions", "prev", "next")
		sessionRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, sessions, "sessions", "sessions"), "rows"), "rows[]")
		lane4RequireExactKeys(t, sessionRows, "sessions[]",
			"issued_at", "last_seen_at", "idle_expires_at",
			"absolute_expires_at", "tenant_id", "workspace_id",
			"current", "revoked_at")
		lane4RequireDescribed(t, sessionRows, "sessions[]",
			"issued_at", "last_seen_at", "idle_expires_at",
			"absolute_expires_at", "tenant_id", "workspace_id",
			"current", "revoked_at")
	})
}
