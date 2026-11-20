// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/githublogin"
	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// newCoreAdminProviderConfigMutationHandler wires the DB-backed
// provider-config admin writes. A nil keyring fails secret writes closed;
// a nil tester fails enable/test-connection closed.
func newCoreAdminProviderConfigMutationHandler(
	database *sql.DB,
	governanceAudit query.GovernanceAuditSummaryReader,
	keyring *secretcrypto.Keyring,
	tester query.ProviderConfigConnectionTester,
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
	logger *slog.Logger,
) *query.AdminProviderConfigMutationHandler {
	handler := &query.AdminProviderConfigMutationHandler{
		Audit:  coreRecoveryAuditAppender(governanceAudit),
		Tester: tester,
	}
	if store := newCoreProviderConfigMutationAdapter(database, keyring, oidcLoginHandler, samlHandler); store != nil {
		handler.Store = store
	}
	if readStore := newCoreProviderConfigReadAdapter(database, oidcLoginHandler, samlHandler, logger); readStore != nil {
		handler.ReadStore = readStore
	}
	return handler
}

// coreProviderConfigMutationAdapter translates the provider-config mutation
// contract into the Postgres identity subject store. Env-registered ids are
// never editable through this API (create is the deliberate exception: an
// admin may intentionally supply an env id to create a shadow row).
type coreProviderConfigMutationAdapter struct {
	store          *pgstatus.IdentitySubjectStore
	envProviderIDs map[string]struct{}
	now            func() time.Time
}

func newCoreProviderConfigMutationAdapter(
	rawDB *sql.DB,
	keyring *secretcrypto.Keyring,
	oidcLoginHandler *query.OIDCLoginHandler,
	samlHandler *query.SAMLHandler,
) *coreProviderConfigMutationAdapter {
	if rawDB == nil {
		return nil
	}
	store := pgstatus.NewIdentitySubjectStore(db.ExecQueryer(pgstatus.SQLDB{DB: rawDB}))
	store.SetProviderSecretKeyring(keyring)
	return &coreProviderConfigMutationAdapter{
		store:          store,
		envProviderIDs: coreEnvRegisteredProviderIDs(oidcLoginHandler, samlHandler),
		now:            time.Now,
	}
}

// rejectIfCoreEnvManaged rejects mutations against env/file-registered ids.
func (a *coreProviderConfigMutationAdapter) rejectIfCoreEnvManaged(providerConfigID string) error {
	if _, envManaged := a.envProviderIDs[providerConfigID]; envManaged {
		return query.ErrAdminProviderConfigManagedByEnvironment
	}
	return nil
}

// CreateProviderConfig creates a provider config revision.
func (a *coreProviderConfigMutationAdapter) CreateProviderConfig(
	ctx context.Context,
	req query.AdminProviderConfigCreateRequest,
) (query.AdminProviderConfigWriteResult, error) {
	result, err := a.store.CreateProviderConfig(ctx, pgstatus.ProviderConfigCreate{
		ProviderConfigID:  req.ProviderConfigID,
		TenantID:          req.TenantID,
		ProviderKind:      req.ProviderKind,
		ProviderKeyHash:   req.ProviderKeyHash,
		IssuerHash:        req.IssuerHash,
		ClientIDHash:      req.ClientIDHash,
		MetadataURLHash:   req.MetadataURLHash,
		EntityIDHash:      req.EntityIDHash,
		RevisionID:        req.RevisionID,
		Configuration:     req.Configuration,
		ConfigurationHash: req.ConfigurationHash,
		MetadataHash:      req.MetadataHash,
		PlaintextSecret:   req.PlaintextSecret,
		Now:               req.Now,
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapCoreProviderConfigError(err)
	}
	return toCoreAdminWriteResult(result), nil
}

// UpdateProviderConfig writes a new revision for an existing config.
func (a *coreProviderConfigMutationAdapter) UpdateProviderConfig(
	ctx context.Context,
	req query.AdminProviderConfigUpdateRequest,
) (query.AdminProviderConfigWriteResult, error) {
	if err := a.rejectIfCoreEnvManaged(req.ProviderConfigID); err != nil {
		return query.AdminProviderConfigWriteResult{}, err
	}
	result, err := a.store.UpdateProviderConfig(ctx, pgstatus.ProviderConfigUpdate{
		ProviderConfigID:  req.ProviderConfigID,
		TenantID:          req.TenantID,
		ProviderKind:      req.ProviderKind,
		RevisionID:        req.RevisionID,
		Configuration:     req.Configuration,
		ConfigurationHash: req.ConfigurationHash,
		MetadataHash:      req.MetadataHash,
		PlaintextSecret:   req.PlaintextSecret,
		Now:               req.Now,
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapCoreProviderConfigError(err)
	}
	return toCoreAdminWriteResult(result), nil
}

// RevertProviderConfig reactivates a prior revision.
func (a *coreProviderConfigMutationAdapter) RevertProviderConfig(
	ctx context.Context,
	req query.AdminProviderConfigRevertRequest,
) (query.AdminProviderConfigWriteResult, error) {
	if err := a.rejectIfCoreEnvManaged(req.ProviderConfigID); err != nil {
		return query.AdminProviderConfigWriteResult{}, err
	}
	result, err := a.store.RevertProviderConfig(ctx, pgstatus.ProviderConfigRevert{
		ProviderConfigID: req.ProviderConfigID,
		TenantID:         req.TenantID,
		TargetRevisionID: req.TargetRevisionID,
		Now:              req.Now,
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapCoreProviderConfigError(err)
	}
	return toCoreAdminWriteResult(result), nil
}

// EnableProviderConfig activates a provider config after a tested revision.
func (a *coreProviderConfigMutationAdapter) EnableProviderConfig(
	ctx context.Context,
	providerConfigID, tenantID, expectedActiveRevisionID string,
) (query.AdminProviderConfigWriteResult, error) {
	if err := a.rejectIfCoreEnvManaged(providerConfigID); err != nil {
		return query.AdminProviderConfigWriteResult{}, err
	}
	result, err := a.store.EnableProviderConfig(ctx, pgstatus.ProviderConfigEnable{
		ProviderConfigID:         providerConfigID,
		TenantID:                 tenantID,
		ExpectedActiveRevisionID: expectedActiveRevisionID,
		Now:                      a.clock().UTC(),
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapCoreProviderConfigError(err)
	}
	return toCoreAdminWriteResult(result), nil
}

// DisableProviderConfig deactivates a provider config.
func (a *coreProviderConfigMutationAdapter) DisableProviderConfig(
	ctx context.Context,
	providerConfigID, tenantID string,
) (query.AdminProviderConfigWriteResult, error) {
	if err := a.rejectIfCoreEnvManaged(providerConfigID); err != nil {
		return query.AdminProviderConfigWriteResult{}, err
	}
	result, err := a.store.DisableProviderConfig(ctx, pgstatus.ProviderConfigDisable{
		ProviderConfigID: providerConfigID, TenantID: tenantID, Now: a.clock().UTC(),
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapCoreProviderConfigError(err)
	}
	return toCoreAdminWriteResult(result), nil
}

// clock returns the adapter's injectable clock, defaulting to time.Now.
func (a *coreProviderConfigMutationAdapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func toCoreAdminWriteResult(result pgstatus.ProviderConfigWriteResult) query.AdminProviderConfigWriteResult {
	return query.AdminProviderConfigWriteResult{
		ProviderConfigID: result.ProviderConfigID,
		RevisionID:       result.RevisionID,
		Status:           result.Status,
		Found:            result.Found,
		Changed:          result.Changed,
	}
}

// mapCoreProviderConfigError translates storage sentinels to query-layer
// equivalents so package query never imports storage/postgres for them.
func mapCoreProviderConfigError(err error) error {
	switch {
	case errors.Is(err, pgstatus.ErrProviderConfigDuplicateKey):
		return query.ErrAdminProviderConfigDuplicateKey
	case errors.Is(err, pgstatus.ErrProviderSecretKeyringUnavailable):
		return query.ErrAdminProviderConfigKeyringUnavailable
	case errors.Is(err, pgstatus.ErrProviderConfigRevisionNotFound):
		return query.ErrAdminProviderConfigRevisionNotFound
	case errors.Is(err, pgstatus.ErrProviderConfigKindMismatch):
		return query.ErrAdminProviderConfigKindMismatch
	case errors.Is(err, pgstatus.ErrProviderConfigRevisionChanged):
		return query.ErrAdminProviderConfigRevisionChanged
	default:
		return err
	}
}

// coreProviderConfigConnectionTester runs the bounded safe portion of a
// provider's connection test. It reads sealed ciphertext (never plaintext)
// and hands it to the protocol testers; this type never calls query.Open itself.
type coreProviderConfigConnectionTester struct {
	store   *pgstatus.IdentitySubjectStore
	keyring *secretcrypto.Keyring
}

// newCoreProviderConfigConnectionTester builds the tester, or nil without a
// database (enable/test-connection then fail closed 503).
func newCoreProviderConfigConnectionTester(
	rawDB *sql.DB,
	keyring *secretcrypto.Keyring,
) query.ProviderConfigConnectionTester {
	if rawDB == nil {
		return nil
	}
	return &coreProviderConfigConnectionTester{
		store:   pgstatus.NewIdentitySubjectStore(db.ExecQueryer(pgstatus.SQLDB{DB: rawDB})),
		keyring: keyring,
	}
}

// TestProviderConnection tests one provider config's connectivity.
func (t *coreProviderConfigConnectionTester) TestProviderConnection(
	ctx context.Context,
	providerConfigID, tenantID string,
) (query.AdminProviderConfigConnectionTestResult, error) {
	material, found, err := t.store.GetProviderConfigConnectionTestMaterial(ctx, providerConfigID, tenantID)
	if err != nil {
		return query.AdminProviderConfigConnectionTestResult{}, err
	}
	if !found || material.SealedSecret == "" {
		return query.AdminProviderConfigConnectionTestResult{OK: false, Detail: "provider config has no active secret to test"}, nil
	}
	if t.keyring == nil {
		return query.AdminProviderConfigConnectionTestResult{}, fmt.Errorf("provider connection test: encryption key is not configured")
	}
	switch material.ProviderKind {
	case "external_oidc":
		var cfg struct {
			Issuer string `json:"issuer"`
		}
		_ = json.Unmarshal([]byte(material.Configuration), &cfg)
		result, err := oidclogin.TestConnection(ctx, t.keyring, providerConfigID, material.RevisionID, cfg.Issuer, material.SealedSecret)
		if err != nil {
			return query.AdminProviderConfigConnectionTestResult{}, err
		}
		return query.AdminProviderConfigConnectionTestResult{OK: result.OK, Detail: result.Detail, RevisionID: material.RevisionID}, nil
	case "external_saml":
		var cfg struct {
			EntityID    string `json:"entity_id"`
			MetadataURL string `json:"metadata_url"`
			MetadataXML string `json:"metadata_xml"`
		}
		_ = json.Unmarshal([]byte(material.Configuration), &cfg)
		result, err := samlauth.TestConnection(ctx, t.keyring, providerConfigID, material.RevisionID, cfg.EntityID, cfg.MetadataURL, cfg.MetadataXML, material.SealedSecret)
		if err != nil {
			return query.AdminProviderConfigConnectionTestResult{}, err
		}
		return query.AdminProviderConfigConnectionTestResult{OK: result.OK, Detail: result.Detail, RevisionID: material.RevisionID}, nil
	case "external_github":
		apiBase := coreGitHubConnectionTestAPIBase(material.Configuration)
		result, err := githublogin.TestConnection(ctx, t.keyring, providerConfigID, material.RevisionID, apiBase, material.SealedSecret)
		if err != nil {
			return query.AdminProviderConfigConnectionTestResult{}, err
		}
		return query.AdminProviderConfigConnectionTestResult{OK: result.OK, Detail: result.Detail, RevisionID: material.RevisionID}, nil
	default:
		return query.AdminProviderConfigConnectionTestResult{OK: false, Detail: "unknown provider kind"}, nil
	}
}

// coreGitHubConnectionTestAPIBase derives the REST API base URL the login
// flow actually calls, so a GHES provider is probed at <base_url>/api/v3,
// not the api.github.com default.
func coreGitHubConnectionTestAPIBase(configurationJSON string) string {
	var cfg struct {
		BaseURL    string `json:"base_url"`
		APIBaseURL string `json:"api_base_url"`
	}
	_ = json.Unmarshal([]byte(configurationJSON), &cfg)
	return githublogin.EffectiveAPIBaseURL(cfg.BaseURL, cfg.APIBaseURL)
}
