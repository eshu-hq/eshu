// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/provider/config"
)

// This file holds the single exported, test-only copies of the admin
// provider-config doubles (#7715). They name only the neutral leaf
// internal/query/querycontract/provider/config, so both consumers — the
// root query tests and the provider-config family's internal tests —
// import them without an import cycle. Each consumer keeps a small
// unexported adapter (original field names) that delegates here, the
// shape established by FakeGovernanceAuditAppender; the dispatch rules
// exist once, here.

// FakeAdminProviderConfigReadStore is a tenant-keyed read double for the
// admin provider-config routes. It serves details by provider_config_id,
// lists by tenant id, and lists revisions per provider config; ForceErr
// makes every method fail with it, for the error-path assertions.
type FakeAdminProviderConfigReadStore struct {
	// Details maps provider_config_id to its detail row.
	Details map[string]config.Detail
	// List maps tenant id to the detail rows that tenant sees.
	List map[string][]config.Detail
	// Revisions maps provider_config_id to its revision history rows.
	Revisions map[string][]config.RevisionItem
	// ForceErr, when non-nil, is returned by every method.
	ForceErr error
}

// GetProviderConfigDetail returns the stored detail for providerConfigID,
// or reports not-found.
func (f *FakeAdminProviderConfigReadStore) GetProviderConfigDetail(_ context.Context, providerConfigID, _ string) (config.Detail, bool, error) {
	if f.ForceErr != nil {
		return config.Detail{}, false, f.ForceErr
	}
	detail, ok := f.Details[providerConfigID]
	return detail, ok, nil
}

// ListProviderConfigDetails returns the tenant's detail rows.
func (f *FakeAdminProviderConfigReadStore) ListProviderConfigDetails(_ context.Context, tenantID string) ([]config.Detail, error) {
	if f.ForceErr != nil {
		return nil, f.ForceErr
	}
	return f.List[tenantID], nil
}

// ListProviderConfigRevisions returns the revision history rows for
// providerConfigID.
func (f *FakeAdminProviderConfigReadStore) ListProviderConfigRevisions(_ context.Context, providerConfigID, _ string) ([]config.RevisionItem, error) {
	if f.ForceErr != nil {
		return nil, f.ForceErr
	}
	return f.Revisions[providerConfigID], nil
}

// FakeAdminProviderConfigMutationStore records the request it was asked to
// perform and returns a canned Result; ForceErr overrides with an error.
type FakeAdminProviderConfigMutationStore struct {
	// GotCreate/GotUpdate/GotRevert hold the last request per method.
	GotCreate config.CreateRequest
	GotUpdate config.UpdateRequest
	GotRevert config.RevertRequest
	// GotEnableID/GotEnableTenant/GotEnableExpectedRevisionID record the
	// last EnableProviderConfig call.
	GotEnableID, GotEnableTenant, GotEnableExpectedRevisionID string
	// GotDisableID/GotDisableTenant record the last DisableProviderConfig call.
	GotDisableID, GotDisableTenant string
	// Result is the canned write result.
	Result config.WriteResult
	// ForceErr, when non-nil, is returned by every method.
	ForceErr error
}

// CreateProviderConfig records the create request and returns the canned result.
func (f *FakeAdminProviderConfigMutationStore) CreateProviderConfig(_ context.Context, req config.CreateRequest) (config.WriteResult, error) {
	f.GotCreate = req
	if f.ForceErr != nil {
		return config.WriteResult{}, f.ForceErr
	}
	return f.Result, nil
}

// UpdateProviderConfig records the update request and returns the canned result.
func (f *FakeAdminProviderConfigMutationStore) UpdateProviderConfig(_ context.Context, req config.UpdateRequest) (config.WriteResult, error) {
	f.GotUpdate = req
	if f.ForceErr != nil {
		return config.WriteResult{}, f.ForceErr
	}
	return f.Result, nil
}

// RevertProviderConfig records the revert request and returns the canned result.
func (f *FakeAdminProviderConfigMutationStore) RevertProviderConfig(_ context.Context, req config.RevertRequest) (config.WriteResult, error) {
	f.GotRevert = req
	if f.ForceErr != nil {
		return config.WriteResult{}, f.ForceErr
	}
	return f.Result, nil
}

// EnableProviderConfig records the compare-and-swap guard triplet and
// returns the canned result.
func (f *FakeAdminProviderConfigMutationStore) EnableProviderConfig(_ context.Context, providerConfigID, tenantID, expectedActiveRevisionID string) (config.WriteResult, error) {
	f.GotEnableID, f.GotEnableTenant, f.GotEnableExpectedRevisionID = providerConfigID, tenantID, expectedActiveRevisionID
	if f.ForceErr != nil {
		return config.WriteResult{}, f.ForceErr
	}
	return f.Result, nil
}

// DisableProviderConfig records the disable call and returns the canned result.
func (f *FakeAdminProviderConfigMutationStore) DisableProviderConfig(_ context.Context, providerConfigID, tenantID string) (config.WriteResult, error) {
	f.GotDisableID, f.GotDisableTenant = providerConfigID, tenantID
	if f.ForceErr != nil {
		return config.WriteResult{}, f.ForceErr
	}
	return f.Result, nil
}

// FakeProviderConfigConnectionTester scripts the connection test: it records
// the last call and returns Result, or ForceErr when set.
type FakeProviderConfigConnectionTester struct {
	// GotProviderConfigID/GotTenantID record the last test call.
	GotProviderConfigID, GotTenantID string
	// Result is the canned connection-test outcome.
	Result config.ConnectionTestResult
	// ForceErr, when non-nil, is returned by the test call.
	ForceErr error
}

// TestProviderConnection records the call and returns the scripted result.
func (f *FakeProviderConfigConnectionTester) TestProviderConnection(_ context.Context, providerConfigID, tenantID string) (config.ConnectionTestResult, error) {
	f.GotProviderConfigID, f.GotTenantID = providerConfigID, tenantID
	if f.ForceErr != nil {
		return config.ConnectionTestResult{}, f.ForceErr
	}
	return f.Result, nil
}
