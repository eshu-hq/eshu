// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// primaryWorkspaceLookupLimit caps PrimaryWorkspaceForTenant's read at 2
// rows: enough to distinguish "exactly one" from "more than one" without
// ever scanning a tenant's full workspace set.
const primaryWorkspaceLookupLimit = 2

// ErrTenantWorkspaceAmbiguous indicates a tenant has more than one active
// workspace, so PrimaryWorkspaceForTenant cannot default to a single
// workspace for a caller that has no workspace of its own to enforce (#5040:
// a tenant-scoped DB-backed OIDC provider login-start). The caller must
// require an explicit workspace_id instead of guessing.
var ErrTenantWorkspaceAmbiguous = errors.New("tenant workspace grant store: tenant has more than one active workspace")

// ErrTenantWorkspaceNotFound indicates a tenant has no active workspace row,
// so PrimaryWorkspaceForTenant has nothing to default to.
var ErrTenantWorkspaceNotFound = errors.New("tenant workspace grant store: tenant has no active workspace")

// PrimaryWorkspaceForTenant resolves the single active workspace_id for a
// tenant (#5040). It exists for callers that hold a tenant-scoped record with
// no workspace of its own — the motivating case is a DB-backed OIDC provider
// config (identity_provider_configs has no workspace_id column, unlike
// env-file providers) whose login-start must still write a non-blank
// workspace_id into identity_oidc_login_states (workspace_id TEXT NOT NULL).
// Returns ErrTenantWorkspaceAmbiguous when the tenant has more than one
// active workspace — the caller must not silently pick one, it must require
// an explicit workspace_id instead — and ErrTenantWorkspaceNotFound when the
// tenant has none.
func (s *TenantWorkspaceGrantStore) PrimaryWorkspaceForTenant(ctx context.Context, tenantID string) (string, error) {
	if s.db == nil {
		return "", errors.New("tenant workspace grant store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", errors.New("tenant id is required")
	}
	rows, err := s.db.QueryContext(ctx, listActiveWorkspaceIDsForTenantQuery, tenantID, primaryWorkspaceLookupLimit)
	if err != nil {
		return "", fmt.Errorf("primary workspace for tenant: %w", err)
	}
	defer func() { _ = rows.Close() }()

	workspaceIDs := make([]string, 0, primaryWorkspaceLookupLimit)
	for rows.Next() {
		var workspaceID string
		if err := rows.Scan(&workspaceID); err != nil {
			return "", fmt.Errorf("primary workspace for tenant: %w", err)
		}
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("primary workspace for tenant: %w", err)
	}
	switch len(workspaceIDs) {
	case 0:
		return "", ErrTenantWorkspaceNotFound
	case 1:
		return workspaceIDs[0], nil
	default:
		return "", ErrTenantWorkspaceAmbiguous
	}
}
