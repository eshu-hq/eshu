// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeOIDCProviderListerService implements query.OIDCLoginService plus
// query.OIDCProviderLister so newAdminProviderConfigReadHandler's
// env-registered-provider detection can be tested without a real OIDC config
// file.
type fakeOIDCProviderListerService struct {
	providers []query.OIDCRegisteredProvider
}

func (f *fakeOIDCProviderListerService) StartOIDCLogin(context.Context, query.OIDCLoginStartRequest) (query.OIDCLoginStartResponse, error) {
	return query.OIDCLoginStartResponse{}, nil
}

func (f *fakeOIDCProviderListerService) CompleteOIDCLogin(context.Context, query.OIDCLoginCompleteRequest) (query.OIDCLoginCompleteResponse, error) {
	return query.OIDCLoginCompleteResponse{}, nil
}

func (f *fakeOIDCProviderListerService) ListOIDCProviderIDs() []query.OIDCRegisteredProvider {
	return f.providers
}

// fakeSAMLProviderListerStore implements query.SAMLStore plus
// query.SAMLProviderIDLister so newAdminProviderConfigReadHandler's
// env-registered-provider detection can be tested without a real SAML
// provider config file.
type fakeSAMLProviderListerStore struct {
	providerIDs []string
}

func (f *fakeSAMLProviderListerStore) GetSAMLProvider(context.Context, string) (query.SAMLProviderConfig, bool, error) {
	return query.SAMLProviderConfig{}, false, nil
}

func (f *fakeSAMLProviderListerStore) CreateSAMLRequest(context.Context, string, query.SAMLRequestCreateRecord) error {
	return nil
}

func (f *fakeSAMLProviderListerStore) ConsumeSAMLRequest(context.Context, string, string, string, time.Time) (string, bool, error) {
	return "", false, nil
}

func (f *fakeSAMLProviderListerStore) ReserveSAMLReplay(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}

func (f *fakeSAMLProviderListerStore) ResolveSAMLPrincipal(context.Context, string, samlauth.Principal, time.Time) (query.AuthContext, bool, error) {
	return query.AuthContext{}, false, nil
}

func (f *fakeSAMLProviderListerStore) ListProviderIDs() []string {
	return f.providerIDs
}

// TestEnvProviderShadowsDBProvider proves a DB-backed provider config whose
// provider_config_id matches an env/file-registered provider is surfaced with
// ShadowedByEnvironment=true, while a non-colliding DB provider is not.
func TestEnvProviderShadowsDBProvider(t *testing.T) {
	t.Parallel()

	oidcHandler := &query.OIDCLoginHandler{
		Service: &fakeOIDCProviderListerService{
			providers: []query.OIDCRegisteredProvider{{ProviderConfigID: "env_oidc_1", TenantID: "tenant_a"}},
		},
	}
	samlHandler := &query.SAMLHandler{
		Store: &fakeSAMLProviderListerStore{providerIDs: []string{"env_saml_1"}},
	}

	adapter := &providerConfigReadAdapter{
		envProviderIDs: envRegisteredProviderIDs(oidcHandler, samlHandler),
	}

	shadowed := adapter.toAdminDetail(pgstatus.ProviderConfigDetail{
		ProviderConfigID: "env_oidc_1", ProviderKind: "external_oidc", Status: "active",
	})
	if !shadowed.ShadowedByEnvironment || shadowed.ManagedBy != "environment" {
		t.Fatalf("provider config sharing an id with an env-registered OIDC provider = %+v, want ShadowedByEnvironment=true ManagedBy=environment", shadowed)
	}

	samlShadowed := adapter.toAdminDetail(pgstatus.ProviderConfigDetail{
		ProviderConfigID: "env_saml_1", ProviderKind: "external_saml", Status: "active",
	})
	if !samlShadowed.ShadowedByEnvironment || samlShadowed.ManagedBy != "environment" {
		t.Fatalf("provider config sharing an id with an env-registered SAML provider = %+v, want ShadowedByEnvironment=true ManagedBy=environment", samlShadowed)
	}

	notShadowed := adapter.toAdminDetail(pgstatus.ProviderConfigDetail{
		ProviderConfigID: "pc_db_only", ProviderKind: "external_oidc", Status: "active",
	})
	if notShadowed.ShadowedByEnvironment || notShadowed.ManagedBy != "database" {
		t.Fatalf("a DB-only provider config (no env id collision) = %+v, want ShadowedByEnvironment=false ManagedBy=database", notShadowed)
	}
}

// TestListProviderConfigDetailsSynthesizesEnvOnlyOIDCProvider proves a pure
// env-file-only OIDC provider (no DB row at all) is still visible on the
// admin list, with ManagedBy="environment" (#4966 acceptance criteria: "env
// -defined provider visible with managed_by: environment").
func TestListProviderConfigDetailsSynthesizesEnvOnlyOIDCProvider(t *testing.T) {
	t.Parallel()
	oidcHandler := &query.OIDCLoginHandler{
		Service: &fakeOIDCProviderListerService{
			providers: []query.OIDCRegisteredProvider{
				{ProviderConfigID: "env_only_oidc", TenantID: "tenant_a"},
				{ProviderConfigID: "other_tenant_oidc", TenantID: "tenant_b"},
			},
		},
	}
	adapter := &providerConfigReadAdapter{
		envProviderIDs:   envRegisteredProviderIDs(oidcHandler, nil),
		envOIDCProviders: oidcHandler.RegisteredProviders(),
	}

	adapter.store = pgstatus.NewIdentitySubjectStore(&emptyProviderConfigListDB{})
	items, err := adapter.ListProviderConfigDetails(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("ListProviderConfigDetails() error = %v", err)
	}
	found := false
	for _, item := range items {
		if item.ProviderConfigID == "env_only_oidc" {
			found = true
			if item.ManagedBy != "environment" || item.ProviderKind != "oidc" {
				t.Fatalf("synthesized env-only entry = %+v, want ManagedBy=environment ProviderKind=oidc", item)
			}
		}
		if item.ProviderConfigID == "other_tenant_oidc" {
			t.Fatal("a different tenant's env-registered provider must not appear in this tenant's list")
		}
	}
	if !found {
		t.Fatal("env_only_oidc missing from ListProviderConfigDetails() — a pure env-file-only provider must still be admin-visible")
	}
}

// TestEnvRegisteredProviderIDsHandlesNilHandlers proves the merge helper
// degrades to an empty set rather than panicking when OIDC/SAML are not
// configured.
func TestEnvRegisteredProviderIDsHandlesNilHandlers(t *testing.T) {
	t.Parallel()
	ids := envRegisteredProviderIDs(nil, nil)
	if len(ids) != 0 {
		t.Fatalf("envRegisteredProviderIDs(nil, nil) = %v, want empty", ids)
	}
}

// emptyProviderConfigListDB is a minimal pgstatus.ExecQueryer returning zero
// rows for ListProviderConfigs, so tests can exercise
// providerConfigReadAdapter.ListProviderConfigDetails' synthesis/dedupe logic
// without a real database.
type emptyProviderConfigListDB struct{}

func (emptyProviderConfigListDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (emptyProviderConfigListDB) QueryContext(context.Context, string, ...any) (pgstatus.Rows, error) {
	return &emptyProviderConfigListRows{}, nil
}

type emptyProviderConfigListRows struct{}

func (*emptyProviderConfigListRows) Next() bool        { return false }
func (*emptyProviderConfigListRows) Scan(...any) error { return nil }
func (*emptyProviderConfigListRows) Err() error        { return nil }
func (*emptyProviderConfigListRows) Close() error      { return nil }
