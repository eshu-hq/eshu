package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultOIDCGrantLimit = 100
	maxOIDCGrantLimit     = 1000
)

// OIDCLoginStore persists hash-only OIDC login state and resolves IdP group
// hashes through Eshu-owned role target grants.
type OIDCLoginStore struct {
	db ExecQueryer
}

// OIDCLoginStateRecord is one server-side Authorization Code state row.
type OIDCLoginStateRecord struct {
	StateHash        string
	NonceHash        string
	ProviderConfigID string
	ProviderKeyHash  string
	IssuerHash       string
	ClientIDHash     string
	TenantID         string
	WorkspaceID      string
	RedirectURIHash  string
	ReturnToPath     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	UpdatedAt        time.Time
}

// OIDCGroupGrantQuery resolves hashed external groups into Eshu role grants.
type OIDCGroupGrantQuery struct {
	ProviderConfigID    string
	TenantID            string
	WorkspaceID         string
	ExternalGroupHashes []string
	AsOf                time.Time
	Limit               int
}

// OIDCGroupGrantResolution is the concrete authorization snapshot for a login.
type OIDCGroupGrantResolution struct {
	RoleIDs              []string
	PolicyRevisionHash   string
	AllowedScopeIDs      []string
	AllowedRepositoryIDs []string
}

// NewOIDCLoginStore constructs a Postgres OIDC login store.
func NewOIDCLoginStore(db ExecQueryer) *OIDCLoginStore {
	return &OIDCLoginStore{db: db}
}

// OIDCLoginSchemaSQL returns the OIDC login state and mapping DDL.
func OIDCLoginSchemaSQL() string {
	return oidcLoginSchemaSQL
}

func oidcLoginBootstrapDefinition() Definition {
	return Definition{
		Name: "identity_oidc_login",
		Path: "schema/data-plane/postgres/006g_identity_oidc_login.sql",
		SQL:  oidcLoginSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, oidcLoginBootstrapDefinition())
}

// EnsureSchema applies the OIDC login schema.
func (s *OIDCLoginStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("oidc login store database is required")
	}
	if _, err := s.db.ExecContext(ctx, oidcLoginSchemaSQL); err != nil {
		return fmt.Errorf("ensure oidc login schema: %w", err)
	}
	return nil
}

// CreateState writes one hash-only OIDC login state row.
func (s *OIDCLoginStore) CreateState(ctx context.Context, record OIDCLoginStateRecord) error {
	if s.db == nil {
		return errors.New("oidc login store database is required")
	}
	record = normalizeOIDCLoginState(record)
	if err := validateOIDCLoginState(record); err != nil {
		return err
	}
	result, err := s.db.ExecContext(
		ctx,
		createOIDCLoginStateQuery,
		record.StateHash,
		record.NonceHash,
		record.ProviderConfigID,
		record.ProviderKeyHash,
		record.IssuerHash,
		record.ClientIDHash,
		record.TenantID,
		record.WorkspaceID,
		record.RedirectURIHash,
		record.ReturnToPath,
		record.IssuedAt,
		record.ExpiresAt,
		record.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create oidc login state: %w", err)
	}
	if result != nil {
		affected, err := result.RowsAffected()
		if err == nil && affected == 0 {
			return errors.New("active oidc provider config is required to create login state")
		}
	}
	return nil
}

// ConsumeState atomically marks an unexpired state row consumed and returns it.
func (s *OIDCLoginStore) ConsumeState(
	ctx context.Context,
	stateHash string,
	consumedAt time.Time,
) (OIDCLoginStateRecord, bool, error) {
	if s.db == nil {
		return OIDCLoginStateRecord{}, false, errors.New("oidc login store database is required")
	}
	stateHash = strings.TrimSpace(stateHash)
	consumedAt = consumedAt.UTC()
	if stateHash == "" || consumedAt.IsZero() {
		return OIDCLoginStateRecord{}, false, errors.New("oidc state hash and consumed_at are required")
	}
	rows, err := s.db.QueryContext(ctx, consumeOIDCLoginStateQuery, stateHash, consumedAt)
	if err != nil {
		return OIDCLoginStateRecord{}, false, fmt.Errorf("consume oidc login state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return OIDCLoginStateRecord{}, false, fmt.Errorf("consume oidc login state: %w", err)
		}
		return OIDCLoginStateRecord{}, false, nil
	}
	record, err := scanOIDCLoginState(rows)
	if err != nil {
		return OIDCLoginStateRecord{}, false, fmt.Errorf("consume oidc login state: %w", err)
	}
	if err := rows.Err(); err != nil {
		return OIDCLoginStateRecord{}, false, fmt.Errorf("consume oidc login state: %w", err)
	}
	return record, true, nil
}

// ResolveGroupRoleGrants expands hashed external group claims through active
// Eshu role mappings and returns concrete session grants.
func (s *OIDCLoginStore) ResolveGroupRoleGrants(
	ctx context.Context,
	query OIDCGroupGrantQuery,
) (OIDCGroupGrantResolution, bool, error) {
	if s.db == nil {
		return OIDCGroupGrantResolution{}, false, errors.New("oidc login store database is required")
	}
	query = normalizeOIDCGroupGrantQuery(query)
	if err := validateOIDCGroupGrantQuery(query); err != nil {
		return OIDCGroupGrantResolution{}, false, err
	}
	roles, policyRevisionHash, err := s.resolveOIDCRoles(ctx, query)
	if err != nil || len(roles) == 0 {
		return OIDCGroupGrantResolution{}, false, err
	}
	scopes, err := s.resolveOIDCScopes(ctx, query, roles)
	if err != nil {
		return OIDCGroupGrantResolution{}, false, err
	}
	repos, err := s.resolveOIDCRepositories(ctx, query, roles)
	if err != nil {
		return OIDCGroupGrantResolution{}, false, err
	}
	return OIDCGroupGrantResolution{
		RoleIDs:              roles,
		PolicyRevisionHash:   policyRevisionHash,
		AllowedScopeIDs:      scopes,
		AllowedRepositoryIDs: repos,
	}, true, nil
}

func (s *OIDCLoginStore) resolveOIDCRoles(
	ctx context.Context,
	query OIDCGroupGrantQuery,
) ([]string, string, error) {
	rows, err := s.db.QueryContext(
		ctx,
		resolveOIDCGroupRolesQuery,
		query.TenantID,
		query.WorkspaceID,
		query.ProviderConfigID,
		query.AsOf,
		query.ExternalGroupHashes,
		query.Limit,
	)
	if err != nil {
		return nil, "", fmt.Errorf("resolve oidc group roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	roles := make([]string, 0)
	policyRevisionHashes := make(map[string]struct{})
	for rows.Next() {
		var roleID string
		var rowPolicyRevisionHash string
		if err := rows.Scan(&roleID, &rowPolicyRevisionHash); err != nil {
			return nil, "", fmt.Errorf("resolve oidc group roles: %w", err)
		}
		roles = append(roles, roleID)
		if rowPolicyRevisionHash = strings.TrimSpace(rowPolicyRevisionHash); rowPolicyRevisionHash != "" {
			policyRevisionHashes[rowPolicyRevisionHash] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("resolve oidc group roles: %w", err)
	}
	if len(roles) == 0 {
		return nil, "", nil
	}
	policyRevisionHash, err := singleOIDCPolicyRevisionHash(policyRevisionHashes)
	if err != nil {
		return nil, "", err
	}
	return cleanBrowserSessionStrings(roles), strings.TrimSpace(policyRevisionHash), nil
}

func (s *OIDCLoginStore) resolveOIDCScopes(
	ctx context.Context,
	query OIDCGroupGrantQuery,
	roles []string,
) ([]string, error) {
	rows, err := s.db.QueryContext(
		ctx,
		resolveOIDCRoleScopeTargetsQuery,
		query.TenantID,
		query.WorkspaceID,
		roles,
		query.AsOf,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve oidc role scope targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOIDCStringColumn(rows, "resolve oidc role scope targets")
}

func (s *OIDCLoginStore) resolveOIDCRepositories(
	ctx context.Context,
	query OIDCGroupGrantQuery,
	roles []string,
) ([]string, error) {
	rows, err := s.db.QueryContext(
		ctx,
		resolveOIDCRoleRepositoryTargetsQuery,
		query.TenantID,
		query.WorkspaceID,
		roles,
		query.AsOf,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve oidc role repository targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	repositories := make([]string, 0)
	for rows.Next() {
		var repoID string
		var scopeID string
		if err := rows.Scan(&repoID, &scopeID); err != nil {
			return nil, fmt.Errorf("resolve oidc role repository targets: %w", err)
		}
		repositories = append(repositories, repoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve oidc role repository targets: %w", err)
	}
	return cleanBrowserSessionStrings(repositories), nil
}

func normalizeOIDCLoginState(record OIDCLoginStateRecord) OIDCLoginStateRecord {
	record.StateHash = strings.TrimSpace(record.StateHash)
	record.NonceHash = strings.TrimSpace(record.NonceHash)
	record.ProviderConfigID = strings.TrimSpace(record.ProviderConfigID)
	record.ProviderKeyHash = strings.TrimSpace(record.ProviderKeyHash)
	record.IssuerHash = strings.TrimSpace(record.IssuerHash)
	record.ClientIDHash = strings.TrimSpace(record.ClientIDHash)
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.RedirectURIHash = strings.TrimSpace(record.RedirectURIHash)
	record.ReturnToPath = strings.TrimSpace(record.ReturnToPath)
	record.IssuedAt = record.IssuedAt.UTC()
	record.ExpiresAt = record.ExpiresAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func validateOIDCLoginState(record OIDCLoginStateRecord) error {
	if blank(record.StateHash) || blank(record.NonceHash) || blank(record.ProviderConfigID) ||
		blank(record.ProviderKeyHash) || blank(record.IssuerHash) || blank(record.ClientIDHash) ||
		blank(record.TenantID) || blank(record.WorkspaceID) || blank(record.RedirectURIHash) {
		return errors.New("oidc state hash, nonce hash, provider hashes, tenant, workspace, and redirect uri hash are required")
	}
	if record.IssuedAt.IsZero() || record.ExpiresAt.IsZero() || record.UpdatedAt.IsZero() {
		return errors.New("oidc state issued, expiry, and updated timestamps are required")
	}
	if !record.ExpiresAt.After(record.IssuedAt) {
		return errors.New("oidc state expires_at must be after issued_at")
	}
	return nil
}

func normalizeOIDCGroupGrantQuery(query OIDCGroupGrantQuery) OIDCGroupGrantQuery {
	query.ProviderConfigID = strings.TrimSpace(query.ProviderConfigID)
	query.TenantID = strings.TrimSpace(query.TenantID)
	query.WorkspaceID = strings.TrimSpace(query.WorkspaceID)
	query.ExternalGroupHashes = cleanBrowserSessionStrings(query.ExternalGroupHashes)
	query.AsOf = query.AsOf.UTC()
	if query.Limit == 0 {
		query.Limit = defaultOIDCGrantLimit
	}
	if query.Limit > maxOIDCGrantLimit {
		query.Limit = maxOIDCGrantLimit
	}
	return query
}

func validateOIDCGroupGrantQuery(query OIDCGroupGrantQuery) error {
	if blank(query.ProviderConfigID) || blank(query.TenantID) || blank(query.WorkspaceID) {
		return errors.New("oidc provider, tenant, and workspace are required")
	}
	if len(query.ExternalGroupHashes) == 0 {
		return errors.New("oidc external group hashes are required")
	}
	if query.AsOf.IsZero() || query.Limit <= 0 {
		return errors.New("oidc grant query as_of and positive limit are required")
	}
	return nil
}

func scanOIDCLoginState(rows Rows) (OIDCLoginStateRecord, error) {
	var record OIDCLoginStateRecord
	if err := rows.Scan(
		&record.StateHash,
		&record.NonceHash,
		&record.ProviderConfigID,
		&record.TenantID,
		&record.WorkspaceID,
		&record.RedirectURIHash,
		&record.ReturnToPath,
		&record.IssuedAt,
		&record.ExpiresAt,
		&record.UpdatedAt,
	); err != nil {
		return OIDCLoginStateRecord{}, err
	}
	return normalizeOIDCLoginState(record), nil
}

func scanOIDCStringColumn(rows Rows, label string) ([]string, error) {
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return cleanBrowserSessionStrings(values), nil
}

func singleOIDCPolicyRevisionHash(values map[string]struct{}) (string, error) {
	if len(values) != 1 {
		return "", errors.New("oidc group roles must resolve to exactly one policy revision")
	}
	for value := range values {
		return value, nil
	}
	return "", errors.New("oidc group roles must resolve to exactly one policy revision")
}
