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
	"errors"
	"fmt"
	"strings"
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
	// provider_kind is immutable after CreateProviderConfig (see package doc
	// comment). Rejecting a mismatch here — rather than silently accepting it
	// — stops an update request whose provider_kind disagrees with the
	// existing row from storing a configuration/secret JSON shape (SAML
	// fields under an OIDC provider_kind, or vice versa) that provider_kind
	// -driven consumers (oidclogin.TestConnection, samlauth.TestConnection)
	// would then fail to parse.
	if update.ProviderKind != "" && current.providerKind != update.ProviderKind {
		return ProviderConfigWriteResult{}, ErrProviderConfigKindMismatch
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
	activateRows, err := tx.QueryContext(
		ctx,
		activateProviderConfigActiveRevisionQuery,
		update.ProviderConfigID,
		update.TenantID,
		update.RevisionID,
		update.Now.UTC(),
	)
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("activate provider config revision: %w", err)
	}
	// Read the status back from RETURNING rather than reusing current.status
	// (captured under the row lock BEFORE this UPDATE ran): this statement
	// unconditionally resets status to 'draft' (see the query's doc comment),
	// so current.status is stale the instant this UPDATE commits (#4988).
	var postUpdateStatus string
	scannedStatus := activateRows.Next()
	if scannedStatus {
		if err := activateRows.Scan(&postUpdateStatus); err != nil {
			_ = activateRows.Close()
			return ProviderConfigWriteResult{}, fmt.Errorf("scan activated provider config status: %w", err)
		}
	}
	rowsErr := activateRows.Err()
	if err := activateRows.Close(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("close activated provider config status rows: %w", err)
	}
	if rowsErr != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("activate provider config revision: %w", rowsErr)
	}
	if !scannedStatus {
		// The row-locked read above (lockProviderConfig) found it; a
		// concurrent tombstone between that lock and this UPDATE is the only
		// way this branch is reached.
		return ProviderConfigWriteResult{Found: false}, nil
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit update provider config: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: update.ProviderConfigID,
		RevisionID:       update.RevisionID,
		Status:           postUpdateStatus,
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
	// Read the post-UPDATE status back from RETURNING rather than reusing
	// current.status: activateProviderConfigActiveRevisionQuery unconditionally
	// resets status to 'draft' whenever the active revision changes, so
	// current.status is stale the instant this UPDATE commits. Same defect and
	// same fix as UpdateProviderConfig above (#4988, PR #5057 self-review P1).
	revertRows, err := tx.QueryContext(
		ctx,
		activateProviderConfigActiveRevisionQuery,
		revert.ProviderConfigID,
		revert.TenantID,
		revert.TargetRevisionID,
		revert.Now.UTC(),
	)
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("point provider config at reverted revision: %w", err)
	}
	var revertedStatus string
	if revertRows.Next() {
		if err := revertRows.Scan(&revertedStatus); err != nil {
			_ = revertRows.Close()
			return ProviderConfigWriteResult{}, fmt.Errorf("scan reverted provider config status: %w", err)
		}
	}
	rowsErr := revertRows.Err()
	if err := revertRows.Close(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("close reverted provider config status rows: %w", err)
	}
	if rowsErr != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("point provider config at reverted revision: %w", rowsErr)
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit revert provider config: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: revert.ProviderConfigID,
		RevisionID:       revert.TargetRevisionID,
		Status:           revertedStatus,
		Found:            true,
		Changed:          true,
	}, nil
}

// See identity_provider_config_writes_helpers.go for sealProviderSecret,
// providerSecretAAD, lockedProviderConfig/lockProviderConfig, scanInsertedID,
// scanExists, and nullTextParam.
