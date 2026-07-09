// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type postgresBrowserSessionAdapter struct {
	store      *pgstatus.BrowserSessionStore
	idleWindow time.Duration
	// signInPolicy records the require_sso guardrail's SSO-admin-proof signal
	// (issue #4968, epic #4962) from CreateBrowserSession — see that method's
	// doc comment for why this is the single choke point both OIDC and SAML
	// funnel through. nil in the (test-only) construction paths that never
	// wire a database.
	signInPolicy *pgstatus.IdentitySubjectStore
}

func newPostgresBrowserSessionAdapter(
	db *sql.DB,
	instruments *telemetry.Instruments,
) *postgresBrowserSessionAdapter {
	if db == nil {
		return nil
	}
	sessionDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		sessionDB = &pgstatus.InstrumentedDB{
			Inner:       sessionDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "browser_sessions",
		}
	}
	signInPolicyDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		signInPolicyDB = &pgstatus.InstrumentedDB{
			Inner:       signInPolicyDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "identity_sign_in_policy",
		}
	}
	return &postgresBrowserSessionAdapter{
		store:        pgstatus.NewBrowserSessionStore(sessionDB),
		idleWindow:   query.DefaultBrowserSessionIdleTimeout,
		signInPolicy: pgstatus.NewIdentitySubjectStore(signInPolicyDB),
	}
}

func newBrowserSessionHandler(
	db *sql.DB,
	instruments *telemetry.Instruments,
	cookieSecureMode query.CookieSecureMode,
) *query.BrowserSessionHandler {
	handler := &query.BrowserSessionHandler{CookieSecure: cookieSecureMode}
	if store := newPostgresBrowserSessionAdapter(db, instruments); store != nil {
		handler.Store = store
	}
	return handler
}

func newBrowserSessionResolver(
	db *sql.DB,
	instruments *telemetry.Instruments,
) query.BrowserSessionResolver {
	resolver := newPostgresBrowserSessionAdapter(db, instruments)
	if resolver == nil {
		return nil
	}
	return resolver
}

func newBrowserSessionStore(
	db *sql.DB,
	instruments *telemetry.Instruments,
) query.BrowserSessionStore {
	store := newPostgresBrowserSessionAdapter(db, instruments)
	if store == nil {
		return nil
	}
	return store
}

func wrapAPIAuth(
	apiKey string,
	scopedTokenResolver query.ScopedTokenResolver,
	sessionResolver query.BrowserSessionResolver,
	next http.Handler,
	audit query.GovernanceAuditAppender,
) http.Handler {
	return query.AuthMiddlewareWithBrowserSessionsScopedTokensAndGovernanceAudit(
		apiKey,
		scopedTokenResolver,
		sessionResolver,
		next,
		audit,
	)
}

// CreateBrowserSession persists one dashboard session. It is the single
// choke point every session-issuing path funnels through — local/break-glass
// login (LocalIdentityHandler), OIDC (BrowserSessionHandler.
// issueBrowserSessionWithExternalAuth), and SAML (SAMLHandler.createSession)
// all call this same interface method — so it is also where the require_sso
// guardrail's SSO-admin-proof signal (issue #4968, epic #4962) is captured:
// whenever the resulting session is both an admin (AllScopes) and was
// established via an external IdP (ExternalProviderConfigID set), record
// that this tenant has now proven "an admin can sign in via SSO," one half
// of the guardrail that gates enabling require_sso. Local and break-glass
// sessions never carry ExternalProviderConfigID, so they never trigger this.
//
// The record is best-effort (logged, not fatal): a transient failure to
// persist this proof must never fail the login itself — the same
// best-effort convention governance audit already uses on this path.
func (a *postgresBrowserSessionAdapter) CreateBrowserSession(
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
	a.recordSSOAdminVerificationBestEffort(ctx, record)
	return nil
}

func (a *postgresBrowserSessionAdapter) recordSSOAdminVerificationBestEffort(
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
		slog.ErrorContext(ctx, "sign-in policy sso admin verification record failed",
			"err", err, "tenant_id", record.TenantID)
	}
}

func (a *postgresBrowserSessionAdapter) RevokeBrowserSession(
	ctx context.Context,
	sessionHash string,
	revokedAt time.Time,
) error {
	return a.store.RevokeSession(ctx, sessionHash, revokedAt)
}

func (a *postgresBrowserSessionAdapter) SwitchBrowserSessionWorkspace(
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
	return authContextFromBrowserSessionRecord(record), true, nil
}

func (a *postgresBrowserSessionAdapter) ResolveBrowserSession(
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
	return authContextFromBrowserSessionRecord(record), true, nil
}

func authContextFromBrowserSessionRecord(record pgstatus.BrowserSessionRecord) query.AuthContext {
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

// ListSessionsBySubject delegates to the postgres store, mapping postgres
// BrowserSessionListItem to the query-layer type. It never exposes
// session_hash, csrf_token_hash, or external identity secrets.
func (a *postgresBrowserSessionAdapter) ListSessionsBySubject(
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

// newBrowserSessionListHandler builds a BrowserSessionListHandler backed by
// the postgres store when a database connection is available.
func newBrowserSessionListHandler(db *sql.DB, instruments *telemetry.Instruments) *query.BrowserSessionListHandler {
	adapter := newPostgresBrowserSessionAdapter(db, instruments)
	if adapter == nil {
		return &query.BrowserSessionListHandler{}
	}
	return &query.BrowserSessionListHandler{Store: adapter}
}

// newProfileHandler builds a ProfileHandler backed by the local identity store.
func newProfileHandler(db *sql.DB, instruments *telemetry.Instruments, governanceAudit query.GovernanceAuditSummaryReader) *query.ProfileHandler {
	store := newPostgresLocalIdentityAdapter(db, instruments)
	if store == nil {
		return &query.ProfileHandler{}
	}
	return &query.ProfileHandler{LocalIdentityStore: store}
}
