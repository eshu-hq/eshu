// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// decodeCoreProviderConfiguration parses a stored configuration blob,
// warning once on malformed JSON instead of failing the whole read.
func decodeCoreProviderConfiguration(
	ctx context.Context,
	logger *slog.Logger,
	providerConfigID, configurationJSON string,
) map[string]any {
	if configurationJSON == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(configurationJSON), &out); err != nil {
		if logger != nil {
			logger.WarnContext(
				ctx,
				"provider config: stored configuration failed to parse as JSON",
				"provider_config_id", providerConfigID,
				"err", err,
			)
		}
		return nil
	}
	return out
}

// newCoreAdminProviderConfigReadHandler wires the DB-backed provider-config
// admin reads. Nil-safe: a nil database yields a store-less handler.
func newCoreAdminProviderConfigReadHandler(
	database *sql.DB,
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
	logger *slog.Logger,
) *query.AdminProviderConfigReadHandler {
	handler := &query.AdminProviderConfigReadHandler{}
	if store := newCoreProviderConfigReadAdapter(database, oidcLoginHandler, samlHandler, logger); store != nil {
		handler.Store = store
	}
	return handler
}

// coreProviderConfigReadAdapter combines Postgres-backed CRUD configs with
// env-file-registered OIDC/SAML providers (env wins; shadowed DB rows read
// back read-only with ShadowedByEnvironment).
type coreProviderConfigReadAdapter struct {
	store              *pgstatus.IdentitySubjectStore
	envProviderIDs     map[string]struct{}
	envOIDCProviders   []query.OIDCRegisteredProvider
	envSAMLProviderIDs []string
	logger             *slog.Logger
}

func newCoreProviderConfigReadAdapter(
	rawDB *sql.DB,
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
	logger *slog.Logger,
) *coreProviderConfigReadAdapter {
	if rawDB == nil {
		return nil
	}
	var envOIDCProviders []query.OIDCRegisteredProvider
	if oidcLoginHandler != nil {
		envOIDCProviders = oidcLoginHandler.RegisteredProviders()
	}
	var envSAMLProviderIDs []string
	if samlHandler != nil {
		envSAMLProviderIDs = samlHandler.RegisteredProviderIDs()
	}
	return &coreProviderConfigReadAdapter{
		store:              pgstatus.NewIdentitySubjectStore(db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})),
		envProviderIDs:     coreEnvRegisteredProviderIDs(oidcLoginHandler, samlHandler),
		envOIDCProviders:   envOIDCProviders,
		envSAMLProviderIDs: envSAMLProviderIDs,
		logger:             logger,
	}
}

// coreEnvRegisteredProviderIDs collects every env/file-registered
// provider_config_id for the shadow-detection check.
func coreEnvRegisteredProviderIDs(
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
) map[string]struct{} {
	ids := make(map[string]struct{})
	if oidcLoginHandler != nil {
		for _, p := range oidcLoginHandler.RegisteredProviders() {
			ids[p.ProviderConfigID] = struct{}{}
		}
	}
	if samlHandler != nil {
		for _, id := range samlHandler.RegisteredProviderIDs() {
			ids[id] = struct{}{}
		}
	}
	return ids
}

// GetProviderConfigDetail returns one provider config's admin view.
func (a *coreProviderConfigReadAdapter) GetProviderConfigDetail(
	ctx context.Context,
	providerConfigID, tenantID string,
) (query.AdminProviderConfigDetail, bool, error) {
	detail, found, err := a.store.GetProviderConfigDetail(ctx, providerConfigID, tenantID)
	if err != nil || !found {
		return query.AdminProviderConfigDetail{}, found, err
	}
	return a.toAdminDetail(ctx, detail), true, nil
}

// ListProviderConfigDetails lists configs plus synthesized env-only entries.
func (a *coreProviderConfigReadAdapter) ListProviderConfigDetails(
	ctx context.Context,
	tenantID string,
) ([]query.AdminProviderConfigDetail, error) {
	items, err := a.store.ListProviderConfigs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]query.AdminProviderConfigDetail, 0, len(items))
	for _, item := range items {
		seen[item.ProviderConfigID] = struct{}{}
		out = append(out, a.toAdminDetail(ctx, item))
	}
	for _, p := range a.envOIDCProviders {
		if p.TenantID != tenantID {
			continue
		}
		if _, alreadyListed := seen[p.ProviderConfigID]; alreadyListed {
			continue
		}
		seen[p.ProviderConfigID] = struct{}{}
		out = append(out, query.AdminProviderConfigDetail{
			ProviderConfigID: p.ProviderConfigID,
			ProviderKind:     "oidc",
			Status:           "active",
			ManagedBy:        "environment",
		})
	}
	for _, providerID := range a.envSAMLProviderIDs {
		if _, alreadyListed := seen[providerID]; alreadyListed {
			continue
		}
		hasDBRow, err := a.store.HasActiveSAMLProviderConfig(ctx, providerID)
		if err != nil {
			return nil, fmt.Errorf("check active saml provider config for env-only synthesis: %w", err)
		}
		if hasDBRow {
			continue
		}
		seen[providerID] = struct{}{}
		out = append(out, query.AdminProviderConfigDetail{
			ProviderConfigID: providerID,
			ProviderKind:     "saml",
			Status:           "active",
			ManagedBy:        "environment",
		})
	}
	return out, nil
}

// ListProviderConfigRevisions lists one config's revisions.
func (a *coreProviderConfigReadAdapter) ListProviderConfigRevisions(
	ctx context.Context,
	providerConfigID, tenantID string,
) ([]query.AdminProviderConfigRevisionItem, error) {
	items, err := a.store.ListProviderConfigRevisions(ctx, providerConfigID, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminProviderConfigRevisionItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminProviderConfigRevisionItem{
			RevisionID:   item.RevisionID,
			Status:       item.Status,
			HasSecret:    item.HasSecret,
			CreatedAt:    item.CreatedAt,
			ActivatedAt:  item.ActivatedAt,
			SupersededAt: item.SupersededAt,
		})
	}
	return out, nil
}

func (a *coreProviderConfigReadAdapter) toAdminDetail(
	ctx context.Context,
	detail pgstatus.ProviderConfigDetail,
) query.AdminProviderConfigDetail {
	_, shadowed := a.envProviderIDs[detail.ProviderConfigID]
	managedBy := "database"
	if shadowed {
		managedBy = "environment"
	}
	return query.AdminProviderConfigDetail{
		ProviderConfigID:      detail.ProviderConfigID,
		ProviderKind:          detail.ProviderKind,
		Status:                detail.Status,
		ActiveRevisionID:      detail.ActiveRevisionID,
		Configuration:         decodeCoreProviderConfiguration(ctx, a.logger, detail.ProviderConfigID, detail.Configuration),
		HasSecret:             detail.HasSecret,
		SecretFingerprint:     detail.SecretFingerprint,
		SecretKeyID:           detail.SecretKeyID,
		ShadowedByEnvironment: shadowed,
		ManagedBy:             managedBy,
		CreatedAt:             detail.CreatedAt,
		UpdatedAt:             detail.UpdatedAt,
	}
}
