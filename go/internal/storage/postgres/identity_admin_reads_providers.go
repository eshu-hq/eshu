// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AdminIdPProviderListItem is the metadata-only view of one configured identity
// provider. provider_key_hash, issuer_hash, metadata_url_hash, entity_id_hash,
// client_id_hash, and credential_handle are secrets and are never selected.
type AdminIdPProviderListItem struct {
	ProviderConfigID string
	ProviderKind     string
	Status           string
}

// AdminIdPGroupMappingListItem is the metadata-only view of one external
// group→role mapping. external_group_hash (a hashed group-name secret) and
// policy_revision_hash are never selected; MappingRef is a stable, non-secret
// reference an admin can use to address the mapping without the group hash.
type AdminIdPGroupMappingListItem struct {
	MappingRef       string
	ProviderConfigID string
	RoleID           string
	Status           string
	EffectiveAt      time.Time
	ExpiresAt        time.Time
	TenantID         string
	WorkspaceID      string
}

// AdminAPITokenListItem is the metadata-only view of one generated API token for
// the tenant-scoped admin list across all users. token_hash and
// display_handle_hash are never selected. DisplayLabel (issue #3708) is the
// real, non-secret operator-facing label.
type AdminAPITokenListItem struct {
	TokenID            string
	TokenClass         string
	UserID             string
	ServicePrincipalID string
	Status             string
	DisplayLabel       string
	IssuedAt           time.Time
	ExpiresAt          time.Time
	RevokedAt          time.Time
	TenantID           string
	WorkspaceID        string
}

// LoginProviderItem is the minimal pre-auth view of one configured identity
// provider used by the public /api/v0/auth/providers discovery endpoint.
// Only provider_config_id and provider_kind are selected — no hashed or private
// IdP fields (issuer_hash, metadata_url_hash, entity_id_hash, client_id_hash,
// credential_handle, tenant_id) are exposed.
type LoginProviderItem struct {
	ProviderConfigID string
	ProviderKind     string
}

// listActiveLoginProvidersQuery selects the active OIDC and SAML provider rows
// scoped to a single tenant for the pre-auth discovery endpoint. Scoping to one
// tenant prevents cross-tenant provider enumeration by anonymous callers. Only
// login-facing provider kinds are returned (external_oidc, external_saml). Rows
// with tombstoned_at IS NOT NULL or status != 'active' are excluded.
const listActiveLoginProvidersQuery = `
SELECT
    provider_config_id,
    provider_kind
FROM identity_provider_configs
WHERE tenant_id = $1
  AND provider_kind IN ('external_oidc', 'external_saml')
  AND status = 'active'
  AND tombstoned_at IS NULL
ORDER BY provider_kind ASC, provider_config_id ASC
LIMIT 200
`

// ListActiveLoginProviders returns the active OIDC and SAML providers for the
// specified tenant for the pre-auth provider-discovery endpoint. The tenant_id
// must be non-empty; callers that cannot resolve a tenant must not call this
// method and must return an empty list instead. No private or hashed IdP fields
// are returned. The caller (authProviderListStore) derives the display label
// from provider_kind and never echoes a domain or org name.
func (s *IdentitySubjectStore) ListActiveLoginProviders(
	ctx context.Context,
	tenantID string,
) ([]LoginProviderItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required for login provider listing")
	}
	rows, err := s.db.QueryContext(ctx, listActiveLoginProvidersQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list active login providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []LoginProviderItem
	for rows.Next() {
		var item LoginProviderItem
		if err := rows.Scan(&item.ProviderConfigID, &item.ProviderKind); err != nil {
			return nil, fmt.Errorf("scan active login provider item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active login providers: %w", err)
	}
	return items, nil
}

// listAdminIdPProvidersQuery selects metadata-only provider columns for the
// caller's tenant. No hashed issuer/metadata/entity/client identifiers and no
// credential handle are selected. tombstoned_at IS NULL excludes soft-deleted
// providers that may still carry status='active'.
const listAdminIdPProvidersQuery = `
SELECT
    provider_config_id,
    provider_kind,
    status
FROM identity_provider_configs
WHERE tenant_id = $1
  AND tombstoned_at IS NULL
ORDER BY provider_config_id ASC
LIMIT 500
`

// ListAdminIdPProviders returns metadata-only provider rows scoped strictly to
// the supplied tenant. It never returns issuer/metadata/entity/client hashes or
// credential handles.
func (s *IdentitySubjectStore) ListAdminIdPProviders(
	ctx context.Context,
	tenantID string,
) ([]AdminIdPProviderListItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	rows, err := s.db.QueryContext(ctx, listAdminIdPProvidersQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list admin idp providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminIdPProviderListItem
	for rows.Next() {
		var item AdminIdPProviderListItem
		if err := rows.Scan(
			&item.ProviderConfigID,
			&item.ProviderKind,
			&item.Status,
		); err != nil {
			return nil, fmt.Errorf("scan admin idp provider item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin idp providers: %w", err)
	}
	return items, nil
}

// listAdminIdPGroupMappingsQuery selects metadata-only mapping columns for the
// caller's tenant/workspace. external_group_hash is never selected; a stable
// md5 digest over the composite primary key forms a non-secret MappingRef so an
// admin can address a row without the hashed group name. md5 here is a
// non-cryptographic row identifier, not a secret. tombstoned_at IS NULL excludes
// soft-deleted mappings that may still carry status='active'.
const listAdminIdPGroupMappingsQuery = `
SELECT
    md5(provider_config_id || ':' || tenant_id || ':' || workspace_id || ':' || role_id || ':' || external_group_hash) AS mapping_ref,
    provider_config_id,
    role_id,
    status,
    effective_at,
    expires_at,
    tenant_id,
    workspace_id
FROM identity_provider_group_role_mappings
WHERE tenant_id = $1 AND workspace_id = $2
  AND tombstoned_at IS NULL
ORDER BY provider_config_id ASC, role_id ASC, mapping_ref ASC
LIMIT 500
`

// ListAdminIdPGroupMappings returns metadata-only group→role mapping rows scoped
// strictly to the supplied tenant and workspace. It never returns
// external_group_hash, the hashed external group-name secret.
func (s *IdentitySubjectStore) ListAdminIdPGroupMappings(
	ctx context.Context,
	tenantID string,
	workspaceID string,
) ([]AdminIdPGroupMappingListItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	rows, err := s.db.QueryContext(ctx, listAdminIdPGroupMappingsQuery, tenantID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list admin idp group mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminIdPGroupMappingListItem
	for rows.Next() {
		var item AdminIdPGroupMappingListItem
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.MappingRef,
			&item.ProviderConfigID,
			&item.RoleID,
			&item.Status,
			&item.EffectiveAt,
			&expiresAt,
			&item.TenantID,
			&item.WorkspaceID,
		); err != nil {
			return nil, fmt.Errorf("scan admin idp group mapping item: %w", err)
		}
		item.EffectiveAt = item.EffectiveAt.UTC()
		if expiresAt.Valid {
			item.ExpiresAt = expiresAt.Time.UTC()
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin idp group mappings: %w", err)
	}
	return items, nil
}

// listAdminAPITokensQuery selects metadata-only token columns for every user in
// the caller's tenant/workspace. token_hash and display_handle_hash are never
// selected; display_label (issue #3708) is the real, non-secret
// operator-facing label and is safe to return as-is.
const listAdminAPITokensQuery = `
SELECT
    token_id,
    token_class,
    user_id,
    service_principal_id,
    status,
    display_label,
    issued_at,
    expires_at,
    revoked_at,
    tenant_id,
    workspace_id
FROM identity_token_metadata
WHERE tenant_id = $1 AND workspace_id = $2
ORDER BY issued_at DESC, token_id ASC
LIMIT 500
`

// ListAdminAPITokens returns metadata-only generated-token rows for every user
// scoped strictly to the supplied tenant and workspace. It never returns
// token_hash or display_handle_hash. This is the admin counterpart to the
// self-scoped ListAPITokensBySubject.
func (s *IdentitySubjectStore) ListAdminAPITokens(
	ctx context.Context,
	tenantID string,
	workspaceID string,
) ([]AdminAPITokenListItem, error) {
	if s.db == nil {
		return nil, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	rows, err := s.db.QueryContext(ctx, listAdminAPITokensQuery, tenantID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list admin api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []AdminAPITokenListItem
	for rows.Next() {
		var item AdminAPITokenListItem
		var userID, servicePrincipalID, displayLabel sql.NullString
		var expiresAt, revokedAt sql.NullTime
		if err := rows.Scan(
			&item.TokenID,
			&item.TokenClass,
			&userID,
			&servicePrincipalID,
			&item.Status,
			&displayLabel,
			&item.IssuedAt,
			&expiresAt,
			&revokedAt,
			&item.TenantID,
			&item.WorkspaceID,
		); err != nil {
			return nil, fmt.Errorf("scan admin api token item: %w", err)
		}
		if userID.Valid {
			item.UserID = userID.String
		}
		if servicePrincipalID.Valid {
			item.ServicePrincipalID = servicePrincipalID.String
		}
		if displayLabel.Valid {
			item.DisplayLabel = displayLabel.String
		}
		item.IssuedAt = item.IssuedAt.UTC()
		if expiresAt.Valid {
			item.ExpiresAt = expiresAt.Time.UTC()
		}
		if revokedAt.Valid {
			item.RevokedAt = revokedAt.Time.UTC()
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admin api tokens: %w", err)
	}
	return items, nil
}
