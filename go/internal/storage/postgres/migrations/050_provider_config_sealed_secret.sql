-- Adds the encrypted write-only secret column for DB-backed identity provider
-- configuration (epic #4962, #4966). sealed_secret holds an ESK1 envelope
-- (see go/internal/secretcrypto) sealing a small JSON blob:
--   OIDC: {"client_secret":"..."}
--   SAML: {"sp_private_key":"...","sp_certificate":"..."}
-- The secret lives on the REVISION, not a side table: revisions are already
-- the immutable ledger (identity_provider_config_revisions, see
-- 006e_identity_subjects.sql:45), and revert already activates a prior
-- revision via identity_provider_configs.active_revision_id
-- (006e_identity_subjects.sql:30, partial index :59) — activating an older
-- revision therefore restores that revision's sealed secret automatically,
-- with no separate revert-time secret copy.
ALTER TABLE identity_provider_config_revisions
    ADD COLUMN IF NOT EXISTS sealed_secret TEXT NULL;

-- Adds the non-secret configuration column, needed for the CRUD API to be
-- functional: identity_provider_configs only ever stored HASHES of
-- issuer/metadata_url/entity_id/client_id (issuer_hash, metadata_url_hash,
-- entity_id_hash, client_id_hash — see 006e_identity_subjects.sql:25-29),
-- because the pre-#4966 schema was a correlation/dedup table for env-file
-- -backed providers, not a source of truth for the provider's own settings.
-- A DB-backed CRUD provider has nowhere else to durably hold its non-secret
-- settings (issuer, client_id, scopes, group_claim for OIDC; metadata_url,
-- entity_id, group_attribute for SAML) for the admin to read back or for the
-- test-connection / login paths to use. This column is scoped strictly to
-- non-secret configuration; it is a JSON-encoded TEXT blob to match the
-- existing "every identity column is TEXT" convention documented in
-- go/internal/secretcrypto/README.md. This is a deliberate addition beyond
-- the originally scoped "sealed_secret only" migration text — see the #4966
-- executor report for the explicit call-out.
ALTER TABLE identity_provider_config_revisions
    ADD COLUMN IF NOT EXISTS configuration TEXT NULL;
