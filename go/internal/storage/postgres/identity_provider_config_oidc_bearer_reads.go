// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// selectActiveOIDCBearerProvidersQuery lists every enabled (status='active',
// not tombstoned) external_oidc provider config across ALL tenants, for the
// IdP bearer-token resolver's provider source (issue #5162). Unlike the
// login-facing reads in identity_provider_config_login_reads.go, this is
// deliberately NOT tenant-scoped: the bearer resolver's verifier cache
// routes an inbound token by its unverified "iss" claim alone (it does not
// yet know which tenant the caller belongs to — that is exactly what this
// read, plus the token's claims, establishes), so it must see every active
// OIDC provider across every tenant to build its issuer routing table.
//
// sealed_secret is deliberately never selected: JWT bearer-token validation
// verifies a signature against the issuer's own published JWKS, never
// against Eshu's stored OAuth2 client secret.
// #nosec G101 -- SQL query constant; its column list selects no secret (sealed_secret is deliberately excluded), so this is query text, not a hardcoded credential. LOW-confidence gosec heuristic triggered by the adjacent doc-comment word.
const selectActiveOIDCBearerProvidersQuery = `
SELECT
    c.provider_config_id,
    c.tenant_id,
    c.active_revision_id,
    r.configuration
FROM identity_provider_configs c
JOIN identity_provider_config_revisions r
    ON r.provider_config_id = c.provider_config_id AND r.revision_id = c.active_revision_id
WHERE c.status = 'active' AND c.tombstoned_at IS NULL AND c.provider_kind = 'external_oidc'
ORDER BY c.provider_config_id ASC
LIMIT 2000
`

// oidcBearerProviderConfiguration mirrors the non-secret subset of
// dbProviderConfiguration (internal/oidclogin/db_provider_config.go) this
// read actually needs: issuer and group_claim. Both packages independently
// decode the same identity_provider_config_revisions.configuration JSON
// column; keeping this package's decode shape narrow (no client_id, scopes,
// or redirect_url — none of which bearer validation uses) avoids this
// storage package needing to track oidclogin's full shape.
type oidcBearerProviderConfiguration struct {
	Issuer     string `json:"issuer"`
	GroupClaim string `json:"group_claim"`
}

// OIDCBearerProviderRow is one enabled DB-backed OIDC provider's bearer
// -validation-relevant fields: no sealed_secret, no client_id, no redirect
// URL — only what a JWT signature/issuer/claims check and grant-resolution
// scoping need. WorkspaceID is deliberately absent: identity_provider_configs
// has no workspace column (it is tenant-scoped only), so a caller mapping
// this row into an oidcbearer.BearerProvider must resolve a concrete
// WorkspaceID itself, exactly like cmd/api's oidcDBProviderResolver does for
// the interactive login path (see that type's resolveWorkspace).
type OIDCBearerProviderRow struct {
	ProviderConfigID string
	TenantID         string
	RevisionID       string
	Issuer           string
	GroupClaim       string
}

// ListActiveOIDCBearerProviders returns every enabled external_oidc provider
// config across all tenants, for the bearer-token resolver's provider
// source. A row whose configuration JSON fails to decode or carries a blank
// issuer is skipped (logged by the caller, not here — this package has no
// logger dependency) rather than failing the whole list: one misconfigured
// provider config must not take every other enabled provider's bearer
// validation down with it.
func (s *IdentitySubjectStore) ListActiveOIDCBearerProviders(ctx context.Context) ([]OIDCBearerProviderRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("identity subject store database is required")
	}
	rows, err := s.db.QueryContext(ctx, selectActiveOIDCBearerProvidersQuery)
	if err != nil {
		return nil, fmt.Errorf("list active oidc bearer providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []OIDCBearerProviderRow
	for rows.Next() {
		var providerConfigID, tenantID string
		var activeRevisionID, configuration sql.NullString
		if err := rows.Scan(&providerConfigID, &tenantID, &activeRevisionID, &configuration); err != nil {
			return nil, fmt.Errorf("scan active oidc bearer provider: %w", err)
		}
		if !activeRevisionID.Valid || !configuration.Valid {
			continue
		}
		var cfg oidcBearerProviderConfiguration
		if err := json.Unmarshal([]byte(configuration.String), &cfg); err != nil {
			continue
		}
		if cfg.Issuer == "" {
			continue
		}
		items = append(items, OIDCBearerProviderRow{
			ProviderConfigID: providerConfigID,
			TenantID:         tenantID,
			RevisionID:       activeRevisionID.String,
			Issuer:           cfg.Issuer,
			GroupClaim:       cfg.GroupClaim,
		})
	}
	return items, rows.Err()
}
