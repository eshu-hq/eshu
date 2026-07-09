// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/oidclogin"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
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

// authProvidersFakeDB is a minimal pgstatus.ExecQueryer fake driving the three
// read queries ListLoginProviders depends on: the tenant-scoped active-login
// -provider list, and the two per-id "is this provider active for this
// tenant" checks used for env-config providers. Dispatch is by SQL-shape
// substring (mirroring the WHERE clause each query uses to select its
// provider_kind), since this package cannot reference postgres's unexported
// query constants directly.
type authProvidersFakeDB struct {
	// dbRows is every DB-backed provider row exposed by the tenant-scoped
	// active-login-provider list.
	dbRows []pgstatus.LoginProviderItem
	// activeForTenant marks which provider_config_ids the per-id active
	// -check queries report as active (used for env-registered providers).
	activeForTenant map[string]bool
}

func (f *authProvidersFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *authProvidersFakeDB) QueryContext(_ context.Context, query string, args ...any) (pgstatus.Rows, error) {
	switch {
	case strings.Contains(query, "provider_kind IN ('external_oidc', 'external_saml')"):
		data := make([][]any, 0, len(f.dbRows))
		for _, row := range f.dbRows {
			data = append(data, []any{row.ProviderConfigID, row.ProviderKind})
		}
		return &authProvidersFakeRows{data: data}, nil
	case strings.Contains(query, "provider_kind = 'external_saml'"), strings.Contains(query, "provider_kind = 'external_oidc'"):
		providerConfigID, _ := args[0].(string)
		if f.activeForTenant[providerConfigID] {
			return &authProvidersFakeRows{data: [][]any{{providerConfigID}}}, nil
		}
		return &authProvidersFakeRows{}, nil
	default:
		return nil, nil
	}
}

// authProvidersFakeRows is a minimal pgstatus.Rows fake supporting *string
// scans only — every column ListLoginProviders' backing queries select is a
// string (provider_config_id, provider_kind).
type authProvidersFakeRows struct {
	data [][]any
	idx  int
}

func (r *authProvidersFakeRows) Next() bool {
	if r.idx == 0 && r.data == nil {
		return false
	}
	r.idx++
	return r.idx <= len(r.data)
}

func (r *authProvidersFakeRows) Scan(dest ...any) error {
	row := r.data[r.idx-1]
	for i, val := range row {
		if d, ok := dest[i].(*string); ok {
			*d = val.(string)
		}
	}
	return nil
}

func (r *authProvidersFakeRows) Err() error   { return nil }
func (r *authProvidersFakeRows) Close() error { return nil }

// TestListLoginProvidersEnvProviderWinsOverCollidingDBRow proves that when a
// provider_config_id is registered both via env/file config (here, OIDC) and
// has a colliding DB row, the DB-sourced entry is NOT what wins: it is
// excluded, and the env-sourced entry is subject to the env path's OWN
// per-tenant active check rather than trusted from the DB row's plain
// "active" list membership. Env config is authoritative for login per the
// epic #4962 contract, matching the read adapter's shadowed_by_environment
// derivation for the same collision (see
// admin_provider_config_reads_test.go's TestEnvProviderShadowsDBProvider).
//
// The differentiator: the DB row for env_oidc_1 IS present in the plain
// active-login-provider list (as the old, buggy code would trust outright),
// but the env path's separate HasActiveOIDCProviderConfigForTenant check is
// deliberately made to report false. Under the old (buggy) DB-wins ordering,
// env_oidc_1 would still appear (sourced from the DB row, never subjected to
// the env active check). Under the fixed env-wins ordering, the DB row is
// skipped because the id is env-registered, and the env path's own active
// check then correctly excludes it — env_oidc_1 must be ABSENT.
//
// This test also proves the SAML twin of the OIDC dead-button fix now holds
// in the OTHER direction: an enabled DB-only external_saml row IS offered as
// a login option, exactly like external_oidc, because the SAML login runtime
// now resolves DB-backed providers too (samlDBProviderResolver, #4966, epic
// #4962; completes #4978) — surfacing it no longer risks a login that always
// fails.
func TestListLoginProvidersEnvProviderWinsOverCollidingDBRow(t *testing.T) {
	t.Parallel()

	fakeDB := &authProvidersFakeDB{
		dbRows: []pgstatus.LoginProviderItem{
			// Colliding row: same id as the env-registered OIDC provider
			// below, and reported "active" by the plain list query.
			{ProviderConfigID: "env_oidc_1", ProviderKind: "external_oidc"},
			// Non-colliding DB-only SAML row: must appear, same as OIDC — the
			// SAML DB login runtime now resolves it (#4978).
			{ProviderConfigID: "pc_db_only_saml", ProviderKind: "external_saml"},
			// Non-colliding DB-only OIDC row: must still appear regardless.
			{ProviderConfigID: "pc_db_only_oidc", ProviderKind: "external_oidc"},
		},
		// env_oidc_1 deliberately reports NOT active via the env path's own
		// per-tenant check (map defaults to false for an absent key) — the
		// differentiator described above.
		activeForTenant: map[string]bool{},
	}
	store := &authProviderListStore{
		identity: pgstatus.NewIdentitySubjectStore(fakeDB),
		oidcProviders: []query.OIDCRegisteredProvider{
			{ProviderConfigID: "env_oidc_1", TenantID: "tenant_a"},
		},
	}

	items, err := store.ListLoginProviders(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("ListLoginProviders() error = %v", err)
	}

	byID := make(map[string]query.AuthProviderItem, len(items))
	for _, item := range items {
		if _, dup := byID[item.ProviderConfigID]; dup {
			t.Fatalf("provider_config_id %q appeared twice in ListLoginProviders() result", item.ProviderConfigID)
		}
		byID[item.ProviderConfigID] = item
	}

	if _, ok := byID["env_oidc_1"]; ok {
		t.Fatal("env_oidc_1 present in ListLoginProviders() result: the colliding DB row won instead of deferring to the env path's own (failing) active check — env must be authoritative")
	}
	if _, ok := byID["pc_db_only_saml"]; !ok {
		t.Fatal("pc_db_only_saml missing from ListLoginProviders() result: an enabled DB-only external_saml provider must now be offered as a login option (#4978)")
	}
	if _, ok := byID["pc_db_only_oidc"]; !ok {
		t.Fatal("pc_db_only_oidc (non-colliding DB-only external_oidc row) missing from ListLoginProviders() result")
	}

	// Positive case: when the env path's own active check DOES pass, the
	// env-registered id is surfaced (via the env path, not the DB row).
	fakeDB.activeForTenant = map[string]bool{"env_oidc_1": true}
	items, err = store.ListLoginProviders(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("ListLoginProviders() error = %v", err)
	}
	found := false
	for _, item := range items {
		if item.ProviderConfigID == "env_oidc_1" {
			found = true
		}
	}
	if !found {
		t.Fatal("env_oidc_1 missing once the env path's own active check passes")
	}
}

// TestLoginListAndReadAdapterAgreeOnCollision proves that for the same
// colliding provider_config_id, the pre-auth login list correctly excludes
// the DB-sourced entry (env wins — see
// TestListLoginProvidersEnvProviderWinsOverCollidingDBRow) AND the admin read
// adapter (providerConfigReadAdapter.toAdminDetail, admin_provider_config_reads.go)
// reports shadowed_by_environment=true for the same id. Both surfaces must
// agree that env config is authoritative for a colliding id; a mismatch here
// would mean the admin UI shows a provider as "editable, DB-authoritative"
// while the actual login path silently ignores its DB row (or vice versa).
func TestLoginListAndReadAdapterAgreeOnCollision(t *testing.T) {
	t.Parallel()
	const collidingID = "env_oidc_1"

	oidcHandler := &query.OIDCLoginHandler{
		Service: &fakeOIDCProviderListerService{
			providers: []query.OIDCRegisteredProvider{{ProviderConfigID: collidingID, TenantID: "tenant_a"}},
		},
	}

	// Read-adapter side: shadowed_by_environment must be true for the
	// colliding id.
	readAdapter := &providerConfigReadAdapter{
		envProviderIDs: envRegisteredProviderIDs(oidcHandler, nil),
	}
	detail := readAdapter.toAdminDetail(context.Background(), pgstatus.ProviderConfigDetail{
		ProviderConfigID: collidingID, ProviderKind: "external_oidc", Status: "active",
	})
	if !detail.ShadowedByEnvironment {
		t.Fatalf("read adapter: ShadowedByEnvironment = false for colliding id %q, want true", collidingID)
	}

	// Login-list side: the colliding DB row must not win (same differentiator
	// as TestListLoginProvidersEnvProviderWinsOverCollidingDBRow — the env
	// path's own active check is left failing).
	fakeDB := &authProvidersFakeDB{
		dbRows:          []pgstatus.LoginProviderItem{{ProviderConfigID: collidingID, ProviderKind: "external_oidc"}},
		activeForTenant: map[string]bool{},
	}
	loginStore := &authProviderListStore{
		identity:      pgstatus.NewIdentitySubjectStore(fakeDB),
		oidcProviders: oidcHandler.RegisteredProviders(),
	}
	items, err := loginStore.ListLoginProviders(context.Background(), "tenant_a")
	if err != nil {
		t.Fatalf("ListLoginProviders() error = %v", err)
	}
	for _, item := range items {
		if item.ProviderConfigID == collidingID {
			t.Fatalf("login list: colliding id %q was surfaced from the DB row, contradicting the read adapter's shadowed_by_environment=true", collidingID)
		}
	}
}
