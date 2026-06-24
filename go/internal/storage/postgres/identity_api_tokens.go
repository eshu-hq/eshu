package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	identityAPITokenClassPersonal         = "personal"
	identityAPITokenClassServicePrincipal = "service_principal"
)

// IdentityAPITokenResolution is the authorization snapshot for a generated
// personal or service-principal API token.
type IdentityAPITokenResolution struct {
	TokenHash                    string
	TokenClass                   string
	TenantID                     string
	WorkspaceID                  string
	SubjectClass                 string
	SubjectIDHash                string
	PolicyRevisionHash           string
	RoleIDs                      []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
}

type identityAPITokenSubject struct {
	tokenHash          string
	tokenClass         string
	tenantID           string
	workspaceID        string
	userSubjectIDHash  string
	servicePrincipalID string
	policyRevisionHash string
}

// ResolveIdentityAPITokenHash resolves a generated personal or
// service-principal token hash through active identity subjects, roles, and
// repository/scope targets. Raw bearer token values must be hashed before this
// method is called.
func (s *ScopedAPITokenStore) ResolveIdentityAPITokenHash(
	ctx context.Context,
	tokenHash string,
	asOf time.Time,
) (IdentityAPITokenResolution, bool, error) {
	if s.db == nil {
		return IdentityAPITokenResolution{}, false, errors.New("scoped API token store database is required")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return IdentityAPITokenResolution{}, false, errors.New("token hash is required")
	}
	if asOf.IsZero() {
		return IdentityAPITokenResolution{}, false, errors.New("token lookup as_of is required")
	}
	asOf = asOf.UTC()

	subject, ok, err := s.resolveIdentityAPITokenSubject(ctx, tokenHash, asOf)
	if err != nil || !ok {
		return IdentityAPITokenResolution{}, ok, err
	}
	roles, policyRevisionHash, err := s.resolveIdentityAPITokenRoles(ctx, subject, asOf)
	if err != nil {
		return IdentityAPITokenResolution{}, false, err
	}
	if len(roles) == 0 {
		return IdentityAPITokenResolution{}, false, nil
	}
	features, dataClasses, err := s.resolveIdentityAPITokenPermissions(ctx, subject, roles, asOf)
	if err != nil {
		return IdentityAPITokenResolution{}, false, err
	}
	scopes, repos, err := s.resolveIdentityAPITokenTargets(ctx, subject, roles, asOf)
	if err != nil {
		return IdentityAPITokenResolution{}, false, err
	}
	if policyRevisionHash == "" {
		policyRevisionHash = subject.policyRevisionHash
	}
	return IdentityAPITokenResolution{
		TokenHash:                    subject.tokenHash,
		TokenClass:                   subject.tokenClass,
		TenantID:                     subject.tenantID,
		WorkspaceID:                  subject.workspaceID,
		SubjectClass:                 identityAPITokenSubjectClass(subject.tokenClass),
		SubjectIDHash:                identityAPITokenSubjectHash(subject),
		PolicyRevisionHash:           policyRevisionHash,
		RoleIDs:                      roles,
		AllowedPermissionFeatures:    features,
		AllowedPermissionDataClasses: dataClasses,
		AllowedScopeIDs:              scopes,
		AllowedRepositoryIDs:         repos,
	}, true, nil
}

// MarkIdentityAPITokenUsed records the last successful generated API-token
// authentication timestamp.
func (s *ScopedAPITokenStore) MarkIdentityAPITokenUsed(ctx context.Context, tokenHash string, usedAt time.Time) error {
	if s.db == nil {
		return errors.New("scoped API token store database is required")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return errors.New("token hash is required")
	}
	if usedAt.IsZero() {
		return errors.New("token used_at is required")
	}
	if _, err := s.db.ExecContext(ctx, markIdentityAPITokenUsedQuery, tokenHash, usedAt.UTC()); err != nil {
		return fmt.Errorf("mark identity API token used: %w", err)
	}
	return nil
}

func (s *ScopedAPITokenStore) resolveIdentityAPITokenSubject(
	ctx context.Context,
	tokenHash string,
	asOf time.Time,
) (identityAPITokenSubject, bool, error) {
	rows, err := s.db.QueryContext(ctx, resolveIdentityAPITokenSubjectQuery, tokenHash, asOf)
	if err != nil {
		return identityAPITokenSubject{}, false, fmt.Errorf("resolve identity API token subject: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return identityAPITokenSubject{}, false, fmt.Errorf("resolve identity API token subject: %w", err)
		}
		return identityAPITokenSubject{}, false, nil
	}
	var subject identityAPITokenSubject
	if err := rows.Scan(
		&subject.tokenHash,
		&subject.tokenClass,
		&subject.tenantID,
		&subject.workspaceID,
		&subject.userSubjectIDHash,
		&subject.servicePrincipalID,
		&subject.policyRevisionHash,
	); err != nil {
		return identityAPITokenSubject{}, false, fmt.Errorf("resolve identity API token subject: %w", err)
	}
	if err := rows.Err(); err != nil {
		return identityAPITokenSubject{}, false, fmt.Errorf("resolve identity API token subject: %w", err)
	}
	return subject, true, nil
}

func (s *ScopedAPITokenStore) resolveIdentityAPITokenRoles(
	ctx context.Context,
	subject identityAPITokenSubject,
	asOf time.Time,
) ([]string, string, error) {
	query := resolveIdentityPersonalAPITokenRolesQuery
	if subject.tokenClass == identityAPITokenClassServicePrincipal {
		query = resolveIdentityServicePrincipalAPITokenRolesQuery
	}
	rows, err := s.db.QueryContext(ctx, query, subject.tokenHash, asOf, maxOIDCGrantLimit)
	if err != nil {
		return nil, "", fmt.Errorf("resolve identity API token roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	roles := make([]string, 0)
	policyRevisionHashes := make(map[string]struct{})
	for rows.Next() {
		var roleID string
		var policyRevisionHash string
		if err := rows.Scan(&roleID, &policyRevisionHash); err != nil {
			return nil, "", fmt.Errorf("resolve identity API token roles: %w", err)
		}
		roles = append(roles, roleID)
		if policyRevisionHash = strings.TrimSpace(policyRevisionHash); policyRevisionHash != "" {
			policyRevisionHashes[policyRevisionHash] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("resolve identity API token roles: %w", err)
	}
	policyRevisionHash, err := singleIdentityPolicyRevisionHash(policyRevisionHashes)
	if err != nil {
		return nil, "", err
	}
	return cleanBrowserSessionStrings(roles), policyRevisionHash, nil
}

func (s *ScopedAPITokenStore) resolveIdentityAPITokenTargets(
	ctx context.Context,
	subject identityAPITokenSubject,
	roles []string,
	asOf time.Time,
) ([]string, []string, error) {
	if len(roles) == 0 {
		return nil, nil, nil
	}
	scopeRows, err := s.db.QueryContext(
		ctx,
		resolveOIDCRoleScopeTargetsQuery,
		subject.tenantID,
		subject.workspaceID,
		roles,
		asOf,
		maxOIDCGrantLimit,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve identity API token scope targets: %w", err)
	}
	scopes, err := scanOIDCStringColumn(scopeRows, "resolve identity API token scope targets")
	if err != nil {
		return nil, nil, err
	}

	repoRows, err := s.db.QueryContext(
		ctx,
		resolveOIDCRoleRepositoryTargetsQuery,
		subject.tenantID,
		subject.workspaceID,
		roles,
		asOf,
		maxOIDCGrantLimit,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve identity API token repository targets: %w", err)
	}
	repos, err := scanIdentityAPIRepositoryTargets(repoRows)
	if err != nil {
		return nil, nil, err
	}
	return scopes, repos, nil
}

func (s *ScopedAPITokenStore) resolveIdentityAPITokenPermissions(
	ctx context.Context,
	subject identityAPITokenSubject,
	roles []string,
	asOf time.Time,
) ([]string, []string, error) {
	return s.ResolvePermissionGrantsForRoles(ctx, subject.tenantID, roles, asOf)
}

// ResolvePermissionGrantsForRoles resolves the distinct permission features and
// data classes granted to a set of role IDs within one tenant as of the given
// time, using the same active-grant filtering as scoped-token resolution. It is
// the single source of truth for deriving a caller's permission grants from
// roles, so scoped tokens and browser sessions authorize identically.
//
// An empty role set returns empty grants. Unknown or inactive roles contribute
// nothing because the underlying query joins active, non-tombstoned grants and
// roles only. Results are deduplicated and trimmed.
func (s *ScopedAPITokenStore) ResolvePermissionGrantsForRoles(
	ctx context.Context,
	tenantID string,
	roles []string,
	asOf time.Time,
) ([]string, []string, error) {
	if s.db == nil {
		return nil, nil, errors.New("scoped API token store database is required")
	}
	return resolvePermissionGrantsForRoles(ctx, s.db, tenantID, roles, asOf)
}

// resolvePermissionGrantsForRoles is the package-level source of truth for
// deriving the distinct permission features and data classes granted to a role
// set within one tenant as of a point in time. Both scoped-token resolution and
// browser-session issuance call it so the two authorize identically. An empty
// role set or blank tenant returns empty grants without querying. Results are
// deduplicated and trimmed.
func resolvePermissionGrantsForRoles(
	ctx context.Context,
	db Queryer,
	tenantID string,
	roles []string,
	asOf time.Time,
) ([]string, []string, error) {
	if db == nil {
		return nil, nil, errors.New("permission grant resolution database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	roles = cleanBrowserSessionStrings(roles)
	if tenantID == "" || len(roles) == 0 {
		return nil, nil, nil
	}
	if asOf.IsZero() {
		return nil, nil, errors.New("permission grant resolution as_of is required")
	}
	rows, err := db.QueryContext(
		ctx,
		resolveIdentityAPITokenPermissionsQuery,
		tenantID,
		roles,
		asOf.UTC(),
		maxOIDCGrantLimit,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve identity API token permissions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	features := make([]string, 0)
	dataClasses := make([]string, 0)
	for rows.Next() {
		var feature string
		var dataClass string
		if err := rows.Scan(&feature, &dataClass); err != nil {
			return nil, nil, fmt.Errorf("resolve identity API token permissions: %w", err)
		}
		features = append(features, feature)
		dataClasses = append(dataClasses, dataClass)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("resolve identity API token permissions: %w", err)
	}
	return cleanBrowserSessionStrings(features), cleanBrowserSessionStrings(dataClasses), nil
}

func scanIdentityAPIRepositoryTargets(rows Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	repositories := make([]string, 0)
	for rows.Next() {
		var repoID string
		var scopeID string
		if err := rows.Scan(&repoID, &scopeID); err != nil {
			return nil, fmt.Errorf("resolve identity API token repository targets: %w", err)
		}
		repositories = append(repositories, repoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve identity API token repository targets: %w", err)
	}
	return cleanBrowserSessionStrings(repositories), nil
}

func identityAPITokenSubjectClass(tokenClass string) string {
	if tokenClass == identityAPITokenClassServicePrincipal {
		return "service_principal"
	}
	return "user"
}

func identityAPITokenSubjectHash(subject identityAPITokenSubject) string {
	if subject.tokenClass == identityAPITokenClassServicePrincipal {
		sum := sha256.Sum256([]byte(strings.TrimSpace(subject.servicePrincipalID)))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return strings.TrimSpace(subject.userSubjectIDHash)
}

func singleIdentityPolicyRevisionHash(values map[string]struct{}) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 {
		return "", errors.New("identity API token roles must resolve to at most one policy revision")
	}
	for value := range values {
		return value, nil
	}
	return "", nil
}
