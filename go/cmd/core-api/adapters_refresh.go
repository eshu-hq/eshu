// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// OIDCSessionRefreshWorker runs bounded OIDC active-session revocation
// refresh passes on a fixed cadence. Main runs it with the process context;
// it stops when that context is done.
type OIDCSessionRefreshWorker struct {
	refresher *oidclogin.Refresher
	interval  time.Duration
	logger    *slog.Logger

	passes              metric.Int64Counter
	scanned             metric.Int64Counter
	revoked             metric.Int64Counter
	refreshed           metric.Int64Counter
	providerUnavailable metric.Int64Counter
}

// CoreOIDCSessionRefreshWorker builds the refresh worker, or returns nil
// when the worker is disabled or Postgres is unavailable. Gated on
// ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED.
func CoreOIDCSessionRefreshWorker(
	getenv func(string) string,
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (*OIDCSessionRefreshWorker, error) {
	if !coreBoolEnv(coreEnvOrEmpty(getenv, "ESHU_AUTH_OIDC_SESSION_REFRESH_ENABLED")) {
		return nil, nil
	}
	if rawDB == nil {
		return nil, fmt.Errorf("postgres is required for oidc session refresh")
	}
	interval := time.Minute
	if raw := coreEnvOrEmpty(getenv, "ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL: %w", err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("ESHU_AUTH_OIDC_SESSION_REFRESH_INTERVAL must be positive")
		}
		interval = parsed
	}
	batchSize := oidclogin.DefaultRefreshBatchSize
	if raw := coreEnvOrEmpty(getenv, "ESHU_AUTH_OIDC_SESSION_REFRESH_BATCH_SIZE"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("ESHU_AUTH_OIDC_SESSION_REFRESH_BATCH_SIZE must be a positive integer")
		}
		batchSize = parsed
	}
	window := query.DefaultOIDCSessionRefreshWindow
	if raw := coreEnvOrEmpty(getenv, coreEnvAuthOIDCSessionRefreshWindow); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("ESHU_AUTH_OIDC_SESSION_REFRESH_WINDOW must be a positive duration")
		}
		window = parsed
	}
	sessionDB := coreInstrumentedDB(rawDB, instruments, "browser_sessions")
	refresher := oidclogin.NewRefresher(
		&coreSessionRefreshStore{store: pgstatus.NewBrowserSessionStore(sessionDB)},
		&coreRoleGrantResolver{store: pgstatus.NewOIDCLoginStore(coreInstrumentedDB(rawDB, instruments, "identity_oidc_login"))},
		oidclogin.RefreshConfig{
			BatchSize:     batchSize,
			Window:        window,
			SubjectLookup: &coreExternalSubjectLookup{store: pgstatus.NewOIDCLoginStore(coreInstrumentedDB(rawDB, instruments, "identity_oidc_login"))},
		},
	)
	worker := &OIDCSessionRefreshWorker{refresher: refresher, interval: interval, logger: logger}
	if err := worker.registerInstruments(); err != nil {
		return nil, err
	}
	return worker, nil
}

func (w *OIDCSessionRefreshWorker) registerInstruments() error {
	meter := otel.Meter(telemetry.DefaultSignalName)
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
		metric.WithDescription("OIDC sessions extended by active-session refresh."),
	); err != nil {
		return fmt.Errorf("register oidc refresh extended counter: %w", err)
	}
	if w.providerUnavailable, err = meter.Int64Counter(
		"eshu_auth_oidc_session_refresh_provider_unavailable_total",
		metric.WithDescription("OIDC sessions skipped because the provider was unavailable."),
	); err != nil {
		return fmt.Errorf("register oidc refresh provider-unavailable counter: %w", err)
	}
	return nil
}

// Run executes refresh passes until ctx is done.
func (w *OIDCSessionRefreshWorker) Run(ctx context.Context) {
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

func (w *OIDCSessionRefreshWorker) runPass(ctx context.Context) {
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

func (w *OIDCSessionRefreshWorker) recordOutcome(ctx context.Context, outcome oidclogin.RefreshOutcome) {
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

func coreBoolEnv(raw string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && parsed
}

// coreSessionRefreshStore adapts the Postgres browser session store to the
// refresher's bounded read/write surface.
type coreSessionRefreshStore struct {
	store *pgstatus.BrowserSessionStore
}

// ListStaleSessions lists stale external sessions for refresh.
func (a *coreSessionRefreshStore) ListStaleSessions(
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
			ExternalGroupHashes:      append([]string(nil), record.ExternalGroupHashes...),
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

// RevokeSession revokes one session.
func (a *coreSessionRefreshStore) RevokeSession(
	ctx context.Context,
	sessionHash string,
	revokedAt time.Time,
) error {
	return a.store.RevokeSession(ctx, sessionHash, revokedAt)
}

// UpdateSessionAuthProof refreshes one session's external auth proof.
func (a *coreSessionRefreshStore) UpdateSessionAuthProof(
	ctx context.Context,
	update oidclogin.SessionAuthProofUpdate,
) error {
	return a.store.UpdateOIDCSessionAuthProof(ctx, pgstatus.OIDCSessionAuthProofUpdate{
		SessionHash:                  update.SessionHash,
		ExternalAuthValidatedAt:      update.ExternalAuthValidatedAt,
		ExternalAuthStaleAfter:       update.ExternalAuthStaleAfter,
		PolicyRevisionHash:           update.PolicyRevisionHash,
		ExternalGroupHashes:          append([]string(nil), update.ExternalGroupHashes...),
		RoleIDs:                      append([]string(nil), update.RoleIDs...),
		AllScopes:                    update.AllScopes,
		AllowedScopeIDs:              append([]string(nil), update.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), update.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), update.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), update.AllowedPermissionDataClasses...),
		UpdatedAt:                    update.UpdatedAt,
	})
}

// coreRoleGrantResolver adapts the OIDC login store's role re-resolution to
// the refresher.
type coreRoleGrantResolver struct {
	store *pgstatus.OIDCLoginStore
}

// ResolveGroupGrants resolves group grants, falling back to active role
// grants when no group hashes are present.
func (r *coreRoleGrantResolver) ResolveGroupGrants(
	ctx context.Context,
	grantQuery oidclogin.GrantQuery,
) (oidclogin.GrantResolution, bool, error) {
	if len(grantQuery.GroupHashes) > 0 {
		resolution, ok, err := r.store.ResolveGroupRoleGrants(ctx, pgstatus.OIDCGroupGrantQuery{
			ProviderConfigID:    grantQuery.ProviderConfigID,
			TenantID:            grantQuery.TenantID,
			WorkspaceID:         grantQuery.WorkspaceID,
			ExternalGroupHashes: append([]string(nil), grantQuery.GroupHashes...),
			AsOf:                grantQuery.AsOf,
		})
		if err != nil || !ok {
			return oidclogin.GrantResolution{}, ok, err
		}
		return oidclogin.GrantResolution{
			RoleIDs:                      append([]string(nil), resolution.RoleIDs...),
			PolicyRevisionHash:           resolution.PolicyRevisionHash,
			PermissionCatalogEnforced:    true,
			AllowedScopeIDs:              append([]string(nil), resolution.AllowedScopeIDs...),
			AllowedRepositoryIDs:         append([]string(nil), resolution.AllowedRepositoryIDs...),
			AllowedPermissionFeatures:    append([]string(nil), resolution.AllowedPermissionFeatures...),
			AllowedPermissionDataClasses: append([]string(nil), resolution.AllowedPermissionDataClasses...),
		}, true, nil
	}
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
		RoleIDs:                      append([]string(nil), resolution.RoleIDs...),
		PolicyRevisionHash:           resolution.PolicyRevisionHash,
		PermissionCatalogEnforced:    true,
		AllowedScopeIDs:              append([]string(nil), resolution.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), resolution.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), resolution.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), resolution.AllowedPermissionDataClasses...),
	}, true, nil
}

// coreExternalSubjectLookup adapts the OIDC login store's subject-active
// check to the refresher.
type coreExternalSubjectLookup struct {
	store *pgstatus.OIDCLoginStore
}

// ExternalSubjectActive reports whether the external subject is still active.
func (l *coreExternalSubjectLookup) ExternalSubjectActive(
	ctx context.Context,
	providerConfigID string,
	subjectIDHash string,
) (bool, error) {
	return l.store.ExternalSubjectActive(ctx, providerConfigID, subjectIDHash)
}
