// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const identityLocalIdentitySchemaSQL = `
CREATE TABLE IF NOT EXISTS identity_local_auth_attempts (
    user_id TEXT PRIMARY KEY REFERENCES identity_users(user_id) ON DELETE CASCADE,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ NULL,
    last_failed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS identity_local_auth_attempts_locked_idx
    ON identity_local_auth_attempts (locked_until DESC)
    WHERE locked_until IS NOT NULL;

CREATE TABLE IF NOT EXISTS identity_invitations (
    invite_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    invite_code_hash TEXT NOT NULL,
    invitee_handle_hash TEXT NULL,
    inviter_subject_id_hash TEXT NULL,
    role_id TEXT NOT NULL,
    status TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_by_user_id TEXT NULL REFERENCES identity_users(user_id) ON DELETE SET NULL,
    accepted_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_invitations_code_hash_idx
    ON identity_invitations (invite_code_hash)
    WHERE tombstoned_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_invitations_active_idx
    ON identity_invitations (tenant_id, workspace_id, expires_at DESC)
    WHERE status = 'active' AND accepted_at IS NULL AND revoked_at IS NULL AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_break_glass_windows (
    recovery_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    subject_id_hash TEXT NOT NULL,
    break_glass_code_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    enabled_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ NULL,
    used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_break_glass_windows_code_hash_idx
    ON identity_break_glass_windows (break_glass_code_hash)
    WHERE disabled_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_break_glass_windows_active_idx
    ON identity_break_glass_windows (tenant_id, workspace_id, expires_at DESC)
    WHERE status = 'active' AND disabled_at IS NULL AND used_at IS NULL;
`
