// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBrowserSessionCSRFInvalid identifies an active browser session whose CSRF
// proof did not match the server-side hash.
var ErrBrowserSessionCSRFInvalid = errors.New("browser session csrf token invalid")

// ErrBrowserSessionRefreshRequired identifies an OIDC-backed browser session
// revoked because its external provider proof exceeded the refresh window.
var ErrBrowserSessionRefreshRequired = errors.New("browser session refresh required")

// BrowserSessionStore persists hash-only browser session rows.
type BrowserSessionStore struct {
	db ExecQueryer
}

// BrowserSessionRecord is the durable server-managed dashboard session state.
type BrowserSessionRecord struct {
	SessionHash                  string
	CSRFTokenHash                string
	TenantID                     string
	WorkspaceID                  string
	SubjectIDHash                string
	SubjectClass                 string
	PolicyRevisionHash           string
	RoleIDs                      []string
	AllScopes                    bool
	PermissionCatalogEnforced    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	ExternalProviderConfigID     string
	ExternalSubjectIDHash        string
	ExternalGroupHashes          []string
	ExternalAuthValidatedAt      time.Time
	ExternalAuthStaleAfter       time.Time
	IssuedAt                     time.Time
	LastSeenAt                   time.Time
	IdleExpiresAt                time.Time
	AbsoluteExpiresAt            time.Time
	RevokedAt                    time.Time
	UpdatedAt                    time.Time
}

// NewBrowserSessionStore constructs a Postgres-backed browser session store.
func NewBrowserSessionStore(db ExecQueryer) *BrowserSessionStore {
	return &BrowserSessionStore{db: db}
}

// BrowserSessionSchemaSQL returns the browser session registry DDL.
func BrowserSessionSchemaSQL() string {
	return browserSessionSchemaSQL
}

func browserSessionBootstrapDefinition() Definition {
	return Definition{
		Name: "browser_sessions",
		Path: "schema/data-plane/postgres/006f_browser_sessions.sql",
		SQL:  browserSessionSchemaSQL,
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, browserSessionBootstrapDefinition())
}

// EnsureSchema applies the browser session registry schema.
func (s *BrowserSessionStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("browser session store database is required")
	}
	if _, err := s.db.ExecContext(ctx, browserSessionSchemaSQL); err != nil {
		return fmt.Errorf("ensure browser session schema: %w", err)
	}
	return nil
}

// CreateSession creates or replaces one hash-only browser session row.
func (s *BrowserSessionStore) CreateSession(ctx context.Context, record BrowserSessionRecord) error {
	if s.db == nil {
		return errors.New("browser session store database is required")
	}
	record = normalizeBrowserSessionRecord(record)
	if err := validateBrowserSessionRecord(record); err != nil {
		return err
	}
	allowedScopes, err := marshalBrowserSessionStrings(record.AllowedScopeIDs)
	if err != nil {
		return err
	}
	allowedRepositories, err := marshalBrowserSessionStrings(record.AllowedRepositoryIDs)
	if err != nil {
		return err
	}
	roleIDs, err := marshalBrowserSessionStrings(record.RoleIDs)
	if err != nil {
		return err
	}
	externalGroupHashes, err := marshalBrowserSessionStrings(record.ExternalGroupHashes)
	if err != nil {
		return err
	}
	allowedPermissionFeatures, err := marshalBrowserSessionStrings(record.AllowedPermissionFeatures)
	if err != nil {
		return err
	}
	allowedPermissionDataClasses, err := marshalBrowserSessionStrings(record.AllowedPermissionDataClasses)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(
		ctx,
		createBrowserSessionQuery,
		record.SessionHash,
		record.CSRFTokenHash,
		record.TenantID,
		record.WorkspaceID,
		record.SubjectIDHash,
		record.SubjectClass,
		record.PolicyRevisionHash,
		roleIDs,
		record.AllScopes,
		allowedScopes,
		allowedRepositories,
		nullBrowserSessionString(record.ExternalProviderConfigID),
		nullBrowserSessionString(record.ExternalSubjectIDHash),
		nullTime(record.ExternalAuthValidatedAt),
		nullTime(record.ExternalAuthStaleAfter),
		record.IssuedAt,
		record.LastSeenAt,
		record.IdleExpiresAt,
		record.AbsoluteExpiresAt,
		nullTime(record.RevokedAt),
		record.UpdatedAt,
		externalGroupHashes,
		record.PermissionCatalogEnforced,
		allowedPermissionFeatures,
		allowedPermissionDataClasses,
	)
	if err != nil {
		return fmt.Errorf("create browser session: %w", err)
	}
	if result != nil {
		affected, err := result.RowsAffected()
		if err == nil && affected == 0 {
			return errors.New("active tenant/workspace is required to create browser session")
		}
	}
	return nil
}

// ResolveSessionHash loads an active session by hash and verifies CSRF when
// the request method requires a browser-origin proof.
func (s *BrowserSessionStore) ResolveSessionHash(
	ctx context.Context,
	sessionHash string,
	csrfTokenHash string,
	requireCSRF bool,
	asOf time.Time,
	idleTimeout time.Duration,
) (BrowserSessionRecord, bool, error) {
	if s.db == nil {
		return BrowserSessionRecord{}, false, errors.New("browser session store database is required")
	}
	sessionHash = strings.TrimSpace(sessionHash)
	csrfTokenHash = strings.TrimSpace(csrfTokenHash)
	if sessionHash == "" {
		return BrowserSessionRecord{}, false, errors.New("session hash is required")
	}
	if asOf.IsZero() {
		return BrowserSessionRecord{}, false, errors.New("session lookup as_of is required")
	}
	if idleTimeout <= 0 {
		return BrowserSessionRecord{}, false, errors.New("session idle timeout is required")
	}
	result, err := s.db.ExecContext(
		ctx,
		revokeStaleOIDCBrowserSessionQuery,
		sessionHash,
		asOf.UTC(),
	)
	if err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("revoke stale oidc browser session: %w", err)
	}
	if result != nil {
		affected, err := result.RowsAffected()
		if err == nil && affected > 0 {
			return BrowserSessionRecord{}, false, ErrBrowserSessionRefreshRequired
		}
	}
	if requireCSRF && csrfTokenHash == "" {
		return BrowserSessionRecord{}, false, ErrBrowserSessionCSRFInvalid
	}
	rows, err := s.db.QueryContext(
		ctx,
		resolveBrowserSessionQuery,
		sessionHash,
		csrfTokenHash,
		requireCSRF,
		asOf.UTC(),
		asOf.Add(idleTimeout).UTC(),
	)
	if err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("resolve browser session: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return BrowserSessionRecord{}, false, fmt.Errorf("resolve browser session: %w", err)
		}
		return BrowserSessionRecord{}, false, nil
	}
	record, csrfOK, err := scanBrowserSession(rows)
	if err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("resolve browser session: %w", err)
	}
	if err := rows.Err(); err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("resolve browser session: %w", err)
	}
	if requireCSRF && !csrfOK {
		return BrowserSessionRecord{}, false, ErrBrowserSessionCSRFInvalid
	}
	return record, true, nil
}

// RevokeSession marks one active browser session as revoked by hash.
func (s *BrowserSessionStore) RevokeSession(
	ctx context.Context,
	sessionHash string,
	revokedAt time.Time,
) error {
	if s.db == nil {
		return errors.New("browser session store database is required")
	}
	sessionHash = strings.TrimSpace(sessionHash)
	if sessionHash == "" {
		return errors.New("session hash is required")
	}
	if revokedAt.IsZero() {
		return errors.New("session revoked_at is required")
	}
	if _, err := s.db.ExecContext(ctx, revokeBrowserSessionQuery, sessionHash, revokedAt.UTC()); err != nil {
		return fmt.Errorf("revoke browser session: %w", err)
	}
	return nil
}

// SwitchSessionWorkspace moves one active session to another active workspace.
func (s *BrowserSessionStore) SwitchSessionWorkspace(
	ctx context.Context,
	sessionHash string,
	tenantID string,
	workspaceID string,
	switchedAt time.Time,
) (BrowserSessionRecord, bool, error) {
	if s.db == nil {
		return BrowserSessionRecord{}, false, errors.New("browser session store database is required")
	}
	sessionHash = strings.TrimSpace(sessionHash)
	tenantID = strings.TrimSpace(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	if sessionHash == "" || tenantID == "" || workspaceID == "" {
		return BrowserSessionRecord{}, false, errors.New("session hash, tenant, and workspace are required")
	}
	if switchedAt.IsZero() {
		return BrowserSessionRecord{}, false, errors.New("session switched_at is required")
	}
	rows, err := s.db.QueryContext(
		ctx,
		switchBrowserSessionWorkspaceQuery,
		sessionHash,
		tenantID,
		workspaceID,
		switchedAt.UTC(),
	)
	if err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("switch browser session workspace: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return BrowserSessionRecord{}, false, fmt.Errorf("switch browser session workspace: %w", err)
		}
		return BrowserSessionRecord{}, false, nil
	}
	record, _, err := scanBrowserSession(rows)
	if err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("switch browser session workspace: %w", err)
	}
	if err := rows.Err(); err != nil {
		return BrowserSessionRecord{}, false, fmt.Errorf("switch browser session workspace: %w", err)
	}
	return record, true, nil
}

func normalizeBrowserSessionRecord(record BrowserSessionRecord) BrowserSessionRecord {
	record.SessionHash = strings.TrimSpace(record.SessionHash)
	record.CSRFTokenHash = strings.TrimSpace(record.CSRFTokenHash)
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.SubjectIDHash = strings.TrimSpace(record.SubjectIDHash)
	record.SubjectClass = strings.TrimSpace(record.SubjectClass)
	record.PolicyRevisionHash = strings.TrimSpace(record.PolicyRevisionHash)
	record.RoleIDs = cleanBrowserSessionStrings(record.RoleIDs)
	record.AllowedScopeIDs = cleanBrowserSessionStrings(record.AllowedScopeIDs)
	record.AllowedRepositoryIDs = cleanBrowserSessionStrings(record.AllowedRepositoryIDs)
	record.AllowedPermissionFeatures = cleanBrowserSessionStrings(record.AllowedPermissionFeatures)
	record.AllowedPermissionDataClasses = cleanBrowserSessionStrings(record.AllowedPermissionDataClasses)
	record.ExternalProviderConfigID = strings.TrimSpace(record.ExternalProviderConfigID)
	record.ExternalSubjectIDHash = strings.TrimSpace(record.ExternalSubjectIDHash)
	record.ExternalGroupHashes = cleanBrowserSessionStrings(record.ExternalGroupHashes)
	record.ExternalAuthValidatedAt = record.ExternalAuthValidatedAt.UTC()
	record.ExternalAuthStaleAfter = record.ExternalAuthStaleAfter.UTC()
	record.IssuedAt = record.IssuedAt.UTC()
	record.LastSeenAt = record.LastSeenAt.UTC()
	record.IdleExpiresAt = record.IdleExpiresAt.UTC()
	record.AbsoluteExpiresAt = record.AbsoluteExpiresAt.UTC()
	record.RevokedAt = record.RevokedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record
}

func validateBrowserSessionRecord(record BrowserSessionRecord) error {
	if blank(record.SessionHash) || blank(record.CSRFTokenHash) || blank(record.TenantID) ||
		blank(record.WorkspaceID) {
		return errors.New("session hash, csrf hash, tenant, and workspace are required")
	}
	if record.IssuedAt.IsZero() || record.LastSeenAt.IsZero() ||
		record.IdleExpiresAt.IsZero() || record.AbsoluteExpiresAt.IsZero() ||
		record.UpdatedAt.IsZero() {
		return errors.New("session issued, last seen, expiry, and updated timestamps are required")
	}
	if !record.IdleExpiresAt.After(record.IssuedAt) {
		return errors.New("session idle_expires_at must be after issued_at")
	}
	if !record.AbsoluteExpiresAt.After(record.IssuedAt) {
		return errors.New("session absolute_expires_at must be after issued_at")
	}
	if record.IdleExpiresAt.After(record.AbsoluteExpiresAt) {
		return errors.New("session idle_expires_at must not exceed absolute_expires_at")
	}
	hasExternalAuth := record.ExternalProviderConfigID != "" || record.ExternalSubjectIDHash != "" ||
		!record.ExternalAuthValidatedAt.IsZero() || !record.ExternalAuthStaleAfter.IsZero()
	if hasExternalAuth {
		if blank(record.ExternalProviderConfigID) || blank(record.ExternalSubjectIDHash) ||
			record.ExternalAuthValidatedAt.IsZero() || record.ExternalAuthStaleAfter.IsZero() {
			return errors.New("external auth provider, subject hash, validation, and stale timestamps must be set together")
		}
		if len(record.ExternalGroupHashes) == 0 {
			return errors.New("external auth group hashes must be set")
		}
		if !record.ExternalAuthStaleAfter.After(record.ExternalAuthValidatedAt) {
			return errors.New("external_auth_stale_after must be after external_auth_validated_at")
		}
	}
	return nil
}

func scanBrowserSession(rows Rows) (BrowserSessionRecord, bool, error) {
	var record BrowserSessionRecord
	var roleIDBytes, allowedScopeBytes, allowedRepositoryBytes []byte
	var allowedPermissionFeatureBytes, allowedPermissionDataClassBytes []byte
	var revokedAt sql.NullTime
	var csrfOK bool
	if err := rows.Scan(
		&record.SessionHash,
		&record.CSRFTokenHash,
		&record.TenantID,
		&record.WorkspaceID,
		&record.SubjectIDHash,
		&record.SubjectClass,
		&record.PolicyRevisionHash,
		&roleIDBytes,
		&record.AllScopes,
		&record.PermissionCatalogEnforced,
		&allowedScopeBytes,
		&allowedRepositoryBytes,
		&allowedPermissionFeatureBytes,
		&allowedPermissionDataClassBytes,
		&record.IssuedAt,
		&record.LastSeenAt,
		&record.IdleExpiresAt,
		&record.AbsoluteExpiresAt,
		&revokedAt,
		&csrfOK,
	); err != nil {
		return BrowserSessionRecord{}, false, err
	}
	roleIDs, err := unmarshalBrowserSessionStrings(roleIDBytes)
	if err != nil {
		return BrowserSessionRecord{}, false, err
	}
	allowedScopeIDs, err := unmarshalBrowserSessionStrings(allowedScopeBytes)
	if err != nil {
		return BrowserSessionRecord{}, false, err
	}
	allowedRepositoryIDs, err := unmarshalBrowserSessionStrings(allowedRepositoryBytes)
	if err != nil {
		return BrowserSessionRecord{}, false, err
	}
	allowedPermissionFeatures, err := unmarshalBrowserSessionStrings(allowedPermissionFeatureBytes)
	if err != nil {
		return BrowserSessionRecord{}, false, err
	}
	allowedPermissionDataClasses, err := unmarshalBrowserSessionStrings(allowedPermissionDataClassBytes)
	if err != nil {
		return BrowserSessionRecord{}, false, err
	}
	record.RoleIDs = roleIDs
	record.AllowedScopeIDs = allowedScopeIDs
	record.AllowedRepositoryIDs = allowedRepositoryIDs
	record.AllowedPermissionFeatures = allowedPermissionFeatures
	record.AllowedPermissionDataClasses = allowedPermissionDataClasses
	record.RevokedAt = timeFromNull(revokedAt)
	return normalizeBrowserSessionRecord(record), csrfOK, nil
}

func marshalBrowserSessionStrings(values []string) (string, error) {
	cleaned := cleanBrowserSessionStrings(values)
	data, err := json.Marshal(cleaned)
	if err != nil {
		return "", fmt.Errorf("marshal browser session strings: %w", err)
	}
	return string(data), nil
}

func unmarshalBrowserSessionStrings(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("unmarshal browser session strings: %w", err)
	}
	return cleanBrowserSessionStrings(values), nil
}

func cleanBrowserSessionStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func nullBrowserSessionString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}
