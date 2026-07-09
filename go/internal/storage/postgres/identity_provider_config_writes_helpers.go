// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

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
	providerKind     string
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
	var providerConfigIDOut string
	if err := rows.Scan(&providerConfigIDOut, &current.providerKind, &current.status, &activeRevisionID); err != nil {
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
