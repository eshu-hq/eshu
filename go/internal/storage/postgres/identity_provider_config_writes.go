// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres (this file): DB-backed identity provider-config CRUD
// writes (#4966, epic #4962). Every write that carries a secret seals it with
// (*IdentitySubjectStore).providerSecretKeyring before it reaches SQL; this
// file never calls (*secretcrypto.Keyring).Open, matching the epic's
// boundary: Open is confined to login/authn packages (oidclogin, samlauth).
//
// Design decision — Update always requires the full plaintext secret to be
// resupplied, never a partial (secret-omitted) edit: the AAD binds each
// sealed envelope to (provider_config_id, revision_id) specifically so a
// ciphertext cannot be copied forward to a new revision id (that is the
// intended cut-and-paste defense, not a limitation to work around). Carrying
// a secret forward across a metadata-only edit would require opening it in
// this layer, which the epic's read-path boundary forbids. Write-only secret
// fields commonly require re-entry on every edit for exactly this reason;
// this store enforces it by returning an error when PlaintextSecret is empty
// on Create or Update.
//
// Design decision — the provider's identity/dedup key (provider_key_hash,
// issuer_hash, client_id_hash, metadata_url_hash, entity_id_hash) is set once
// at CreateProviderConfig and never recomputed by Update or Revert. Changing
// an IdP's issuer or entity id is a bigger structural change than an
// in-place edit (it is a different identity for uniqueness/dedup purposes);
// callers that need that must tombstone and recreate. This keeps Update and
// Revert from needing to parse the non-secret Configuration JSON to
// re-derive hashes, and keeps those four hash columns write-once as their
// "never selected, correlation only" doc comments already imply.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CreateProviderConfig creates a new provider config in draft status with one
// active revision carrying the sealed secret. It is idempotent under the
// tenant/kind/key unique index: a duplicate create returns
// ErrProviderConfigDuplicateKey without writing a second row.
func (s *IdentitySubjectStore) CreateProviderConfig(
	ctx context.Context,
	create ProviderConfigCreate,
) (ProviderConfigWriteResult, error) {
	if s.db == nil {
		return ProviderConfigWriteResult{}, errors.New("identity subject store database is required")
	}
	create.ProviderConfigID = strings.TrimSpace(create.ProviderConfigID)
	create.TenantID = strings.TrimSpace(create.TenantID)
	create.ProviderKind = strings.TrimSpace(create.ProviderKind)
	create.ProviderKeyHash = strings.TrimSpace(create.ProviderKeyHash)
	create.RevisionID = strings.TrimSpace(create.RevisionID)
	if create.ProviderConfigID == "" || create.TenantID == "" || create.ProviderKind == "" ||
		create.ProviderKeyHash == "" || create.RevisionID == "" {
		return ProviderConfigWriteResult{}, errors.New("provider_config_id, tenant_id, provider_kind, provider_key_hash, and revision_id are required")
	}
	if strings.TrimSpace(create.PlaintextSecret) == "" {
		return ProviderConfigWriteResult{}, errors.New("a secret is required to create a provider config")
	}
	sealed, err := s.sealProviderSecret(create.ProviderConfigID, create.RevisionID, create.PlaintextSecret)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	insertRows, err := tx.QueryContext(
		ctx,
		insertProviderConfigQuery,
		create.ProviderConfigID,
		create.TenantID,
		create.ProviderKind,
		create.ProviderKeyHash,
		nullTextParam(create.IssuerHash),
		nullTextParam(create.MetadataURLHash),
		nullTextParam(create.EntityIDHash),
		nullTextParam(create.ClientIDHash),
		create.Now.UTC(),
	)
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("insert provider config: %w", err)
	}
	inserted, err := scanInsertedID(insertRows)
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("insert provider config: %w", err)
	}
	if !inserted {
		return ProviderConfigWriteResult{}, ErrProviderConfigDuplicateKey
	}

	if _, err := tx.ExecContext(
		ctx,
		insertProviderConfigRevisionQuery,
		create.ProviderConfigID,
		create.RevisionID,
		create.ConfigurationHash,
		nullTextParam(create.MetadataHash),
		sealed,
		nullTextParam(create.Configuration),
		create.Now.UTC(),
	); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("insert provider config revision: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		activateProviderConfigActiveRevisionQuery,
		create.ProviderConfigID,
		create.TenantID,
		create.RevisionID,
		create.Now.UTC(),
	); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("activate provider config revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit create provider config: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: create.ProviderConfigID,
		RevisionID:       create.RevisionID,
		Status:           "draft",
		Found:            true,
		Changed:          true,
	}, nil
}

// UpdateProviderConfig creates a new active revision for an existing provider
// config, superseding the current one. The whole read-lock-write sequence
// runs in one transaction with a row lock on the provider config
// (selectProviderConfigForUpdateQuery ... FOR UPDATE), so a concurrent Update
// or Revert against the same provider_config_id serializes behind it —
// exactly one revision is ever active (concurrency-deadlock-rigor: conflict
// domain is the single identity_provider_configs row for this
// provider_config_id; lock order is always "lock the provider config row,
// then touch its revisions," matching CreateProviderConfig/RevertProviderConfig).
func (s *IdentitySubjectStore) UpdateProviderConfig(
	ctx context.Context,
	update ProviderConfigUpdate,
) (ProviderConfigWriteResult, error) {
	if s.db == nil {
		return ProviderConfigWriteResult{}, errors.New("identity subject store database is required")
	}
	update.ProviderConfigID = strings.TrimSpace(update.ProviderConfigID)
	update.TenantID = strings.TrimSpace(update.TenantID)
	update.RevisionID = strings.TrimSpace(update.RevisionID)
	if update.ProviderConfigID == "" || update.TenantID == "" || update.RevisionID == "" {
		return ProviderConfigWriteResult{}, errors.New("provider_config_id, tenant_id, and revision_id are required")
	}
	if strings.TrimSpace(update.PlaintextSecret) == "" {
		return ProviderConfigWriteResult{}, errors.New("a secret is required on every provider config update; write-only secrets are never carried forward automatically")
	}
	sealed, err := s.sealProviderSecret(update.ProviderConfigID, update.RevisionID, update.PlaintextSecret)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, found, err := lockProviderConfig(ctx, tx, update.ProviderConfigID, update.TenantID)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	if !found {
		return ProviderConfigWriteResult{Found: false}, nil
	}

	if _, err := tx.ExecContext(
		ctx,
		insertProviderConfigRevisionQuery,
		update.ProviderConfigID,
		update.RevisionID,
		update.ConfigurationHash,
		nullTextParam(update.MetadataHash),
		sealed,
		nullTextParam(update.Configuration),
		update.Now.UTC(),
	); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("insert provider config revision: %w", err)
	}
	if current.activeRevisionID != "" {
		if _, err := tx.ExecContext(
			ctx,
			supersedeProviderConfigRevisionQuery,
			update.ProviderConfigID,
			current.activeRevisionID,
			update.Now.UTC(),
		); err != nil {
			return ProviderConfigWriteResult{}, fmt.Errorf("supersede prior provider config revision: %w", err)
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		activateProviderConfigActiveRevisionQuery,
		update.ProviderConfigID,
		update.TenantID,
		update.RevisionID,
		update.Now.UTC(),
	); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("activate provider config revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit update provider config: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: update.ProviderConfigID,
		RevisionID:       update.RevisionID,
		Status:           current.status,
		Found:            true,
		Changed:          true,
	}, nil
}

// RevertProviderConfig activates a prior revision. It never opens or reseals
// any secret: the target revision's sealed_secret was already correctly
// sealed under its own (provider_config_id, revision_id) AAD, so reactivating
// it restores that revision's secret automatically. Idempotent: reverting to
// the already-active revision is a no-op reporting Changed=false. Runs under
// the same row lock as UpdateProviderConfig, so it serializes against
// concurrent updates/reverts on this provider_config_id.
func (s *IdentitySubjectStore) RevertProviderConfig(
	ctx context.Context,
	revert ProviderConfigRevert,
) (ProviderConfigWriteResult, error) {
	if s.db == nil {
		return ProviderConfigWriteResult{}, errors.New("identity subject store database is required")
	}
	revert.ProviderConfigID = strings.TrimSpace(revert.ProviderConfigID)
	revert.TenantID = strings.TrimSpace(revert.TenantID)
	revert.TargetRevisionID = strings.TrimSpace(revert.TargetRevisionID)
	if revert.ProviderConfigID == "" || revert.TenantID == "" || revert.TargetRevisionID == "" {
		return ProviderConfigWriteResult{}, errors.New("provider_config_id, tenant_id, and target_revision_id are required")
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, found, err := lockProviderConfig(ctx, tx, revert.ProviderConfigID, revert.TenantID)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	if !found {
		return ProviderConfigWriteResult{Found: false}, nil
	}
	if current.activeRevisionID == revert.TargetRevisionID {
		if err := tx.Commit(); err != nil {
			return ProviderConfigWriteResult{}, fmt.Errorf("commit revert provider config no-op: %w", err)
		}
		committed = true
		return ProviderConfigWriteResult{
			ProviderConfigID: revert.ProviderConfigID,
			RevisionID:       revert.TargetRevisionID,
			Status:           current.status,
			Found:            true,
			Changed:          false,
		}, nil
	}

	revisionRows, err := tx.QueryContext(ctx, selectProviderConfigRevisionExistsQuery, revert.ProviderConfigID, revert.TargetRevisionID)
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("check target revision exists: %w", err)
	}
	revisionExists, err := scanExists(revisionRows)
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("check target revision exists: %w", err)
	}
	if !revisionExists {
		return ProviderConfigWriteResult{}, ErrProviderConfigRevisionNotFound
	}

	if current.activeRevisionID != "" {
		if _, err := tx.ExecContext(
			ctx,
			supersedeProviderConfigRevisionQuery,
			revert.ProviderConfigID,
			current.activeRevisionID,
			revert.Now.UTC(),
		); err != nil {
			return ProviderConfigWriteResult{}, fmt.Errorf("supersede current provider config revision: %w", err)
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		activateProviderConfigRevisionQuery,
		revert.ProviderConfigID,
		revert.TargetRevisionID,
		revert.Now.UTC(),
	); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("activate target provider config revision: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		activateProviderConfigActiveRevisionQuery,
		revert.ProviderConfigID,
		revert.TenantID,
		revert.TargetRevisionID,
		revert.Now.UTC(),
	); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("point provider config at reverted revision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit revert provider config: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: revert.ProviderConfigID,
		RevisionID:       revert.TargetRevisionID,
		Status:           current.status,
		Found:            true,
		Changed:          true,
	}, nil
}

// EnableProviderConfig transitions a provider config from draft to active.
// The caller must have already confirmed a passing test-connection result for
// the current active revision; this store enforces only that an active
// revision exists, not that it was tested.
func (s *IdentitySubjectStore) EnableProviderConfig(
	ctx context.Context,
	enable ProviderConfigEnable,
) (ProviderConfigWriteResult, error) {
	return s.setProviderConfigStatus(ctx, enable.ProviderConfigID, enable.TenantID, "active", enable.Now)
}

// DisableProviderConfig transitions a provider config from active back to
// draft. Idempotent.
func (s *IdentitySubjectStore) DisableProviderConfig(
	ctx context.Context,
	disable ProviderConfigDisable,
) (ProviderConfigWriteResult, error) {
	return s.setProviderConfigStatus(ctx, disable.ProviderConfigID, disable.TenantID, "draft", disable.Now)
}

func (s *IdentitySubjectStore) setProviderConfigStatus(
	ctx context.Context,
	providerConfigID, tenantID, targetStatus string,
	now time.Time,
) (ProviderConfigWriteResult, error) {
	if s.db == nil {
		return ProviderConfigWriteResult{}, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || tenantID == "" {
		return ProviderConfigWriteResult{}, errors.New("provider_config_id and tenant_id are required")
	}
	rows, err := s.db.QueryContext(ctx, setProviderConfigStatusQuery, providerConfigID, tenantID, targetStatus, now.UTC())
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("set provider config status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ProviderConfigWriteResult{}, fmt.Errorf("set provider config status: %w", err)
		}
		return ProviderConfigWriteResult{Found: false}, nil
	}
	var status string
	if err := rows.Scan(&status); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("scan provider config status: %w", err)
	}
	return ProviderConfigWriteResult{
		ProviderConfigID: providerConfigID,
		Status:           status,
		Found:            true,
		Changed:          status == targetStatus,
	}, rows.Err()
}

// sealProviderSecret constructs the AAD from (provider_config_id,
// revision_id) exactly per the epic #4962 shared crypto contract and seals
// plaintext under the store's configured keyring. It returns
// ErrProviderSecretKeyringUnavailable, never a silent no-op, when no keyring
// is wired — a provider-config write that carries a secret must fail closed,
// not persist an unsealed value or drop it.
func (s *IdentitySubjectStore) sealProviderSecret(providerConfigID, revisionID, plaintext string) (string, error) {
	if s.providerSecretKeyring == nil {
		return "", ErrProviderSecretKeyringUnavailable
	}
	aad := providerSecretAAD(providerConfigID, revisionID)
	sealed, err := s.providerSecretKeyring.Seal([]byte(plaintext), []byte(aad))
	if err != nil {
		return "", fmt.Errorf("seal provider config secret: %w", err)
	}
	return sealed, nil
}

// providerSecretAAD builds the AAD text for a provider-config secret
// envelope. Both Seal (here) and Open (oidclogin/samlauth) must construct
// this identically or decryption fails closed with ErrDecrypt.
func providerSecretAAD(providerConfigID, revisionID string) string {
	return providerSecretAADPrefix + "|" + providerConfigID + "|" + revisionID
}

// lockedProviderConfig is the row-locked snapshot read by
// selectProviderConfigForUpdateQuery.
type lockedProviderConfig struct {
	status           string
	activeRevisionID string
}

// lockProviderConfig row-locks and reads the provider config for the
// duration of the caller's transaction. found=false when no live
// (non-tombstoned) row matches in the tenant.
func lockProviderConfig(ctx context.Context, tx Transaction, providerConfigID, tenantID string) (lockedProviderConfig, bool, error) {
	rows, err := tx.QueryContext(ctx, selectProviderConfigForUpdateQuery, providerConfigID, tenantID)
	if err != nil {
		return lockedProviderConfig{}, false, fmt.Errorf("lock provider config: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return lockedProviderConfig{}, false, fmt.Errorf("lock provider config: %w", err)
		}
		return lockedProviderConfig{}, false, nil
	}
	var current lockedProviderConfig
	var activeRevisionID sql.NullString
	var providerConfigIDOut, providerKindOut string
	if err := rows.Scan(&providerConfigIDOut, &providerKindOut, &current.status, &activeRevisionID); err != nil {
		return lockedProviderConfig{}, false, fmt.Errorf("scan locked provider config: %w", err)
	}
	if activeRevisionID.Valid {
		current.activeRevisionID = activeRevisionID.String
	}
	return current, true, rows.Err()
}

// scanInsertedID reports whether an ON CONFLICT ... DO NOTHING RETURNING
// query actually inserted a row (a conflict produces zero returned rows).
func scanInsertedID(rows Rows) (bool, error) {
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		return false, err
	}
	return true, rows.Err()
}

// scanExists reports whether a `SELECT 1 ... LIMIT 1` query matched a row.
func scanExists(rows Rows) (bool, error) {
	defer func() { _ = rows.Close() }()
	exists := rows.Next()
	return exists, rows.Err()
}

// nullTextParam converts an empty string to a SQL NULL so optional hash and
// configuration columns stay NULL rather than storing an empty string.
func nullTextParam(value string) any {
	if value == "" {
		return nil
	}
	return value
}
