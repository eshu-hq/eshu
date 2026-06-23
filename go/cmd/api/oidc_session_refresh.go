package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	envAuthOIDCSessionRefreshEnabled   = "ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED"
	envAuthOIDCSessionRefreshInterval  = "ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL"
	envAuthOIDCSessionRefreshBatchSize = "ESHU_AUTH_OIDC_SESSION_REFRESH_BATCH_SIZE"

	defaultOIDCSessionRefreshInterval  = time.Minute
	defaultOIDCSessionRefreshBatchSize = oidclogin.DefaultRefreshBatchSize
)

// oidcSessionRefreshConfig is the operator-tunable cadence and bound for the
// bounded OIDC active-session revocation refresh worker.
type oidcSessionRefreshConfig struct {
	Interval  time.Duration
	BatchSize int
	Window    time.Duration
}

// loadOIDCSessionRefreshConfig reads the refresh worker cadence, bound, and
// staleness window from the environment, applying safe defaults. The window
// reuses the same OIDC session refresh window as the request-time stale check so
// proactive and request-time enforcement agree.
func loadOIDCSessionRefreshConfig(getenv func(string) string) (oidcSessionRefreshConfig, error) {
	config := oidcSessionRefreshConfig{
		Interval:  defaultOIDCSessionRefreshInterval,
		BatchSize: defaultOIDCSessionRefreshBatchSize,
		Window:    query.DefaultOIDCSessionRefreshWindow,
	}
	if raw := strings.TrimSpace(getenv(envAuthOIDCSessionRefreshInterval)); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return oidcSessionRefreshConfig{}, fmt.Errorf("parse %s: %w", envAuthOIDCSessionRefreshInterval, err)
		}
		if interval <= 0 {
			return oidcSessionRefreshConfig{}, fmt.Errorf("%s must be positive", envAuthOIDCSessionRefreshInterval)
		}
		config.Interval = interval
	}
	if raw := strings.TrimSpace(getenv(envAuthOIDCSessionRefreshBatchSize)); raw != "" {
		batchSize, err := strconv.Atoi(raw)
		if err != nil {
			return oidcSessionRefreshConfig{}, fmt.Errorf("parse %s: %w", envAuthOIDCSessionRefreshBatchSize, err)
		}
		if batchSize <= 0 {
			return oidcSessionRefreshConfig{}, fmt.Errorf("%s must be positive", envAuthOIDCSessionRefreshBatchSize)
		}
		config.BatchSize = batchSize
	}
	if raw := strings.TrimSpace(getenv(envAuthOIDCSessionRefreshWindow)); raw != "" {
		window, err := time.ParseDuration(raw)
		if err != nil {
			return oidcSessionRefreshConfig{}, fmt.Errorf("parse %s: %w", envAuthOIDCSessionRefreshWindow, err)
		}
		if window <= 0 {
			return oidcSessionRefreshConfig{}, fmt.Errorf("%s must be positive", envAuthOIDCSessionRefreshWindow)
		}
		config.Window = window
	}
	return config, nil
}

// oidcSessionRefreshWorker runs bounded OIDC active-session revocation refresh
// passes on a fixed cadence and emits operator-facing telemetry for each pass.
type oidcSessionRefreshWorker struct {
	refresher *oidclogin.Refresher
	interval  time.Duration
	logger    *slog.Logger

	passes              metric.Int64Counter
	scanned             metric.Int64Counter
	revoked             metric.Int64Counter
	refreshed           metric.Int64Counter
	providerUnavailable metric.Int64Counter
}

// newOIDCSessionRefreshWorker builds the refresh worker from Postgres adapters,
// or returns nil when the worker is disabled or Postgres is unavailable. The
// worker is gated on ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED so deployments that
// have not finished security review keep it off.
func newOIDCSessionRefreshWorker(
	getenv func(string) string,
	db *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*oidcSessionRefreshWorker, error) {
	if !boolEnv(getenv(envAuthOIDCSessionRefreshEnabled)) {
		return nil, nil
	}
	if db == nil {
		return nil, fmt.Errorf("postgres is required for oidc session refresh")
	}
	config, err := loadOIDCSessionRefreshConfig(getenv)
	if err != nil {
		return nil, err
	}

	sessionDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	oidcDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		sessionDB = &pgstatus.InstrumentedDB{
			Inner:       sessionDB,
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "browser_sessions",
		}
		oidcDB = &pgstatus.InstrumentedDB{
			Inner:       oidcDB,
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "identity_oidc_login",
		}
	}
	sessionStore := pgstatus.NewBrowserSessionStore(sessionDB)
	oidcStore := pgstatus.NewOIDCLoginStore(oidcDB)

	refresher := oidclogin.NewRefresher(
		&postgresSessionRefreshStore{store: sessionStore},
		&postgresRoleGrantResolver{store: oidcStore},
		oidclogin.RefreshConfig{
			BatchSize:     config.BatchSize,
			Window:        config.Window,
			SubjectLookup: &postgresExternalSubjectLookup{store: oidcStore},
		},
	)

	worker := &oidcSessionRefreshWorker{
		refresher: refresher,
		interval:  config.Interval,
		logger:    logger,
	}
	if err := worker.registerInstruments(); err != nil {
		return nil, err
	}
	return worker, nil
}

func (w *oidcSessionRefreshWorker) registerInstruments() error {
	meter := otel.Meter("eshu-api")
	var err error
	if w.passes, err = meter.Int64Counter(
		"eshu_auth_oidc_session_refresh_passes_total",
		metric.WithDescription("Bounded OIDC active-session refresh passes completed."),
	); err != nil {
		return fmt.Errorf("register oidc refresh passes counter: %w", err)
	}
	if w.scanned, err = meter.Int64Counter(
		"eshu_auth_oidc_session_refresh_scanned_total",
		metric.WithDescription("Stale OIDC sessions scanned by the refresh worker."),
	); err != nil {
		return fmt.Errorf("register oidc refresh scanned counter: %w", err)
	}
	if w.revoked, err = meter.Int64Counter(
		"eshu_auth_oidc_session_refresh_revoked_total",
		metric.WithDescription("OIDC sessions revoked by active-session refresh."),
	); err != nil {
		return fmt.Errorf("register oidc refresh revoked counter: %w", err)
	}
	if w.refreshed, err = meter.Int64Counter(
		"eshu_auth_oidc_session_refresh_extended_total",
		metric.WithDescription("OIDC sessions whose bounded proof window was extended."),
	); err != nil {
		return fmt.Errorf("register oidc refresh extended counter: %w", err)
	}
	if w.providerUnavailable, err = meter.Int64Counter(
		"eshu_auth_oidc_session_refresh_provider_unavailable_total",
		metric.WithDescription("OIDC refresh decisions deferred because the provider or store was unavailable."),
	); err != nil {
		return fmt.Errorf("register oidc refresh provider-unavailable counter: %w", err)
	}
	return nil
}

// Run executes refresh passes until the context is cancelled. It runs one pass
// immediately, then on the configured interval. Each pass records counts and a
// structured log so an operator can see revocations and provider-unavailable
// decisions without raw provider secrets.
func (w *oidcSessionRefreshWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.runPass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runPass(ctx)
		}
	}
}

func (w *oidcSessionRefreshWorker) runPass(ctx context.Context) {
	outcome, err := w.refresher.RefreshOnce(ctx)
	if w.passes != nil {
		w.passes.Add(ctx, 1)
	}
	if err != nil {
		if w.logger != nil {
			w.logger.Error(
				"oidc session refresh pass failed",
				telemetry.EventAttr("auth.oidc.session_refresh.failed"),
				"error", err,
			)
		}
		return
	}
	w.recordOutcome(ctx, outcome)
	if w.logger != nil && (outcome.Revoked > 0 || outcome.ProviderUnavailable > 0) {
		w.logger.Info(
			"oidc session refresh pass completed",
			telemetry.EventAttr("auth.oidc.session_refresh.completed"),
			"scanned", outcome.Scanned,
			"revoked", outcome.Revoked,
			"refreshed", outcome.Refreshed,
			"provider_unavailable", outcome.ProviderUnavailable,
		)
	}
}

func (w *oidcSessionRefreshWorker) recordOutcome(ctx context.Context, outcome oidclogin.RefreshOutcome) {
	attrs := metric.WithAttributes(attribute.String("subject_class", "external_oidc_user"))
	if w.scanned != nil {
		w.scanned.Add(ctx, int64(outcome.Scanned), attrs)
	}
	if w.revoked != nil {
		w.revoked.Add(ctx, int64(outcome.Revoked), attrs)
	}
	if w.refreshed != nil {
		w.refreshed.Add(ctx, int64(outcome.Refreshed), attrs)
	}
	if w.providerUnavailable != nil {
		w.providerUnavailable.Add(ctx, int64(outcome.ProviderUnavailable), attrs)
	}
}

func boolEnv(raw string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && parsed
}

// postgresSessionRefreshStore adapts the Postgres browser session store to the
// refresher's bounded read/write surface.
type postgresSessionRefreshStore struct {
	store *pgstatus.BrowserSessionStore
}

func (a *postgresSessionRefreshStore) ListStaleSessions(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]oidclogin.StaleSession, error) {
	records, err := a.store.ListStaleOIDCSessions(ctx, asOf, limit)
	if err != nil {
		return nil, err
	}
	sessions := make([]oidclogin.StaleSession, 0, len(records))
	for _, record := range records {
		sessions = append(sessions, oidclogin.StaleSession{
			SessionHash:              record.SessionHash,
			ExternalProviderConfigID: record.ExternalProviderConfigID,
			ExternalSubjectIDHash:    record.ExternalSubjectIDHash,
			TenantID:                 record.TenantID,
			WorkspaceID:              record.WorkspaceID,
			PolicyRevisionHash:       record.PolicyRevisionHash,
			RoleIDs:                  append([]string(nil), record.RoleIDs...),
			AllScopes:                record.AllScopes,
			AllowedScopeIDs:          append([]string(nil), record.AllowedScopeIDs...),
			AllowedRepositoryIDs:     append([]string(nil), record.AllowedRepositoryIDs...),
			ExternalAuthValidatedAt:  record.ExternalAuthValidatedAt,
			ExternalAuthStaleAfter:   record.ExternalAuthStaleAfter,
		})
	}
	return sessions, nil
}

func (a *postgresSessionRefreshStore) RevokeSession(
	ctx context.Context,
	sessionHash string,
	revokedAt time.Time,
) error {
	return a.store.RevokeSession(ctx, sessionHash, revokedAt)
}

func (a *postgresSessionRefreshStore) UpdateSessionAuthProof(
	ctx context.Context,
	update oidclogin.SessionAuthProofUpdate,
) error {
	return a.store.UpdateOIDCSessionAuthProof(ctx, pgstatus.OIDCSessionAuthProofUpdate{
		SessionHash:             update.SessionHash,
		ExternalAuthValidatedAt: update.ExternalAuthValidatedAt,
		ExternalAuthStaleAfter:  update.ExternalAuthStaleAfter,
		PolicyRevisionHash:      update.PolicyRevisionHash,
		RoleIDs:                 append([]string(nil), update.RoleIDs...),
		AllScopes:               update.AllScopes,
		AllowedScopeIDs:         append([]string(nil), update.AllowedScopeIDs...),
		AllowedRepositoryIDs:    append([]string(nil), update.AllowedRepositoryIDs...),
		UpdatedAt:               update.UpdatedAt,
	})
}

// postgresRoleGrantResolver adapts the Postgres OIDC login store's role-based
// re-resolution to the refresher's RoleGrantResolver.
type postgresRoleGrantResolver struct {
	store *pgstatus.OIDCLoginStore
}

func (r *postgresRoleGrantResolver) ResolveGroupGrants(
	ctx context.Context,
	grantQuery oidclogin.GrantQuery,
) (oidclogin.GrantResolution, bool, error) {
	resolution, ok, err := r.store.ResolveActiveRoleGrants(ctx, pgstatus.OIDCRoleGrantQuery{
		ProviderConfigID: grantQuery.ProviderConfigID,
		TenantID:         grantQuery.TenantID,
		WorkspaceID:      grantQuery.WorkspaceID,
		RoleIDs:          append([]string(nil), grantQuery.RoleIDs...),
		AsOf:             grantQuery.AsOf,
	})
	if err != nil || !ok {
		return oidclogin.GrantResolution{}, ok, err
	}
	return oidclogin.GrantResolution{
		RoleIDs:              append([]string(nil), resolution.RoleIDs...),
		PolicyRevisionHash:   resolution.PolicyRevisionHash,
		AllowedScopeIDs:      append([]string(nil), resolution.AllowedScopeIDs...),
		AllowedRepositoryIDs: append([]string(nil), resolution.AllowedRepositoryIDs...),
	}, true, nil
}

// postgresExternalSubjectLookup adapts the Postgres OIDC login store's
// subject-active check to the refresher's ExternalSubjectLookup.
type postgresExternalSubjectLookup struct {
	store *pgstatus.OIDCLoginStore
}

func (l *postgresExternalSubjectLookup) ExternalSubjectActive(
	ctx context.Context,
	providerConfigID string,
	subjectIDHash string,
) (bool, error) {
	return l.store.ExternalSubjectActive(ctx, providerConfigID, subjectIDHash)
}
