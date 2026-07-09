// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestNewSAMLHandlerDisabledWhenProvidersUnset(t *testing.T) {
	t.Parallel()

	handler, err := newSAMLHandler(nil, nil, func(string) string { return "" }, nil, query.CookieSecureAuto, nil)
	if err != nil {
		t.Fatalf("newSAMLHandler() error = %v, want nil", err)
	}
	if handler != nil {
		t.Fatalf("newSAMLHandler() = %#v, want nil when %s is unset", handler, envSAMLProvidersJSON)
	}
}

func TestNewSAMLHandlerRequiresPostgresWhenProvidersConfigured(t *testing.T) {
	t.Parallel()

	_, err := newSAMLHandler(nil, nil, samlTestGetenv(), fakeBrowserSessionStore{}, query.CookieSecureAuto, nil)
	if err == nil {
		t.Fatal("newSAMLHandler() error = nil, want postgres requirement")
	}
	if !strings.Contains(err.Error(), "postgres is required") {
		t.Fatalf("newSAMLHandler() error = %q, want postgres requirement", err)
	}
}

func TestResolveSAMLPrincipalDeniesWithoutDurableIdentityStore(t *testing.T) {
	t.Parallel()

	providers, err := loadSAMLProviderConfigs(samlTestGetenv())
	if err != nil {
		t.Fatalf("loadSAMLProviderConfigs() error = %v", err)
	}
	runtime, ok := providers["provider_a"]
	if !ok {
		t.Fatal("provider_a missing")
	}
	if got := string(runtime.provider.IdentityProviderMetadataXML); got != samlTestMetadataXML {
		t.Fatalf("metadata XML = %q, want env-provided metadata", got)
	}
	store := &postgresSAMLStore{providers: providers}
	auth, ok, err := store.ResolveSAMLPrincipal(context.Background(), "provider_a", samlauth.Principal{
		ExternalSubjectHash: "sha256:subject",
		GroupKeys:           []string{"SAML_Admins"},
	}, time.Date(2026, 6, 22, 17, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveSAMLPrincipal() error = %v", err)
	}
	if ok || auth.TenantID != "" {
		t.Fatalf("ResolveSAMLPrincipal() auth = %#v ok = %t, want no group-rule permission fallback", auth, ok)
	}
}

func TestResolveSAMLPrincipalUsesDurableIdentity(t *testing.T) {
	t.Parallel()

	providers, err := loadSAMLProviderConfigs(samlTestGetenv())
	if err != nil {
		t.Fatalf("loadSAMLProviderConfigs() error = %v", err)
	}
	db := &samlIdentityTestDB{
		queryResponses: []samlIdentityTestRows{{
			rows: [][]any{{
				"tenant_durable",
				"workspace_durable",
				"sha256:user-subject",
				"sha256:policy-durable",
				"user_durable",
				true, // has_all_scope_role: admin path, no follow-up queries
			}},
		}},
	}
	store := &postgresSAMLStore{
		identity:  pgstatus.NewIdentitySubjectStore(db),
		providers: providers,
	}

	auth, ok, err := store.ResolveSAMLPrincipal(context.Background(), "provider_a", samlauth.Principal{
		ExternalSubjectHash: "sha256:external-subject",
		GroupClaimHash:      "sha256:groups-current",
		GroupKeys:           []string{"unmapped-in-static-rules"},
	}, time.Date(2026, 6, 22, 18, 20, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveSAMLPrincipal() error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveSAMLPrincipal() ok = false, want durable identity resolution")
	}
	if auth.TenantID != "tenant_durable" || auth.WorkspaceID != "workspace_durable" {
		t.Fatalf("auth tenant/workspace = %q/%q, want durable identity", auth.TenantID, auth.WorkspaceID)
	}
	if auth.SubjectIDHash != "sha256:user-subject" || auth.PolicyRevisionHash != "sha256:policy-durable" {
		t.Fatalf("auth subject/policy = %q/%q, want durable user/policy", auth.SubjectIDHash, auth.PolicyRevisionHash)
	}
	if !auth.AllScopes {
		t.Fatal("ResolveSAMLPrincipal() AllScopes = false, want true for admin durable subject")
	}
}

func TestResolveSAMLPrincipalDeniesKnownDurableSubject(t *testing.T) {
	t.Parallel()

	providers, err := loadSAMLProviderConfigs(samlTestGetenv())
	if err != nil {
		t.Fatalf("loadSAMLProviderConfigs() error = %v", err)
	}
	db := &samlIdentityTestDB{
		queryResponses: []samlIdentityTestRows{
			{},
			{rows: [][]any{{"external_identity_saml"}}},
		},
	}
	store := &postgresSAMLStore{
		identity:  pgstatus.NewIdentitySubjectStore(db),
		providers: providers,
	}

	auth, ok, err := store.ResolveSAMLPrincipal(context.Background(), "provider_a", samlauth.Principal{
		ExternalSubjectHash: "sha256:external-subject",
		GroupClaimHash:      "sha256:groups-stale",
		GroupKeys:           []string{"saml_admins"},
	}, time.Date(2026, 6, 22, 18, 25, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveSAMLPrincipal() error = %v", err)
	}
	if ok || auth.TenantID != "" {
		t.Fatalf("ResolveSAMLPrincipal() auth = %#v ok = %t, want durable denial without auth-rule fallback", auth, ok)
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("durable identity query count = %d, want resolution plus known-subject check", got)
	}
}

func TestResolveSAMLPrincipalDeniesUnknownDurableSubject(t *testing.T) {
	t.Parallel()

	providers, err := loadSAMLProviderConfigs(samlTestGetenv())
	if err != nil {
		t.Fatalf("loadSAMLProviderConfigs() error = %v", err)
	}
	db := &samlIdentityTestDB{
		queryResponses: []samlIdentityTestRows{{}, {}},
	}
	store := &postgresSAMLStore{
		identity:  pgstatus.NewIdentitySubjectStore(db),
		providers: providers,
	}

	auth, ok, err := store.ResolveSAMLPrincipal(context.Background(), "provider_a", samlauth.Principal{
		ExternalSubjectHash: "sha256:external-subject",
		GroupClaimHash:      "sha256:groups-current",
		GroupKeys:           []string{"saml_admins"},
	}, time.Date(2026, 6, 22, 18, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolveSAMLPrincipal() error = %v", err)
	}
	if ok || auth.TenantID != "" {
		t.Fatalf("ResolveSAMLPrincipal() auth = %#v ok = %t, want durable denial for unmapped subject", auth, ok)
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("durable identity query count = %d, want resolution plus known-subject check", got)
	}
}

func TestGetSAMLProviderRequiresActiveDurableProvider(t *testing.T) {
	t.Parallel()

	providers, err := loadSAMLProviderConfigs(samlTestGetenv())
	if err != nil {
		t.Fatalf("loadSAMLProviderConfigs() error = %v", err)
	}
	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{{}}}
	store := &postgresSAMLStore{
		identity:  pgstatus.NewIdentitySubjectStore(db),
		providers: providers,
	}

	cfg, ok, err := store.GetSAMLProvider(context.Background(), "provider_a")
	if err != nil {
		t.Fatalf("GetSAMLProvider() error = %v", err)
	}
	if ok || cfg.ProviderConfigID != "" {
		t.Fatalf("GetSAMLProvider() cfg = %#v ok = %t, want disabled without active provider row", cfg, ok)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("active provider query count = %d, want 1", got)
	}
	if !strings.Contains(db.queries[0].query, "pc.provider_kind = 'external_saml'") {
		t.Fatalf("active provider query did not require external_saml kind:\n%s", db.queries[0].query)
	}
}

func TestLoadSAMLProviderConfigsRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := loadSAMLProviderConfigs(func(key string) string {
		if key == envSAMLProvidersJSON {
			return `[{"provider_config_id":"provider_a","unexpected":true}]`
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadSAMLProviderConfigs() error = nil, want unknown field error")
	}
}

func samlTestGetenv() func(string) string {
	return func(key string) string {
		switch key {
		case envSAMLProvidersJSON:
			return samlTestProviderJSON
		case "SAML_METADATA_XML":
			return samlTestMetadataXML
		default:
			return ""
		}
	}
}

type fakeBrowserSessionStore struct{}

func (fakeBrowserSessionStore) CreateBrowserSession(context.Context, query.BrowserSessionCreateRecord) error {
	return nil
}

func (fakeBrowserSessionStore) RevokeBrowserSession(context.Context, string, time.Time) error {
	return nil
}

func (fakeBrowserSessionStore) SwitchBrowserSessionWorkspace(
	context.Context,
	string,
	string,
	string,
	time.Time,
) (query.AuthContext, bool, error) {
	return query.AuthContext{}, false, nil
}

type samlIdentityTestDB struct {
	queries        []samlIdentityTestQuery
	queryResponses []samlIdentityTestRows
}

type samlIdentityTestQuery struct {
	query string
	args  []any
}

func (db *samlIdentityTestDB) QueryContext(_ context.Context, query string, args ...any) (pgstatus.Rows, error) {
	db.queries = append(db.queries, samlIdentityTestQuery{query: query, args: args})
	if len(db.queryResponses) == 0 {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	rows := db.queryResponses[0]
	db.queryResponses = db.queryResponses[1:]
	return &rows, nil
}

func (db *samlIdentityTestDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected exec")
}

type samlIdentityTestRows struct {
	rows  [][]any
	index int
}

func (r *samlIdentityTestRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *samlIdentityTestRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *bool:
			value, ok := row[i].(bool)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want bool", i, row[i])
			}
			*target = value
		case *sql.NullString:
			// Used by GetActiveSAMLProviderConfigForLogin's
			// sealed_secret/configuration columns, which are nullable in the
			// real schema (identity_provider_config_revisions).
			switch value := row[i].(type) {
			case string:
				*target = sql.NullString{String: value, Valid: true}
			case nil:
				*target = sql.NullString{}
			default:
				return fmt.Errorf("row[%d] type = %T, want string or nil for sql.NullString", i, row[i])
			}
		default:
			return fmt.Errorf("unsupported scan target %T", dest[i])
		}
	}
	r.index++
	return nil
}

func (r *samlIdentityTestRows) Err() error {
	return nil
}

func (r *samlIdentityTestRows) Close() error {
	return nil
}

const samlTestMetadataXML = `<EntityDescriptor entityID="https://idp.example.test"></EntityDescriptor>`

const samlTestProviderJSON = `[
  {
    "provider_config_id": "provider_a",
    "service_provider_entity_id": "https://api.example.test/api/v0/auth/saml/providers/provider_a/metadata",
    "service_provider_acs_url": "https://api.example.test/api/v0/auth/saml/providers/provider_a/acs",
    "identity_provider_metadata_xml_env": "SAML_METADATA_XML",
    "expected_identity_provider_entity_id": "https://idp.example.test",
    "group_attribute_names": ["groups"],
    "require_groups": true,
    "hash_scope": "tenant_a/provider_a"
  }
]`
