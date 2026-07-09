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

// selectActiveSAMLProviderConfigForLoginQuery reads the active revision's
// sealed_secret (ciphertext) and configuration for the SAML LOGIN runtime
// ONLY (#4966, epic #4962; completes #4978) — like
// selectActiveProviderConfigForLoginQuery (OIDC), this REQUIRES
// c.status = 'active': a draft or disabled provider config must never be
// usable to authenticate, only to test.
//
// Unlike selectActiveProviderConfigForLoginQuery, this is NOT tenant-scoped:
// the SAML routes (/api/v0/auth/saml/providers/{provider_id}/...) carry no
// tenant_id, matching every other SAML login-time lookup in this codebase
// (HasActiveSAMLProviderConfig, resolveSAMLExternalSubjectRoles) — tenant
// scoping for a SAML session happens later, when ResolveSAMLExternalSubject
// maps the validated principal to a tenant/workspace membership. The join
// against tenants still requires the owning tenant itself to be active, same
// as selectActiveSAMLProviderConfigQuery.
const selectActiveSAMLProviderConfigForLoginQuery = `
SELECT
    c.provider_kind,
    r.revision_id,
    r.sealed_secret,
    r.configuration
FROM identity_provider_configs c
JOIN identity_provider_config_revisions r
    ON r.provider_config_id = c.provider_config_id AND r.revision_id = c.active_revision_id
JOIN tenants t
    ON t.tenant_id = c.tenant_id
WHERE c.provider_config_id = $1
  AND c.provider_kind = 'external_saml'
  AND c.status = 'active'
  AND c.tombstoned_at IS NULL
  AND t.status = 'active'
  AND t.tombstoned_at IS NULL
`

// GetActiveSAMLProviderConfigForLogin returns the active revision's
// ciphertext and non-secret configuration for a DB-backed SAML provider
// config that is currently ENABLED (status='active') and belongs to an
// active tenant. found=false for a missing, draft, disabled, tombstoned, or
// tenant-inactive provider config — login must never proceed against a
// provider that has not passed Enable's test-connection gate (see
// EnableProviderConfig), mirroring GetActiveProviderConfigForLogin's OIDC
// contract exactly.
//
// This method returns ciphertext, never plaintext — it never calls
// (*secretcrypto.Keyring).Open. The caller (cmd/api, on behalf of the SAML
// login runtime via samlDBProviderResolver) must hand the ciphertext to
// samlauth.ResolveSealedProviderConfig, which is the actual Open call site,
// confined to the samlauth login/authn package per the epic #4962 boundary.
func (s *IdentitySubjectStore) GetActiveSAMLProviderConfigForLogin(
	ctx context.Context,
	providerConfigID string,
) (ProviderConfigConnectionTestMaterial, bool, error) {
	if s.db == nil {
		return ProviderConfigConnectionTestMaterial{}, false, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	if providerConfigID == "" {
		return ProviderConfigConnectionTestMaterial{}, false, errors.New("provider_config_id is required")
	}
	rows, err := s.db.QueryContext(ctx, selectActiveSAMLProviderConfigForLoginQuery, providerConfigID)
	if err != nil {
		return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("select active saml provider config for login: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("select active saml provider config for login: %w", err)
		}
		return ProviderConfigConnectionTestMaterial{}, false, nil
	}
	material := ProviderConfigConnectionTestMaterial{ProviderConfigID: providerConfigID}
	var sealedSecret, configuration sql.NullString
	if err := rows.Scan(&material.ProviderKind, &material.RevisionID, &sealedSecret, &configuration); err != nil {
		return ProviderConfigConnectionTestMaterial{}, false, fmt.Errorf("scan active saml provider config for login: %w", err)
	}
	if sealedSecret.Valid {
		material.SealedSecret = sealedSecret.String
	}
	if configuration.Valid {
		material.Configuration = configuration.String
	}
	return material, true, rows.Err()
}
