-- GitHub Authorization Code (plain OAuth2) login state, issue #5166 (F-5).
-- Mirrors identity_oidc_login_states (006g_identity_oidc_login.sql) minus
-- its ID-token nonce column: plain OAuth2 issues no ID token and has no
-- nonce concept, so the state_hash row itself is the only CSRF control.
-- Team→role grant resolution reuses identity_provider_group_role_mappings
-- unchanged (no provider_kind column there — see
-- go/internal/githublogin/doc.go), so no new mapping table is needed here.
CREATE TABLE IF NOT EXISTS identity_github_login_states (
    state_hash TEXT PRIMARY KEY,
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    redirect_uri_hash TEXT NOT NULL,
    return_to_path TEXT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_github_login_states_active_idx
    ON identity_github_login_states (provider_config_id, expires_at DESC)
    WHERE consumed_at IS NULL;
