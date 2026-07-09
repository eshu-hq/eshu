// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
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
	if !shadowed.ShadowedByEnvironment {
		t.Fatal("provider config sharing an id with an env-registered OIDC provider is not marked ShadowedByEnvironment")
	}

	samlShadowed := adapter.toAdminDetail(pgstatus.ProviderConfigDetail{
		ProviderConfigID: "env_saml_1", ProviderKind: "external_saml", Status: "active",
	})
	if !samlShadowed.ShadowedByEnvironment {
		t.Fatal("provider config sharing an id with an env-registered SAML provider is not marked ShadowedByEnvironment")
	}

	notShadowed := adapter.toAdminDetail(pgstatus.ProviderConfigDetail{
		ProviderConfigID: "pc_db_only", ProviderKind: "external_oidc", Status: "active",
	})
	if notShadowed.ShadowedByEnvironment {
		t.Fatal("a DB-only provider config (no env id collision) must not be marked ShadowedByEnvironment")
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
