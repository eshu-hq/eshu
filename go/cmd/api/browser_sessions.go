package main

import (
	"context"
	"database/sql"
	"errors"
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
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "browser_sessions",
		}
	}
	return &postgresBrowserSessionAdapter{
		store:      pgstatus.NewBrowserSessionStore(sessionDB),
		idleWindow: query.DefaultBrowserSessionIdleTimeout,
	}
}

func newBrowserSessionHandler(db *sql.DB, instruments *telemetry.Instruments) *query.BrowserSessionHandler {
	handler := &query.BrowserSessionHandler{}
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

func (a *postgresBrowserSessionAdapter) CreateBrowserSession(
	ctx context.Context,
	record query.BrowserSessionCreateRecord,
) error {
	return a.store.CreateSession(ctx, pgstatus.BrowserSessionRecord{
		SessionHash:          record.SessionHash,
		CSRFTokenHash:        record.CSRFTokenHash,
		TenantID:             record.TenantID,
		WorkspaceID:          record.WorkspaceID,
		SubjectIDHash:        record.SubjectIDHash,
		SubjectClass:         record.SubjectClass,
		PolicyRevisionHash:   record.PolicyRevisionHash,
		AllScopes:            record.AllScopes,
		AllowedScopeIDs:      append([]string(nil), record.AllowedScopeIDs...),
		AllowedRepositoryIDs: append([]string(nil), record.AllowedRepositoryIDs...),
		IssuedAt:             record.IssuedAt,
		LastSeenAt:           record.LastSeenAt,
		IdleExpiresAt:        record.IdleExpiresAt,
		AbsoluteExpiresAt:    record.AbsoluteExpiresAt,
		UpdatedAt:            record.UpdatedAt,
	})
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
		Mode:                 query.AuthModeBrowserSession,
		TenantID:             record.TenantID,
		WorkspaceID:          record.WorkspaceID,
		SubjectClass:         record.SubjectClass,
		SubjectIDHash:        record.SubjectIDHash,
		PolicyRevisionHash:   record.PolicyRevisionHash,
		AllScopes:            record.AllScopes,
		AllowedScopeIDs:      append([]string(nil), record.AllowedScopeIDs...),
		AllowedRepositoryIDs: append([]string(nil), record.AllowedRepositoryIDs...),
	}
}
