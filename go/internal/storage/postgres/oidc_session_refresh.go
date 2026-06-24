package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// OIDCRoleGrantQuery re-resolves whether a session's already granted Eshu roles
// still map to concrete grants under current, active, untombstoned, unexpired
// policy. It is the active-session refresh counterpart to the login-time
// group-hash resolution and never accepts raw provider tokens or group names.
type OIDCRoleGrantQuery struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	RoleIDs          []string
	AsOf             time.Time
	Limit            int
}

// ResolveActiveRoleGrants validates the supplied role ids against active,
// untombstoned, unexpired role rows for the tenant/workspace and re-resolves
// their concrete scope and repository grants. It returns ok=false when no
// supplied role still resolves, which the refresher treats as a revocation
// signal for tombstoned mappings, expired mappings, and revoked role targets.
func (s *OIDCLoginStore) ResolveActiveRoleGrants(
	ctx context.Context,
	query OIDCRoleGrantQuery,
) (OIDCGroupGrantResolution, bool, error) {
	if s.db == nil {
		return OIDCGroupGrantResolution{}, false, errors.New("oidc login store database is required")
	}
	query = normalizeOIDCRoleGrantQuery(query)
	if err := validateOIDCRoleGrantQuery(query); err != nil {
		return OIDCGroupGrantResolution{}, false, err
	}
	roles, policyRevisionHash, err := s.resolveActiveRoles(ctx, query)
	if err != nil || len(roles) == 0 {
		return OIDCGroupGrantResolution{}, false, err
	}
	grantQuery := OIDCGroupGrantQuery{
		ProviderConfigID: query.ProviderConfigID,
		TenantID:         query.TenantID,
		WorkspaceID:      query.WorkspaceID,
		AsOf:             query.AsOf,
		Limit:            query.Limit,
	}
	scopes, err := s.resolveOIDCScopes(ctx, grantQuery, roles)
	if err != nil {
		return OIDCGroupGrantResolution{}, false, err
	}
	repos, err := s.resolveOIDCRepositories(ctx, grantQuery, roles)
	if err != nil {
		return OIDCGroupGrantResolution{}, false, err
	}
	features, dataClasses, err := resolvePermissionGrantsForRoles(ctx, s.db, query.TenantID, roles, query.AsOf)
	if err != nil {
		// Fails closed (refresh denied -> session revoked). Log for operator triage.
		slog.ErrorContext(ctx, "oidc session permission grant resolution failed during refresh; session denied",
			"subject_class", "external_oidc_user", "tenant_id", query.TenantID, "role_count", len(roles), "error", err)
		return OIDCGroupGrantResolution{}, false, err
	}
	return OIDCGroupGrantResolution{
		RoleIDs:                      roles,
		PolicyRevisionHash:           policyRevisionHash,
		AllowedScopeIDs:              scopes,
		AllowedRepositoryIDs:         repos,
		AllowedPermissionFeatures:    features,
		AllowedPermissionDataClasses: dataClasses,
	}, true, nil
}

func (s *OIDCLoginStore) resolveActiveRoles(
	ctx context.Context,
	query OIDCRoleGrantQuery,
) ([]string, string, error) {
	rows, err := s.db.QueryContext(
		ctx,
		resolveOIDCActiveRolesQuery,
		query.TenantID,
		query.WorkspaceID,
		query.ProviderConfigID,
		query.AsOf,
		query.RoleIDs,
		query.Limit,
	)
	if err != nil {
		return nil, "", fmt.Errorf("resolve oidc active roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	roles := make([]string, 0)
	policyRevisionHashes := make(map[string]struct{})
	for rows.Next() {
		var roleID string
		var rowPolicyRevisionHash string
		if err := rows.Scan(&roleID, &rowPolicyRevisionHash); err != nil {
			return nil, "", fmt.Errorf("resolve oidc active roles: %w", err)
		}
		roles = append(roles, roleID)
		if rowPolicyRevisionHash = strings.TrimSpace(rowPolicyRevisionHash); rowPolicyRevisionHash != "" {
			policyRevisionHashes[rowPolicyRevisionHash] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("resolve oidc active roles: %w", err)
	}
	roles = cleanBrowserSessionStrings(roles)
	if len(roles) == 0 {
		return nil, "", nil
	}
	policyRevisionHash, err := singleOIDCPolicyRevisionHash(policyRevisionHashes)
	if err != nil {
		return nil, "", err
	}
	return roles, strings.TrimSpace(policyRevisionHash), nil
}

// ExternalSubjectActive reports whether the hashed external subject is still an
// active, non-disabled, non-tombstoned identity linked to an active user. It
// fails closed: a disabled subject, disabled user, or unknown link returns
// false so the refresher denies subsequent access.
func (s *OIDCLoginStore) ExternalSubjectActive(
	ctx context.Context,
	providerConfigID string,
	subjectIDHash string,
) (bool, error) {
	if s.db == nil {
		return false, errors.New("oidc login store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	subjectIDHash = strings.TrimSpace(subjectIDHash)
	if providerConfigID == "" || subjectIDHash == "" {
		return false, errors.New("oidc subject lookup requires provider config id and subject hash")
	}
	rows, err := s.db.QueryContext(ctx, externalSubjectActiveQuery, providerConfigID, subjectIDHash)
	if err != nil {
		return false, fmt.Errorf("resolve oidc external subject active: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("resolve oidc external subject active: %w", err)
		}
		return false, nil
	}
	var active bool
	if err := rows.Scan(&active); err != nil {
		return false, fmt.Errorf("resolve oidc external subject active: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("resolve oidc external subject active: %w", err)
	}
	return active, nil
}

func normalizeOIDCRoleGrantQuery(query OIDCRoleGrantQuery) OIDCRoleGrantQuery {
	query.ProviderConfigID = strings.TrimSpace(query.ProviderConfigID)
	query.TenantID = strings.TrimSpace(query.TenantID)
	query.WorkspaceID = strings.TrimSpace(query.WorkspaceID)
	query.RoleIDs = cleanBrowserSessionStrings(query.RoleIDs)
	query.AsOf = query.AsOf.UTC()
	if query.Limit == 0 {
		query.Limit = defaultOIDCGrantLimit
	}
	if query.Limit > maxOIDCGrantLimit {
		query.Limit = maxOIDCGrantLimit
	}
	return query
}

func validateOIDCRoleGrantQuery(query OIDCRoleGrantQuery) error {
	if blank(query.ProviderConfigID) || blank(query.TenantID) || blank(query.WorkspaceID) {
		return errors.New("oidc provider, tenant, and workspace are required")
	}
	if len(query.RoleIDs) == 0 {
		return errors.New("oidc role grant refresh requires at least one role id")
	}
	if query.AsOf.IsZero() || query.Limit <= 0 {
		return errors.New("oidc role grant query as_of and positive limit are required")
	}
	return nil
}
