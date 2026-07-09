-- 051_identity_bootstrap_credential.sql
--
-- Stores the one-time generated admin bootstrap credential envelope
-- (epic #4962, #4963) for the first local owner/admin identity created by
-- ESHU_AUTH_BOOTSTRAP_MODE=generated. sealed_credential is the only
-- reversible copy of the generated plaintext password: it is an ESK1
-- envelope (go/internal/secretcrypto) that only an operator holding the
-- configured DEK (ESHU_AUTH_SECRET_ENC_KEY) can open, and only through the
-- `eshu admin initial-credential` CLI or the one-time startup banner.
--
-- The row is destroyed-on-first-login by clearing sealed_credential and
-- setting consumed_at, never by deleting the row: the row survives for
-- audit and for the setup-seal state the first-run setup route reads
-- (#4965). Retrieval reads WHERE consumed_at IS NULL AND
-- sealed_credential <> ''. Generation is idempotent via
-- INSERT ... ON CONFLICT (tenant_id, workspace_id) DO NOTHING, guarded by
-- pg_advisory_xact_lock(3456) (3455 is already used by
-- BootstrapLocalIdentity's local-identity advisory lock).

CREATE TABLE IF NOT EXISTS identity_bootstrap_credentials (
    tenant_id TEXT NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL,
    subject_id_hash TEXT NOT NULL,
    username_hash TEXT NOT NULL,
    sealed_credential TEXT NOT NULL,
    key_id TEXT NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ NULL,
    reset_at TIMESTAMPTZ NULL,
    reset_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, workspace_id)
);
