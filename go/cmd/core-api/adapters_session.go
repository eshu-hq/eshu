// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// corePostgresBrowserSessionAdapter backs dashboard sessions and the
// per-tenant session-timeout override over Postgres.
type corePostgresBrowserSessionAdapter struct {
	store      *pgstatus.BrowserSessionStore
	idleWindow time.Duration
	// signInPolicy resolves the require_sso guardrail's SSO-admin proof and
	// the per-tenant session-timeout override from the same identity store.
	signInPolicy *pgstatus.IdentitySubjectStore
}

// GetSignInPolicy implements query.SignInPolicyReadStore on the same adapter
// instance the session handlers carry, without a second connection.
func (a *corePostgresBrowserSessionAdapter) GetSignInPolicy(
	ctx context.Context,
	tenantID string,
) (query.SignInPolicy, error) {
	if a.signInPolicy == nil {
		return query.SignInPolicy{}, errors.New("sign-in policy store is unavailable")
	}
	policy, err := a.signInPolicy.GetSignInPolicy(ctx, tenantID)
	if err != nil {
		return query.SignInPolicy{}, err
	}
	return signInPolicyFromCorePostgres(policy), nil
}

// newCorePostgresBrowserSessionAdapter builds the adapter, or nil without a
// database.
func newCorePostgresBrowserSessionAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *corePostgresBrowserSessionAdapter {
	if rawDB == nil {
		return nil
	}
	return &corePostgresBrowserSessionAdapter{
		store:        pgstatus.NewBrowserSessionStore(coreSessionDB(rawDB, instruments, "browser_sessions")),
		idleWindow:   query.DefaultBrowserSessionIdleTimeout,
		signInPolicy: pgstatus.NewIdentitySubjectStore(coreSessionDB(rawDB, instruments, "identity_sign_in_policy")),
	}
}

// coreSessionDB wraps a writer handle with per-surface telemetry.
func coreSessionDB(rawDB *sql.DB, instruments *telemetry.Instruments, storeName string) db.ExecQueryer {
	return coreInstrumentedDB(rawDB, instruments, storeName)
}

// newCoreBrowserSessionHandler wires dashboard session issuance over the
// Postgres adapter.
func newCoreBrowserSessionHandler(
	database *sql.DB,
	instruments *telemetry.Instruments,
	cookieSecureMode query.CookieSecureMode,
) *query.BrowserSessionHandler {
	handler := &query.BrowserSessionHandler{CookieSecure: cookieSecureMode}
	if store := newCorePostgresBrowserSessionAdapter(database, instruments); store != nil {
		handler.Store = store
		handler.SignInPolicy = store
	}
	return handler
}

// newCoreBrowserSessionStore exposes the adapter as the session store.
func newCoreBrowserSessionStore(
	database *sql.DB,
	instruments *telemetry.Instruments,
) query.BrowserSessionStore {
	store := newCorePostgresBrowserSessionAdapter(database, instruments)
	if store == nil {
		return nil
	}
	return store
}

// CoreBrowserSessionResolver builds the auth-chain session resolver over
// Postgres. Nil without a database.
func CoreBrowserSessionResolver(
	database *sql.DB,
	instruments *telemetry.Instruments,
) query.BrowserSessionResolver {
	store := newCorePostgresBrowserSessionAdapter(database, instruments)
	if store == nil {
		return nil
	}
	return store
}

// newCoreBrowserSessionListHandler wires session listing over Postgres.
func newCoreBrowserSessionListHandler(
	database *sql.DB,
	instruments *telemetry.Instruments,
) *query.BrowserSessionListHandler {
	adapter := newCorePostgresBrowserSessionAdapter(database, instruments)
	if adapter == nil {
		return &query.BrowserSessionListHandler{}
	}
	return &query.BrowserSessionListHandler{Store: adapter}
}

// newCoreProfileHandler wires the profile reads over the local identity
// store.
func newCoreProfileHandler(
	database *sql.DB,
	instruments *telemetry.Instruments,
) *query.ProfileHandler {
	store := newCorePostgresLocalIdentityAdapter(database, instruments)
	if store == nil {
		return &query.ProfileHandler{}
	}
	return &query.ProfileHandler{LocalIdentityStore: store}
}

// CreateBrowserSession persists one dashboard session. It is the single
// choke point every session-issuing path funnels through, so it also
// captures the require_sso guardrail's SSO-admin-proof signal (best-effort,
// logged, never fatal to the login itself).
func (a *corePostgresBrowserSessionAdapter) CreateBrowserSession(
	ctx context.Context,
	record query.BrowserSessionCreateRecord,
) error {
	if err := a.store.CreateSession(ctx, pgstatus.BrowserSessionRecord{
		SessionHash:                  record.SessionHash,
		CSRFTokenHash:                record.CSRFTokenHash,
		TenantID:                     record.TenantID,
		WorkspaceID:                  record.WorkspaceID,
		SubjectIDHash:                record.SubjectIDHash,
		SubjectClass:                 record.SubjectClass,
		PolicyRevisionHash:           record.PolicyRevisionHash,
		RoleIDs:                      append([]string(nil), record.RoleIDs...),
		AllScopes:                    record.AllScopes,
		PermissionCatalogEnforced:    record.PermissionCatalogEnforced,
		AllowedScopeIDs:              append([]string(nil), record.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), record.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), record.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), record.AllowedPermissionDataClasses...),
		ExternalProviderConfigID:     record.ExternalProviderConfigID,
		ExternalSubjectIDHash:        record.ExternalSubjectIDHash,
		ExternalGroupHashes:          append([]string(nil), record.ExternalGroupHashes...),
		ExternalAuthValidatedAt:      record.ExternalAuthValidatedAt,
		ExternalAuthStaleAfter:       record.ExternalAuthStaleAfter,
		IssuedAt:                     record.IssuedAt,
		LastSeenAt:                   record.LastSeenAt,
		IdleExpiresAt:                record.IdleExpiresAt,
		AbsoluteExpiresAt:            record.AbsoluteExpiresAt,
		UpdatedAt:                    record.UpdatedAt,
	}); err != nil {
		return err
	}
	a.recordCoreSSOAdminVerificationBestEffort(ctx, record)
	return nil
}

func (a *corePostgresBrowserSessionAdapter) recordCoreSSOAdminVerificationBestEffort(
	ctx context.Context,
	record query.BrowserSessionCreateRecord,
) {
	if a.signInPolicy == nil || !record.AllScopes || record.ExternalProviderConfigID == "" {
		return
	}
	verifiedAt := record.ExternalAuthValidatedAt
	if verifiedAt.IsZero() {
		verifiedAt = record.IssuedAt
	}
	if err := a.signInPolicy.RecordSSOAdminVerification(ctx, record.TenantID, record.ExternalProviderConfigID, verifiedAt); err != nil {
		slog.Default().ErrorContext(ctx, "sign-in policy sso admin verification record failed",
			"err", err, "tenant_id", record.TenantID)
	}
}

// RevokeBrowserSession revokes one dashboard session.
func (a *corePostgresBrowserSessionAdapter) RevokeBrowserSession(
	ctx context.Context,
	sessionHash string,
	revokedAt time.Time,
) error {
	return a.store.RevokeSession(ctx, sessionHash, revokedAt)
}

// SwitchBrowserSessionWorkspace moves a session across workspaces.
func (a *corePostgresBrowserSessionAdapter) SwitchBrowserSessionWorkspace(
	ctx context.Context,
	sessionHash string,
	tenantID string,
	workspaceID string,
	switchedAt time.Time,
) (query.AuthContext, bool, error) {
	record, ok, err := a.store.SwitchSessionWorkspace(ctx, sessionHash, tenantID, workspaceID, switchedAt)
	if err != nil {
		return query.AuthContext{}, false, err
	}
	if !ok {
		return query.AuthContext{}, false, nil
	}
	return authContextFromCoreBrowserSessionRecord(record), true, nil
}

// ResolveBrowserSession resolves a session hash to its auth context.
func (a *corePostgresBrowserSessionAdapter) ResolveBrowserSession(
	ctx context.Context,
	sessionHash string,
	csrfTokenHash string,
	requireCSRF bool,
	asOf time.Time,
) (query.AuthContext, bool, error) {
	record, ok, err := a.store.ResolveSessionHash(ctx, sessionHash, csrfTokenHash, requireCSRF, asOf, a.idleWindow)
	if errors.Is(err, pgstatus.ErrBrowserSessionCSRFInvalid) {
		return query.AuthContext{}, false, query.ErrBrowserSessionCSRFInvalid
	}
	if errors.Is(err, pgstatus.ErrBrowserSessionRefreshRequired) {
		return query.AuthContext{}, false, query.ErrBrowserSessionRefreshRequired
	}
	if err != nil {
		return query.AuthContext{}, false, err
	}
	if !ok {
		return query.AuthContext{}, false, nil
	}
	return authContextFromCoreBrowserSessionRecord(record), true, nil
}

func authContextFromCoreBrowserSessionRecord(record pgstatus.BrowserSessionRecord) query.AuthContext {
	return query.AuthContext{
		Mode:                         query.AuthModeBrowserSession,
		TenantID:                     record.TenantID,
		WorkspaceID:                  record.WorkspaceID,
		SubjectClass:                 record.SubjectClass,
		SubjectIDHash:                record.SubjectIDHash,
		PolicyRevisionHash:           record.PolicyRevisionHash,
		RoleIDs:                      append([]string(nil), record.RoleIDs...),
		AllScopes:                    record.AllScopes,
		PermissionCatalogEnforced:    record.PermissionCatalogEnforced,
		AllowedScopeIDs:              append([]string(nil), record.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), record.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), record.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), record.AllowedPermissionDataClasses...),
		ExternalProviderConfigID:     record.ExternalProviderConfigID,
	}
}

// ListSessionsBySubject lists metadata-only session rows. It never exposes
// session hashes or external identity secrets.
func (a *corePostgresBrowserSessionAdapter) ListSessionsBySubject(
	ctx context.Context,
	subjectIDHash string,
	asOf time.Time,
	sessionHash string,
	limit int,
	offset int,
) ([]query.BrowserSessionListItem, error) {
	items, err := a.store.ListSessionsBySubject(ctx, subjectIDHash, asOf, sessionHash, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]query.BrowserSessionListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.BrowserSessionListItem{
			IssuedAt:          item.IssuedAt,
			LastSeenAt:        item.LastSeenAt,
			IdleExpiresAt:     item.IdleExpiresAt,
			AbsoluteExpiresAt: item.AbsoluteExpiresAt,
			TenantID:          item.TenantID,
			WorkspaceID:       item.WorkspaceID,
			Current:           item.Current,
			RevokedAt:         item.RevokedAt,
		})
	}
	return out, nil
}
