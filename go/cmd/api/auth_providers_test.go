// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeOIDCLoginService satisfies query.OIDCLoginService but does not implement
// query.OIDCProviderLister. Used to verify that RegisteredProviders returns nil
// when the service does not support provider listing.
type fakeOIDCLoginService struct{}

func (fakeOIDCLoginService) StartOIDCLogin(context.Context, query.OIDCLoginStartRequest) (query.OIDCLoginStartResponse, error) {
	return query.OIDCLoginStartResponse{}, nil
}

func (fakeOIDCLoginService) CompleteOIDCLogin(context.Context, query.OIDCLoginCompleteRequest) (query.OIDCLoginCompleteResponse, error) {
	return query.OIDCLoginCompleteResponse{}, nil
}

// TestOIDCServiceAdapterListOIDCProviderIDsExposesOnlySafeFields proves that
// oidcServiceAdapter.ListOIDCProviderIDs returns only the provider_config_id
// and tenant_id fields from each configured OIDC provider. No issuer URL,
// client ID, client secret, scopes, or claim mappings are exposed.
func TestOIDCServiceAdapterListOIDCProviderIDsExposesOnlySafeFields(t *testing.T) {
	t.Parallel()

	// All required fields must be present for ValidateConfig to accept the config
	// and preserve the provider list in service.config.Providers.
	service := oidclogin.NewService(
		oidclogin.Config{
			Providers: []oidclogin.ProviderConfig{
				{
					ProviderConfigID: "provider_oidc_a",
					TenantID:         "tenant-a",
					WorkspaceID:      "workspace-a",
					IssuerURL:        "https://issuer.example.test",
					ClientID:         "client-id-a",
					RedirectURL:      "https://app.example.test/callback",
				},
				{
					ProviderConfigID: "provider_oidc_b",
					TenantID:         "tenant-b",
					WorkspaceID:      "workspace-b",
					IssuerURL:        "https://other-issuer.example.test",
					ClientID:         "client-id-b",
					RedirectURL:      "https://app.example.test/callback",
				},
			},
		},
		nil, // no state store needed for listing
		nil, // no grant resolver needed for listing
		nil, // no connector factory needed for listing
	)

	adapter := oidcServiceAdapter{service}
	providers := adapter.ListOIDCProviderIDs()

	if len(providers) != 2 {
		t.Fatalf("ListOIDCProviderIDs() = %d providers, want 2", len(providers))
	}

	// Verify only safe fields are present — all sensitive fields are absent from
	// the returned OIDCRegisteredProvider struct by construction.
	wantByID := map[string]string{
		"provider_oidc_a": "tenant-a",
		"provider_oidc_b": "tenant-b",
	}
	for _, p := range providers {
		wantTenant, ok := wantByID[p.ProviderConfigID]
		if !ok {
			t.Errorf("unexpected provider_config_id %q in ListOIDCProviderIDs()", p.ProviderConfigID)
			continue
		}
		if p.TenantID != wantTenant {
			t.Errorf("provider %q: TenantID = %q, want %q", p.ProviderConfigID, p.TenantID, wantTenant)
		}
	}
}

// TestOIDCLoginHandlerRegisteredProvidersNilSafe proves RegisteredProviders
// returns nil for a nil handler and for a handler whose Service does not
// implement OIDCProviderLister.
func TestOIDCLoginHandlerRegisteredProvidersNilSafe(t *testing.T) {
	t.Parallel()

	var h *query.OIDCLoginHandler
	if got := h.RegisteredProviders(); got != nil {
		t.Fatalf("nil handler RegisteredProviders() = %v, want nil", got)
	}

	// Service that does not implement OIDCProviderLister.
	h = &query.OIDCLoginHandler{Service: fakeOIDCLoginService{}}
	if got := h.RegisteredProviders(); got != nil {
		t.Fatalf("non-listing service RegisteredProviders() = %v, want nil", got)
	}
}

// TestNewAuthProviderListStoreOIDCHandlerNilSafe proves newAuthProviderListStore
// does not panic when oidcHandler is nil.
func TestNewAuthProviderListStoreOIDCHandlerNilSafe(t *testing.T) {
	t.Parallel()

	store := newAuthProviderListStore(nil, nil, nil)
	if store == nil {
		t.Fatal("newAuthProviderListStore() = nil, want non-nil store")
	}
	if len(store.oidcProviders) != 0 {
		t.Fatalf("store.oidcProviders = %v, want empty when oidcHandler is nil", store.oidcProviders)
	}
}
