// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// StaleOIDCSessionRecord is the hash-only projection of one OIDC-backed browser
// session whose external provider proof has reached its bounded staleness
// window and is therefore eligible for active-session revocation refresh.
//
// It carries only the opaque identifiers a refresher needs to re-resolve
// provider-driven grants: the session hash, the provider config id, the hashed
// provider subject, the tenant/workspace boundary, the hashed external group
// claims captured at login, and the last resolved grant snapshot. It never
// carries raw provider tokens, raw group names, emails, or private endpoints.
type StaleOIDCSessionRecord struct {
	SessionHash              string
	ExternalProviderConfigID string
	ExternalSubjectIDHash    string
	TenantID                 string
	WorkspaceID              string
	PolicyRevisionHash       string
	RoleIDs                  []string
	AllScopes                bool
	AllowedScopeIDs          []string
	AllowedRepositoryIDs     []string
	ExternalGroupHashes      []string
	ExternalAuthValidatedAt  time.Time
	ExternalAuthStaleAfter   time.Time
}

// OIDCSessionAuthProofUpdate carries the new hash-only authorization snapshot
// and bounded proof window written back to one OIDC-backed browser session when
// active-session refresh re-confirms the external subject is still authorized.
type OIDCSessionAuthProofUpdate struct {
	SessionHash                  string
	ExternalAuthValidatedAt      time.Time
	ExternalAuthStaleAfter       time.Time
	PolicyRevisionHash           string
	ExternalGroupHashes          []string
	RoleIDs                      []string
	AllScopes                    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	UpdatedAt                    time.Time
}

// ListStaleOIDCSessions returns up to limit active OIDC-backed browser sessions
// whose external provider proof is stale as of asOf, ordered oldest-stale
// first. The bounded limit keeps each refresh pass proportional to the configured
// batch size rather than the full session table, so the worker never performs an
// unbounded scan. Sessions are read using the
// browser_sessions_external_auth_stale_idx partial index.
func (s *BrowserSessionStore) ListStaleOIDCSessions(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]StaleOIDCSessionRecord, error) {
	if s.db == nil {
		return nil, errors.New("browser session store database is required")
	}
	if asOf.IsZero() {
		return nil, errors.New("stale oidc session as_of is required")
	}
	if limit <= 0 {
		return nil, errors.New("stale oidc session limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, listStaleOIDCBrowserSessionsQuery, asOf.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list stale oidc sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]StaleOIDCSessionRecord, 0, limit)
	for rows.Next() {
		record, err := scanStaleOIDCSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list stale oidc sessions: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list stale oidc sessions: %w", err)
	}
	return records, nil
}

// UpdateOIDCSessionAuthProof atomically refreshes one OIDC-backed browser
// session's bounded proof window and authorization snapshot after active-session
// refresh re-confirms the external subject. The WHERE clause keeps the write
// idempotent and safe under concurrent refreshers: it only touches an
// unrevoked external_oidc_user row, so a session that another worker already
// revoked stays revoked.
func (s *BrowserSessionStore) UpdateOIDCSessionAuthProof(
	ctx context.Context,
	update OIDCSessionAuthProofUpdate,
) error {
	if s.db == nil {
		return errors.New("browser session store database is required")
	}
	update = normalizeOIDCSessionAuthProofUpdate(update)
	if err := validateOIDCSessionAuthProofUpdate(update); err != nil {
		return err
	}
	roleIDs, err := marshalBrowserSessionStrings(update.RoleIDs)
	if err != nil {
		return err
	}
	allowedScopes, err := marshalBrowserSessionStrings(update.AllowedScopeIDs)
	if err != nil {
		return err
	}
	allowedRepositories, err := marshalBrowserSessionStrings(update.AllowedRepositoryIDs)
	if err != nil {
		return err
	}
	externalGroupHashes, err := marshalBrowserSessionStrings(update.ExternalGroupHashes)
	if err != nil {
		return err
	}
	allowedPermissionFeatures, err := marshalBrowserSessionStrings(update.AllowedPermissionFeatures)
	if err != nil {
		return err
	}
	allowedPermissionDataClasses, err := marshalBrowserSessionStrings(update.AllowedPermissionDataClasses)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(
		ctx,
		updateOIDCBrowserSessionAuthProofQuery,
		update.SessionHash,
		update.ExternalAuthValidatedAt,
		update.ExternalAuthStaleAfter,
		update.PolicyRevisionHash,
		roleIDs,
		update.AllScopes,
		allowedScopes,
		allowedRepositories,
		update.UpdatedAt,
		externalGroupHashes,
		allowedPermissionFeatures,
		allowedPermissionDataClasses,
	); err != nil {
		return fmt.Errorf("update oidc session auth proof: %w", err)
	}
	return nil
}

func scanStaleOIDCSession(rows Rows) (StaleOIDCSessionRecord, error) {
	var record StaleOIDCSessionRecord
	var roleIDBytes, allowedScopeBytes, allowedRepositoryBytes, groupHashBytes []byte
	if err := rows.Scan(
		&record.SessionHash,
		&record.ExternalProviderConfigID,
		&record.ExternalSubjectIDHash,
		&record.TenantID,
		&record.WorkspaceID,
		&record.PolicyRevisionHash,
		&roleIDBytes,
		&record.AllScopes,
		&allowedScopeBytes,
		&allowedRepositoryBytes,
		&record.ExternalAuthValidatedAt,
		&record.ExternalAuthStaleAfter,
		&groupHashBytes,
	); err != nil {
		return StaleOIDCSessionRecord{}, err
	}
	roleIDs, err := unmarshalBrowserSessionStrings(roleIDBytes)
	if err != nil {
		return StaleOIDCSessionRecord{}, err
	}
	allowedScopeIDs, err := unmarshalBrowserSessionStrings(allowedScopeBytes)
	if err != nil {
		return StaleOIDCSessionRecord{}, err
	}
	allowedRepositoryIDs, err := unmarshalBrowserSessionStrings(allowedRepositoryBytes)
	if err != nil {
		return StaleOIDCSessionRecord{}, err
	}
	groupHashes, err := unmarshalBrowserSessionStrings(groupHashBytes)
	if err != nil {
		return StaleOIDCSessionRecord{}, err
	}
	record.RoleIDs = roleIDs
	record.AllowedScopeIDs = allowedScopeIDs
	record.AllowedRepositoryIDs = allowedRepositoryIDs
	record.ExternalGroupHashes = groupHashes
	record.ExternalAuthValidatedAt = record.ExternalAuthValidatedAt.UTC()
	record.ExternalAuthStaleAfter = record.ExternalAuthStaleAfter.UTC()
	return record, nil
}

func normalizeOIDCSessionAuthProofUpdate(update OIDCSessionAuthProofUpdate) OIDCSessionAuthProofUpdate {
	update.SessionHash = strings.TrimSpace(update.SessionHash)
	update.PolicyRevisionHash = strings.TrimSpace(update.PolicyRevisionHash)
	update.ExternalGroupHashes = cleanBrowserSessionStrings(update.ExternalGroupHashes)
	update.RoleIDs = cleanBrowserSessionStrings(update.RoleIDs)
	update.AllowedScopeIDs = cleanBrowserSessionStrings(update.AllowedScopeIDs)
	update.AllowedRepositoryIDs = cleanBrowserSessionStrings(update.AllowedRepositoryIDs)
	update.ExternalAuthValidatedAt = update.ExternalAuthValidatedAt.UTC()
	update.ExternalAuthStaleAfter = update.ExternalAuthStaleAfter.UTC()
	update.UpdatedAt = update.UpdatedAt.UTC()
	return update
}

func validateOIDCSessionAuthProofUpdate(update OIDCSessionAuthProofUpdate) error {
	if blank(update.SessionHash) || blank(update.PolicyRevisionHash) {
		return errors.New("oidc session refresh requires session hash and policy revision hash")
	}
	if len(update.ExternalGroupHashes) == 0 {
		return errors.New("oidc session refresh requires external group hashes")
	}
	if update.ExternalAuthValidatedAt.IsZero() || update.ExternalAuthStaleAfter.IsZero() ||
		update.UpdatedAt.IsZero() {
		return errors.New("oidc session refresh requires validated, stale, and updated timestamps")
	}
	if !update.ExternalAuthStaleAfter.After(update.ExternalAuthValidatedAt) {
		return errors.New("oidc session refresh stale_after must be after validated_at")
	}
	return nil
}
