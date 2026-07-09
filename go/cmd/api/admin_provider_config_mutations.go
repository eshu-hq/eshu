// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// newAdminProviderConfigMutationHandler wires the DB-backed provider-config
// admin write endpoints (#4966). keyring may be nil (no DEK configured); a
// write carrying a secret then fails closed with a 503, never silently
// unsealed. tester may be nil; enable and test-connection then fail closed
// with a 503 rather than skipping the connection-test gate.
func newAdminProviderConfigMutationHandler(
	db *sql.DB,
	governanceAudit query.GovernanceAuditSummaryReader,
	keyring *secretcrypto.Keyring,
	tester query.ProviderConfigConnectionTester,
) *query.AdminProviderConfigMutationHandler {
	handler := &query.AdminProviderConfigMutationHandler{
		Audit:  adminRecoveryAuditAppender(governanceAudit),
		Tester: tester,
	}
	if store := newProviderConfigMutationAdapter(db, keyring); store != nil {
		handler.Store = store
	}
	return handler
}

// providerConfigMutationAdapter translates the query-layer provider-config
// mutation contract into the Postgres IdentitySubjectStore, mapping the
// storage package's sentinel errors to the query package's own sentinels so
// go/internal/query never imports storage/postgres (or, transitively,
// secretcrypto) to inspect a specific error — see
// admin_provider_config_mutations.go's sentinel doc comments in the query
// package.
type providerConfigMutationAdapter struct {
	store *pgstatus.IdentitySubjectStore
	// now is the injectable clock for Enable/Disable, which build their own
	// timestamp (Create/Update/Revert receive Now from the query-layer
	// request instead). Defaults to time.Now in
	// newProviderConfigMutationAdapter; tests override it for determinism.
	now func() time.Time
}

func newProviderConfigMutationAdapter(db *sql.DB, keyring *secretcrypto.Keyring) *providerConfigMutationAdapter {
	if db == nil {
		return nil
	}
	store := pgstatus.NewIdentitySubjectStore(pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db}))
	store.SetProviderSecretKeyring(keyring)
	return &providerConfigMutationAdapter{store: store, now: time.Now}
}

func (a *providerConfigMutationAdapter) CreateProviderConfig(
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
		return query.AdminProviderConfigWriteResult{}, mapProviderConfigError(err)
	}
	return toAdminWriteResult(result), nil
}

func (a *providerConfigMutationAdapter) UpdateProviderConfig(
	ctx context.Context,
	req query.AdminProviderConfigUpdateRequest,
) (query.AdminProviderConfigWriteResult, error) {
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
		return query.AdminProviderConfigWriteResult{}, mapProviderConfigError(err)
	}
	return toAdminWriteResult(result), nil
}

func (a *providerConfigMutationAdapter) RevertProviderConfig(
	ctx context.Context,
	req query.AdminProviderConfigRevertRequest,
) (query.AdminProviderConfigWriteResult, error) {
	result, err := a.store.RevertProviderConfig(ctx, pgstatus.ProviderConfigRevert{
		ProviderConfigID: req.ProviderConfigID,
		TenantID:         req.TenantID,
		TargetRevisionID: req.TargetRevisionID,
		Now:              req.Now,
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapProviderConfigError(err)
	}
	return toAdminWriteResult(result), nil
}

func (a *providerConfigMutationAdapter) EnableProviderConfig(
	ctx context.Context,
	providerConfigID, tenantID, expectedActiveRevisionID string,
) (query.AdminProviderConfigWriteResult, error) {
	result, err := a.store.EnableProviderConfig(ctx, pgstatus.ProviderConfigEnable{
		ProviderConfigID:         providerConfigID,
		TenantID:                 tenantID,
		ExpectedActiveRevisionID: expectedActiveRevisionID,
		Now:                      a.clock().UTC(),
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapProviderConfigError(err)
	}
	return toAdminWriteResult(result), nil
}

func (a *providerConfigMutationAdapter) DisableProviderConfig(
	ctx context.Context,
	providerConfigID, tenantID string,
) (query.AdminProviderConfigWriteResult, error) {
	result, err := a.store.DisableProviderConfig(ctx, pgstatus.ProviderConfigDisable{
		ProviderConfigID: providerConfigID, TenantID: tenantID, Now: a.clock().UTC(),
	})
	if err != nil {
		return query.AdminProviderConfigWriteResult{}, mapProviderConfigError(err)
	}
	return toAdminWriteResult(result), nil
}

// clock returns the adapter's injectable clock, defaulting to time.Now for
// an adapter constructed without one set explicitly (e.g. a zero-value
// adapter built directly in a test that does not go through
// newProviderConfigMutationAdapter).
func (a *providerConfigMutationAdapter) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func toAdminWriteResult(result pgstatus.ProviderConfigWriteResult) query.AdminProviderConfigWriteResult {
	return query.AdminProviderConfigWriteResult{
		ProviderConfigID: result.ProviderConfigID,
		RevisionID:       result.RevisionID,
		Status:           result.Status,
		Found:            result.Found,
		Changed:          result.Changed,
	}
}

func mapProviderConfigError(err error) error {
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
