// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
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

	shadowed := adapter.toAdminDetail(context.Background(), pgstatus.ProviderConfigDetail{
		ProviderConfigID: "env_oidc_1", ProviderKind: "external_oidc", Status: "active",
	})
	if !shadowed.ShadowedByEnvironment || shadowed.ManagedBy != "environment" {
		t.Fatalf("provider config sharing an id with an env-registered OIDC provider = %+v, want ShadowedByEnvironment=true ManagedBy=environment", shadowed)
	}

	samlShadowed := adapter.toAdminDetail(context.Background(), pgstatus.ProviderConfigDetail{
		ProviderConfigID: "env_saml_1", ProviderKind: "external_saml", Status: "active",
	})
	if !samlShadowed.ShadowedByEnvironment || samlShadowed.ManagedBy != "environment" {
		t.Fatalf("provider config sharing an id with an env-registered SAML provider = %+v, want ShadowedByEnvironment=true ManagedBy=environment", samlShadowed)
	}

	notShadowed := adapter.toAdminDetail(context.Background(), pgstatus.ProviderConfigDetail{
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

// TestListProviderConfigDetailsSynthesizesEnvOnlySAMLProvider proves a pure
// env-file-only SAML provider (no DB row at all) is admin-visible with
// ManagedBy="environment" (closes the #4978 gap: SAML env config carries no
// tenant_id, unlike OIDC's config file, so a synthesized SAML entry is
// tenant-agnostic — it must appear for every tenant's admin list, matching
// GetSAMLProvider's own tenant-agnostic env lookup in saml_sso.go).
func TestListProviderConfigDetailsSynthesizesEnvOnlySAMLProvider(t *testing.T) {
	t.Parallel()
	samlHandler := &query.SAMLHandler{
		Store: &fakeSAMLProviderListerStore{providerIDs: []string{"env_only_saml"}},
	}
	adapter := &providerConfigReadAdapter{
		envProviderIDs:     envRegisteredProviderIDs(nil, samlHandler),
		envSAMLProviderIDs: samlHandler.RegisteredProviderIDs(),
	}
	adapter.store = pgstatus.NewIdentitySubjectStore(&emptyProviderConfigListDB{})

	for _, tenantID := range []string{"tenant_a", "tenant_b"} {
		items, err := adapter.ListProviderConfigDetails(context.Background(), tenantID)
		if err != nil {
			t.Fatalf("ListProviderConfigDetails(%q) error = %v", tenantID, err)
		}
		found := false
		for _, item := range items {
			if item.ProviderConfigID == "env_only_saml" {
				found = true
				if item.ManagedBy != "environment" || item.ProviderKind != "saml" {
					t.Fatalf("synthesized env-only SAML entry = %+v, want ManagedBy=environment ProviderKind=saml", item)
				}
			}
		}
		if !found {
			t.Fatalf("env_only_saml missing from ListProviderConfigDetails(%q) — a pure env-file-only SAML provider must be admin-visible for every tenant (tenant-agnostic)", tenantID)
		}
	}
}

// TestListProviderConfigDetailsDoesNotSynthesizeSAMLIDOwnedByAnotherTenant is
// the P1 regression guard (codex PR #5064): an env SAML id that is ALSO backed
// by an active DB provider_config row (owned by some tenant) must NOT be
// synthesized onto a different tenant's admin list — that would advertise
// another tenant's SAML provider. This tenant has no DB rows of its own (empty
// list) but HasActiveSAMLProviderConfig reports the id active somewhere.
func TestListProviderConfigDetailsDoesNotSynthesizeSAMLIDOwnedByAnotherTenant(t *testing.T) {
	t.Parallel()
	samlHandler := &query.SAMLHandler{
		Store: &fakeSAMLProviderListerStore{providerIDs: []string{"shadowed_saml"}},
	}
	adapter := &providerConfigReadAdapter{
		envProviderIDs:     envRegisteredProviderIDs(nil, samlHandler),
		envSAMLProviderIDs: samlHandler.RegisteredProviderIDs(),
	}
	adapter.store = pgstatus.NewIdentitySubjectStore(collisionSAMLProviderConfigDB{activeSAMLID: "shadowed_saml"})

	items, err := adapter.ListProviderConfigDetails(context.Background(), "tenant_b")
	if err != nil {
		t.Fatalf("ListProviderConfigDetails() error = %v", err)
	}
	for _, item := range items {
		if item.ProviderConfigID == "shadowed_saml" {
			t.Fatalf("shadowed_saml synthesized onto tenant_b's admin list = %+v; an env SAML id with an active DB row (owned by another tenant) must not be advertised cross-tenant", item)
		}
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

// TestDecodeProviderConfigurationLogsMalformedJSON proves that a stored
// configuration column that fails to parse as JSON is surfaced via a warning
// log (naming the provider_config_id and the parse error) rather than being
// silently swallowed. The API response still degrades to configuration:null
// (the column is never secret, so a decode failure must not fail the whole
// list/detail read) — but an operator can now find the corrupt row from logs
// instead of treating it as a legitimately empty configuration.
func TestDecodeProviderConfigurationLogsMalformedJSON(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	adapter := &providerConfigReadAdapter{
		logger: slog.New(slog.NewJSONHandler(&logBuf, nil)),
	}

	detail := adapter.toAdminDetail(context.Background(), pgstatus.ProviderConfigDetail{
		ProviderConfigID: "pc_corrupt",
		ProviderKind:     "external_oidc",
		Status:           "active",
		Configuration:    "{not valid json",
	})

	if detail.Configuration != nil {
		t.Fatalf("Configuration = %v, want nil for malformed JSON", detail.Configuration)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "pc_corrupt") {
		t.Fatalf("log output missing provider_config_id %q; got: %s", "pc_corrupt", logged)
	}
	if !strings.Contains(strings.ToLower(logged), "warn") {
		t.Fatalf("log output missing a warning-level entry for malformed configuration; got: %s", logged)
	}
}

// TestDecodeProviderConfigurationNilLoggerSafe proves a nil logger (the
// zero-value providerConfigReadAdapter used by most other tests in this file)
// does not panic when the configuration fails to decode.
func TestDecodeProviderConfigurationNilLoggerSafe(t *testing.T) {
	t.Parallel()

	adapter := &providerConfigReadAdapter{}
	detail := adapter.toAdminDetail(context.Background(), pgstatus.ProviderConfigDetail{
		ProviderConfigID: "pc_corrupt_nil_logger",
		ProviderKind:     "external_oidc",
		Status:           "active",
		Configuration:    "{not valid json",
	})
	if detail.Configuration != nil {
		t.Fatalf("Configuration = %v, want nil for malformed JSON", detail.Configuration)
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

// collisionSAMLProviderConfigDB returns an empty tenant provider-config list
// but reports one id active via selectActiveSAMLProviderConfigQuery — modeling
// an env SAML id that a DIFFERENT tenant owns via an active DB row (codex
// PR #5064 P1).
type collisionSAMLProviderConfigDB struct{ activeSAMLID string }

func (collisionSAMLProviderConfigDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (d collisionSAMLProviderConfigDB) QueryContext(_ context.Context, query string, args ...any) (pgstatus.Rows, error) {
	if strings.Contains(query, "pc.provider_kind = 'external_saml'") && len(args) == 1 {
		if id, ok := args[0].(string); ok && id == d.activeSAMLID {
			return &singleStringRows{value: d.activeSAMLID}, nil
		}
	}
	return &emptyProviderConfigListRows{}, nil
}

type singleStringRows struct {
	value string
	done  bool
}

func (r *singleStringRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *singleStringRows) Scan(dest ...any) error {
	if len(dest) > 0 {
		if p, ok := dest[0].(*string); ok {
			*p = r.value
		}
	}
	return nil
}

func (*singleStringRows) Err() error   { return nil }
func (*singleStringRows) Close() error { return nil }
