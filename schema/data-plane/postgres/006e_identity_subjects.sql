CREATE TABLE IF NOT EXISTS identity_users (
    user_id TEXT PRIMARY KEY,
    subject_id_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    profile_handle_hash TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_users_subject_hash_idx
    ON identity_users (subject_id_hash);

CREATE INDEX IF NOT EXISTS identity_users_active_idx
    ON identity_users (user_id, updated_at DESC)
    WHERE status = 'active' AND disabled_at IS NULL AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_provider_configs (
    provider_config_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL,
    provider_key_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    issuer_hash TEXT NULL,
    metadata_url_hash TEXT NULL,
    entity_id_hash TEXT NULL,
    client_id_hash TEXT NULL,
    credential_handle TEXT NULL,
    active_revision_id TEXT NULL,
    duplicate_of_provider_config_id TEXT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    tombstoned_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_provider_configs_tenant_kind_key_idx
    ON identity_provider_configs (tenant_id, provider_kind, provider_key_hash)
    WHERE tombstoned_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_provider_configs_active_idx
    ON identity_provider_configs (tenant_id, provider_kind, updated_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_provider_config_revisions (
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    revision_id TEXT NOT NULL,
    status TEXT NOT NULL,
    configuration_hash TEXT NOT NULL,
    metadata_hash TEXT NULL,
    metadata_handle TEXT NULL,
    credential_handle TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    PRIMARY KEY (provider_config_id, revision_id)
);

CREATE INDEX IF NOT EXISTS identity_provider_config_revisions_active_idx
    ON identity_provider_config_revisions (provider_config_id, activated_at DESC)
    WHERE status = 'active' AND superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_saml_authn_requests (
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    request_id_hash TEXT NOT NULL,
    relay_state_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_config_id, request_id_hash)
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_saml_authn_requests_relay_state_idx
    ON identity_saml_authn_requests (provider_config_id, relay_state_hash)
    WHERE consumed_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_saml_authn_requests_pending_idx
    ON identity_saml_authn_requests (provider_config_id, expires_at)
    WHERE status = 'pending' AND consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_saml_replay_keys (
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    replay_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_config_id, replay_hash)
);

CREATE INDEX IF NOT EXISTS identity_saml_replay_keys_expiry_idx
    ON identity_saml_replay_keys (expires_at);

CREATE TABLE IF NOT EXISTS identity_user_emails (
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    email_hash TEXT NOT NULL,
    provider_config_id TEXT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE SET NULL,
    email_status TEXT NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    verified_at TIMESTAMPTZ NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    superseded_at TIMESTAMPTZ NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, email_hash, effective_at)
);

CREATE INDEX IF NOT EXISTS identity_user_emails_current_idx
    ON identity_user_emails (user_id, email_hash, updated_at DESC)
    WHERE superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_user_emails_provider_idx
    ON identity_user_emails (provider_config_id, email_hash, observed_at DESC)
    WHERE provider_config_id IS NOT NULL AND superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_external_subjects (
    external_identity_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    provider_config_id TEXT NOT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE CASCADE,
    external_subject_id_hash TEXT NOT NULL,
    external_subject_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    email_hash TEXT NULL,
    group_claims_hash TEXT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_external_subjects_provider_subject_idx
    ON identity_external_subjects (provider_config_id, external_subject_id_hash)
    WHERE tombstoned_at IS NULL;

CREATE INDEX IF NOT EXISTS identity_external_subjects_user_idx
    ON identity_external_subjects (user_id, provider_config_id, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS identity_local_credentials (
    credential_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    password_algorithm TEXT NOT NULL,
    password_parameters_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS identity_local_credentials_user_active_idx
    ON identity_local_credentials (user_id, rotated_at DESC)
    WHERE status = 'active' AND revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_mfa_factors (
    factor_id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    factor_kind TEXT NOT NULL,
    status TEXT NOT NULL,
    secret_credential_handle TEXT NULL,
    public_key_hash TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ NULL,
    last_used_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS identity_mfa_factors_user_active_idx
    ON identity_mfa_factors (user_id, factor_kind, created_at DESC)
    WHERE status = 'active' AND revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_mfa_recovery_codes (
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    factor_id TEXT NOT NULL REFERENCES identity_mfa_factors(factor_id) ON DELETE CASCADE,
    recovery_code_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    PRIMARY KEY (user_id, recovery_code_hash)
);

CREATE INDEX IF NOT EXISTS identity_mfa_recovery_codes_factor_idx
    ON identity_mfa_recovery_codes (factor_id, created_at DESC);

CREATE TABLE IF NOT EXISTS identity_tenant_memberships (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    membership_source TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    disabled_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, user_id),
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_tenant_memberships_active_idx
    ON identity_tenant_memberships (tenant_id, workspace_id, user_id, effective_at DESC)
    WHERE status = 'active' AND disabled_at IS NULL AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_roles (
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    role_id TEXT NOT NULL,
    role_key_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    built_in BOOLEAN NOT NULL DEFAULT false,
    policy_revision_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    PRIMARY KEY (tenant_id, role_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_roles_tenant_key_idx
    ON identity_roles (tenant_id, role_key_hash)
    WHERE tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_role_grants (
    tenant_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    grant_id TEXT NOT NULL,
    action TEXT NOT NULL,
    feature TEXT NOT NULL,
    data_class TEXT NOT NULL,
    scope_class TEXT NOT NULL,
    scope_id_hash TEXT NULL,
    repository_id_hash TEXT NULL,
    status TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, role_id, grant_id),
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_role_grants_active_idx
    ON identity_role_grants (tenant_id, role_id, action, feature, data_class, effective_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_membership_roles (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    assignment_source TEXT NOT NULL,
    status TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, user_id, role_id),
    FOREIGN KEY (tenant_id, workspace_id, user_id)
        REFERENCES identity_tenant_memberships(tenant_id, workspace_id, user_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_membership_roles_active_idx
    ON identity_membership_roles (tenant_id, workspace_id, user_id, role_id, effective_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_sessions (
    session_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    provider_config_id TEXT NULL REFERENCES identity_provider_configs(provider_config_id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    mfa_state TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    csrf_token_hash TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_sessions_active_idx
    ON identity_sessions (user_id, tenant_id, workspace_id, expires_at DESC)
    WHERE status = 'active' AND revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_service_principals (
    service_principal_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    owner_user_id TEXT NULL REFERENCES identity_users(user_id) ON DELETE SET NULL,
    status TEXT NOT NULL,
    display_handle_hash TEXT NULL,
    policy_revision_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    disabled_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    UNIQUE (tenant_id, workspace_id, service_principal_id),
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_service_principals_active_idx
    ON identity_service_principals (tenant_id, workspace_id, service_principal_id, updated_at DESC)
    WHERE status = 'active' AND disabled_at IS NULL AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_service_principal_roles (
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    service_principal_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    assignment_source TEXT NOT NULL,
    status TEXT NOT NULL,
    policy_revision_hash TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, workspace_id, service_principal_id, role_id),
    FOREIGN KEY (tenant_id, workspace_id, service_principal_id)
        REFERENCES identity_service_principals(tenant_id, workspace_id, service_principal_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id)
        REFERENCES identity_roles(tenant_id, role_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS identity_service_principal_roles_active_idx
    ON identity_service_principal_roles (tenant_id, workspace_id, service_principal_id, role_id, effective_at DESC)
    WHERE status = 'active' AND tombstoned_at IS NULL;

CREATE TABLE IF NOT EXISTS identity_token_metadata (
    token_id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL,
    token_class TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    user_id TEXT NULL REFERENCES identity_users(user_id) ON DELETE CASCADE,
    service_principal_id TEXT NULL REFERENCES identity_service_principals(service_principal_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    display_handle_hash TEXT NULL,
    policy_revision_hash TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    last_used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (tenant_id, workspace_id)
        REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE,
    CHECK (
        (token_class = 'personal' AND user_id IS NOT NULL AND service_principal_id IS NULL)
        OR (token_class = 'service_principal' AND service_principal_id IS NOT NULL AND user_id IS NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS identity_token_metadata_hash_idx
    ON identity_token_metadata (token_hash);

CREATE INDEX IF NOT EXISTS identity_token_metadata_active_idx
    ON identity_token_metadata (tenant_id, workspace_id, token_class, updated_at DESC)
    WHERE status = 'active' AND revoked_at IS NULL;

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
