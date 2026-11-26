// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import "testing"

// TestContractShapesLaneANo200 pins the NO200-gap closure: the five routes
// whose success carries a JSON body under a non-200 status now document
// that body with every property described and its emitting owner named.
// Each entry names the Go owner that emits the keys.
func TestContractShapesLaneANo200(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("bootstrap names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: local/handler.go handleBootstrap emits status, user_id,
		// tenant_id, workspace_id with 201.
		bootstrap := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/bootstrap", "post", "201"), "bootstrap")
		lane4RequireExactKeys(t, bootstrap, "bootstrap",
			"status", "user_id", "tenant_id", "workspace_id")
		lane4RequireDescribed(t, bootstrap, "bootstrap",
			"status", "user_id", "tenant_id", "workspace_id")
	})

	t.Run("invitation create and accept name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: local/handler.go handleCreateInvitation emits invite_id,
		// invite_code (once only), expires_at with 201.
		created := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/invitations", "post", "201"), "invitation-create")
		lane4RequireExactKeys(t, created, "invitation-create",
			"invite_id", "invite_code", "expires_at")
		lane4RequireDescribed(t, created, "invitation-create",
			"invite_id", "invite_code", "expires_at")
		// Owner: local/handler.go handleAcceptInvitation emits status,
		// user_id with 201.
		accepted := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/invitations/accept", "post", "201"), "invitation-accept")
		lane4RequireExactKeys(t, accepted, "invitation-accept",
			"status", "user_id")
		lane4RequireDescribed(t, accepted, "invitation-accept",
			"status", "user_id")
	})

	t.Run("break-glass enable names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: local/helpers.go handleEnableBreakGlass emits
		// recovery_id, break_glass_code (once only), expires_at, and
		// policy_revision_hash with 201.
		window := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/auth/local/break-glass", "post", "201"), "break-glass")
		lane4RequireExactKeys(t, window, "break-glass",
			"recovery_id", "break_glass_code", "expires_at", "policy_revision_hash")
		lane4RequireDescribed(t, window, "break-glass",
			"recovery_id", "break_glass_code", "expires_at", "policy_revision_hash")
	})

	t.Run("repository retirement names owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: admin/retirement/handler.go triggerResponse emits
		// status, retirements, idempotency_key, duplicate with 202; each
		// entry carries the retirement/repository/scope ids plus outcome,
		// with blocked_* only when outcome is blocked.
		retirement := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/admin/repository-retirements", "post", "202"), "retirement")
		lane4RequireExactKeys(t, retirement, "retirement",
			"status", "retirements", "idempotency_key", "duplicate")
		lane4RequireDescribed(t, retirement, "retirement",
			"status", "retirements", "idempotency_key", "duplicate")
		entry := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, retirement, "retirements", "retirement"), "entries"), "entries[]")
		lane4RequireKeys(t, entry, "retirement[]",
			"retirement_id", "repository_id", "scope_id", "outcome")
		lane4RequireDescribed(t, entry, "retirement[]",
			"retirement_id", "repository_id", "scope_id", "outcome",
			"blocked_reason", "blocked_detail", "blocking_statuses")
	})
}
