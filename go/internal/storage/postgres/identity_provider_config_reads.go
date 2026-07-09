// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// secretFingerprintHexLen mirrors secretcrypto's own default KeyID fingerprint
// length (first 8 hex chars of a SHA-256 digest) so operators see a
// consistently-shaped short hash across the envelope's key_id and this
// admin-facing secret_fingerprint. The fingerprint is computed over the
// ENVELOPE CIPHERTEXT TEXT, never the plaintext secret — this file never
// calls (*secretcrypto.Keyring).Open.
const secretFingerprintHexLen = 8

// selectProviderConfigDetailQuery reads one provider config's metadata plus
// its active revision's sealed_secret (ciphertext, not plaintext) and
// non-secret configuration. sealed_secret is read here ONLY so
// GetProviderConfigDetail can derive HasSecret/SecretFingerprint/SecretKeyID
// from it in Go without ever storing or returning the ciphertext itself.
const selectProviderConfigDetailQuery = `
SELECT
    c.provider_config_id,
    c.tenant_id,
    c.provider_kind,
    c.status,
    c.active_revision_id,
    c.created_at,
    c.updated_at,
    r.sealed_secret,
    r.configuration
FROM identity_provider_configs c
LEFT JOIN identity_provider_config_revisions r
    ON r.provider_config_id = c.provider_config_id AND r.revision_id = c.active_revision_id
WHERE c.provider_config_id = $1 AND c.tenant_id = $2 AND c.tombstoned_at IS NULL
`

// selectProviderConfigsQuery lists every non-tombstoned provider config in a
// tenant plus its active revision's sealed_secret/configuration, for the same
// derivation GetProviderConfigDetail performs (has_secret/fingerprint/key_id,
// never plaintext). Includes draft-status rows so an admin sees configs that
// have not yet passed a test-connection.
const selectProviderConfigsQuery = `
SELECT
    c.provider_config_id,
    c.tenant_id,
    c.provider_kind,
    c.status,
    c.active_revision_id,
    c.created_at,
    c.updated_at,
    r.sealed_secret,
    r.configuration
FROM identity_provider_configs c
LEFT JOIN identity_provider_config_revisions r
    ON r.provider_config_id = c.provider_config_id AND r.revision_id = c.active_revision_id
WHERE c.tenant_id = $1 AND c.tombstoned_at IS NULL
ORDER BY c.provider_config_id ASC
LIMIT 500
`

// selectProviderConfigRevisionsQuery lists a provider config's revision
// history, metadata only. (sealed_secret IS NOT NULL) is selected as a
// boolean, never the envelope text itself.
const selectProviderConfigRevisionsQuery = `
SELECT
    r.revision_id,
    r.status,
    (r.sealed_secret IS NOT NULL) AS has_secret,
    r.created_at,
    r.activated_at,
    r.superseded_at
FROM identity_provider_config_revisions r
JOIN identity_provider_configs c ON c.provider_config_id = r.provider_config_id
WHERE r.provider_config_id = $1 AND c.tenant_id = $2 AND c.tombstoned_at IS NULL
ORDER BY r.created_at DESC
LIMIT 200
`

// selectProviderConfigConnectionTestMaterialQuery reads the active revision's
// sealed_secret and configuration for the test-connection orchestration path
// ONLY (see ProviderConfigConnectionTestMaterial's doc comment). It is
// tenant-scoped and excludes tombstoned/draft-with-no-revision configs.
const selectProviderConfigConnectionTestMaterialQuery = `
SELECT
    c.provider_kind,
    r.revision_id,
    r.sealed_secret,
    r.configuration
FROM identity_provider_configs c
JOIN identity_provider_config_revisions r
    ON r.provider_config_id = c.provider_config_id AND r.revision_id = c.active_revision_id
WHERE c.provider_config_id = $1 AND c.tenant_id = $2 AND c.tombstoned_at IS NULL
`

// GetProviderConfigDetail returns the metadata-only admin view of one
// provider config. It never calls (*secretcrypto.Keyring).Open and never
// returns sealed_secret; HasSecret/SecretFingerprint/SecretKeyID are derived
// from the envelope's own text structure and a hash of its ciphertext.
func (s *IdentitySubjectStore) GetProviderConfigDetail(
	ctx context.Context,
	providerConfigID, tenantID string,
) (ProviderConfigDetail, bool, error) {
	if s.db == nil {
		return ProviderConfigDetail{}, false, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || tenantID == "" {
		return ProviderConfigDetail{}, false, errors.New("provider_config_id and tenant_id are required")
	}
	rows, err := s.db.QueryContext(ctx, selectProviderConfigDetailQuery, providerConfigID, tenantID)
	if err != nil {
		return ProviderConfigDetail{}, false, fmt.Errorf("select provider config detail: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ProviderConfigDetail{}, false, fmt.Errorf("select provider config detail: %w", err)
		}
		return ProviderConfigDetail{}, false, nil
	}
	var detail ProviderConfigDetail
	var activeRevisionID, sealedSecret, configuration sql.NullString
	if err := rows.Scan(
		&detail.ProviderConfigID,
		&detail.TenantID,
		&detail.ProviderKind,
		&detail.Status,
		&activeRevisionID,
		&detail.CreatedAt,
		&detail.UpdatedAt,
		&sealedSecret,
		&configuration,
	); err != nil {
		return ProviderConfigDetail{}, false, fmt.Errorf("scan provider config detail: %w", err)
	}
	if activeRevisionID.Valid {
		detail.ActiveRevisionID = activeRevisionID.String
	}
	if configuration.Valid {
		detail.Configuration = configuration.String
	}
	if sealedSecret.Valid && sealedSecret.String != "" {
		detail.HasSecret = true
		detail.SecretFingerprint = fingerprintCiphertext(sealedSecret.String)
		detail.SecretKeyID = envelopeKeyID(sealedSecret.String)
	}
	detail.CreatedAt = detail.CreatedAt.UTC()
	detail.UpdatedAt = detail.UpdatedAt.UTC()
	return detail, true, rows.Err()
}

// ListProviderConfigs returns every non-tombstoned provider config in a
// tenant, metadata only (same derivation as GetProviderConfigDetail — never
// calls Open, never returns sealed_secret).
func (s *IdentitySubjectStore) ListProviderConfigs(
	ctx context.Context,
	tenantID string,
) ([]ProviderConfigDetail, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	rows, err := s.db.QueryContext(ctx, selectProviderConfigsQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list provider configs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []ProviderConfigDetail
	for rows.Next() {
		var detail ProviderConfigDetail
		var activeRevisionID, sealedSecret, configuration sql.NullString
		if err := rows.Scan(
			&detail.ProviderConfigID,
			&detail.TenantID,
			&detail.ProviderKind,
			&detail.Status,
			&activeRevisionID,
			&detail.CreatedAt,
			&detail.UpdatedAt,
			&sealedSecret,
			&configuration,
		); err != nil {
			return nil, fmt.Errorf("scan provider config: %w", err)
		}
		if activeRevisionID.Valid {
			detail.ActiveRevisionID = activeRevisionID.String
		}
		if configuration.Valid {
			detail.Configuration = configuration.String
		}
		if sealedSecret.Valid && sealedSecret.String != "" {
			detail.HasSecret = true
			detail.SecretFingerprint = fingerprintCiphertext(sealedSecret.String)
			detail.SecretKeyID = envelopeKeyID(sealedSecret.String)
		}
		detail.CreatedAt = detail.CreatedAt.UTC()
		detail.UpdatedAt = detail.UpdatedAt.UTC()
		items = append(items, detail)
	}
	return items, rows.Err()
}

// ListProviderConfigRevisions returns the revision history for one provider
// config, metadata only. Never returns sealed_secret.
func (s *IdentitySubjectStore) ListProviderConfigRevisions(
	ctx context.Context,
	providerConfigID, tenantID string,
) ([]ProviderConfigRevisionItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || tenantID == "" {
		return nil, errors.New("provider_config_id and tenant_id are required")
	}
	rows, err := s.db.QueryContext(ctx, selectProviderConfigRevisionsQuery, providerConfigID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list provider config revisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []ProviderConfigRevisionItem
	for rows.Next() {
		var item ProviderConfigRevisionItem
		var activatedAt, supersededAt sql.NullTime
		if err := rows.Scan(&item.RevisionID, &item.Status, &item.HasSecret, &item.CreatedAt, &activatedAt, &supersededAt); err != nil {
			return nil, fmt.Errorf("scan provider config revision: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		if activatedAt.Valid {
			item.ActivatedAt = activatedAt.Time.UTC()
		}
		if supersededAt.Valid {
			item.SupersededAt = supersededAt.Time.UTC()
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetProviderConfigConnectionTestMaterial returns the active revision's
// sealed_secret (ciphertext) and configuration for the test-connection
// orchestration path ONLY. Callers (cmd/api) must hand the ciphertext to a
// login/authn package (oidclogin, samlauth) to Open transiently and must
// never serialize it into any API response, audit event, log, or metric.
// This store never calls Open itself.
func (s *IdentitySubjectStore) GetProviderConfigConnectionTestMaterial(
	ctx context.Context,
	providerConfigID, tenantID string,
) (ProviderConfigConnectionTestMaterial, bool, error) {
	if s.db == nil {
		return ProviderConfigConnectionTestMaterial{}, false, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || tenantID == "" {
		return ProviderConfigConnectionTestMaterial{}, false, errors.New("provider_config_id and tenant_id are required")
	}
	rows, err := s.db.QueryContext(ctx, selectProviderConfigConnectionTestMaterialQuery, providerConfigID, tenantID)
	if err != nil {
		return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("select provider config connection test material: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("select provider config connection test material: %w", err)
		}
		return ProviderConfigConnectionTestMaterial{}, false, nil
	}
	material := ProviderConfigConnectionTestMaterial{ProviderConfigID: providerConfigID}
	var sealedSecret, configuration sql.NullString
	if err := rows.Scan(&material.ProviderKind, &material.RevisionID, &sealedSecret, &configuration); err != nil {
		return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("scan provider config connection test material: %w", err)
	}
	if sealedSecret.Valid {
		material.SealedSecret = sealedSecret.String
	}
	if configuration.Valid {
		material.Configuration = configuration.String
	}
	return material, true, rows.Err()
}

// fingerprintCiphertext returns a short, non-reversible fingerprint of an
// envelope's ciphertext bytes so an admin can see "did the secret change"
// without ever exposing or opening the secret. Hashing the envelope text
// (not the plaintext) keeps this derivable from the read path alone.
func fingerprintCiphertext(sealedSecret string) string {
	sum := sha256.Sum256([]byte(sealedSecret))
	return hex.EncodeToString(sum[:])[:secretFingerprintHexLen]
}

// envelopeKeyID extracts the key_id field from an ESK1 envelope's text
// ("ESK1.<key_id>.<nonce>.<ciphertext>") without any cryptographic work — it
// is a plain string split, safe for the read path. Returns "" for a
// malformed envelope rather than panicking; a malformed envelope can only
// arise from a bug or tampering, and this read path never treats it as
// fatal (the admin still sees has_secret=true, just without a key_id label).
func envelopeKeyID(sealedSecret string) string {
	parts := strings.SplitN(sealedSecret, ".", 4)
	if len(parts) != 4 || parts[0] != "ESK1" {
		return ""
	}
	return parts[1]
}
