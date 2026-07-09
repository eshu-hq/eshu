-- 052_identity_sign_in_policy.sql
--
-- Tenant sign-in policy (epic #4962, issue #4968): one row per tenant
-- controlling require-SSO, local account creation, MFA-for-all-users, and
-- session lifetime overrides. require_sso is guarded application-side
-- (go/internal/storage/postgres/identity_sign_in_policy.go) so it can never
-- be set true unless the tenant has at least one active (passing-test)
-- provider config AND at least one admin has proven an SSO sign-in
-- (sso_admin_verified_at). Break-glass local admin sign-in stays reachable
-- regardless of require_sso — this table never gates that path.
--
-- sso_admin_verified_at/sso_admin_verified_provider_config_id are sticky:
-- once an admin completes one SSO sign-in, the columns are set once and never
-- cleared, matching the guardrail's "has this ever happened" semantics (an
-- admin later losing SSO access, or the verifying provider being disabled,
-- does not retroactively invalidate a still-enabled require_sso policy).
--
-- policy_revision_hash and updated_at default to '' / now() so a first
-- read/guardrail-check before any admin write ever happens against a
-- not-yet-materialized row (created lazily by the row-lock helper in
-- UpsertSignInPolicy) still has a definite, non-NULL value.

CREATE TABLE IF NOT EXISTS identity_sign_in_policies (
    tenant_id TEXT PRIMARY KEY REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    require_sso BOOLEAN NOT NULL DEFAULT FALSE,
    allow_local_user_creation BOOLEAN NOT NULL DEFAULT TRUE,
    require_mfa_for_all_users BOOLEAN NOT NULL DEFAULT FALSE,
    idle_timeout_seconds INTEGER NULL,
    absolute_timeout_seconds INTEGER NULL,
    sso_admin_verified_at TIMESTAMPTZ NULL,
    sso_admin_verified_provider_config_id TEXT NULL,
    policy_revision_hash TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
