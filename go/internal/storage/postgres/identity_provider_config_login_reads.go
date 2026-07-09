// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// selectActiveProviderConfigForLoginQuery reads the active revision's
// sealed_secret (ciphertext) and configuration for the LOGIN runtime ONLY
// (#4966, epic #4962) — unlike selectProviderConfigConnectionTestMaterialQuery,
// this REQUIRES c.status = 'active': a draft (not-yet-enabled, or disabled)
// provider config must never be usable to authenticate, only to test.
const selectActiveProviderConfigForLoginQuery = `
SELECT
    c.provider_kind,
    r.revision_id,
    r.sealed_secret,
    r.configuration
FROM identity_provider_configs c
JOIN identity_provider_config_revisions r
    ON r.provider_config_id = c.provider_config_id AND r.revision_id = c.active_revision_id
WHERE c.provider_config_id = $1 AND c.tenant_id = $2 AND c.status = 'active' AND c.tombstoned_at IS NULL
`

// GetActiveProviderConfigForLogin returns the active revision's ciphertext
// and non-secret configuration for a DB-backed provider config that is
// currently ENABLED (status='active'), scoped to the caller's tenant.
// found=false for a missing, draft, disabled, or tombstoned provider config —
// login must never proceed against a provider that has not passed Enable's
// test-connection gate (see EnableProviderConfig).
//
// This method returns ciphertext, never plaintext — it never calls
// (*secretcrypto.Keyring).Open. The caller (cmd/api, on behalf of
// oidclogin.Service via the DBProviderResolver interface) must hand the
// ciphertext to oidclogin.ResolveSealedProviderConfig, which is the actual
// Open call site, confined to the oidclogin login/authn package per the epic
// #4962 boundary.
func (s *IdentitySubjectStore) GetActiveProviderConfigForLogin(
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
	rows, err := s.db.QueryContext(ctx, selectActiveProviderConfigForLoginQuery, providerConfigID, tenantID)
	if err != nil {
		return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("select active provider config for login: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("select active provider config for login: %w", err)
		}
		return ProviderConfigConnectionTestMaterial{}, false, nil
	}
	material := ProviderConfigConnectionTestMaterial{ProviderConfigID: providerConfigID}
	var sealedSecret, configuration sql.NullString
	if err := rows.Scan(&material.ProviderKind, &material.RevisionID, &sealedSecret, &configuration); err != nil {
		return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("scan active provider config for login: %w", err)
	}
	if sealedSecret.Valid {
		material.SealedSecret = sealedSecret.String
	}
	if configuration.Valid {
		material.Configuration = configuration.String
	}
	return material, true, rows.Err()
}
