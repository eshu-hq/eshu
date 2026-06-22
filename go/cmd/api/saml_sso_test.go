package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

func TestNewSAMLHandlerDisabledWhenProvidersUnset(t *testing.T) {
	t.Parallel()

	handler, err := newSAMLHandler(nil, nil, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatalf("newSAMLHandler() error = %v, want nil", err)
	}
	if handler != nil {
		t.Fatalf("newSAMLHandler() = %#v, want nil when %s is unset", handler, envSAMLProvidersJSON)
	}
}

func TestNewSAMLHandlerRequiresPostgresWhenProvidersConfigured(t *testing.T) {
	t.Parallel()

	_, err := newSAMLHandler(nil, nil, samlTestGetenv(), fakeBrowserSessionStore{})
	if err == nil {
		t.Fatal("newSAMLHandler() error = nil, want postgres requirement")
	}
	if !strings.Contains(err.Error(), "postgres is required") {
		t.Fatalf("newSAMLHandler() error = %q, want postgres requirement", err)
	}
}

func TestLoadSAMLProviderConfigsResolvesGroupRuleAuthContext(t *testing.T) {
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
	if !ok {
		t.Fatal("ResolveSAMLPrincipal() ok = false, want true")
	}
	if auth.TenantID != "tenant_a" || auth.WorkspaceID != "workspace_a" {
		t.Fatalf("auth tenant/workspace = %q/%q, want tenant_a/workspace_a", auth.TenantID, auth.WorkspaceID)
	}
	if auth.SubjectClass != "external_saml" || auth.SubjectIDHash != "sha256:subject" {
		t.Fatalf("auth subject = %q/%q, want external_saml/sha256:subject", auth.SubjectClass, auth.SubjectIDHash)
	}
	if !auth.AllScopes || auth.PolicyRevisionHash != "sha256:policy" {
		t.Fatalf("auth grant = all_scopes:%t policy:%q, want all scopes policy", auth.AllScopes, auth.PolicyRevisionHash)
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
    "hash_scope": "tenant_a/provider_a",
    "auth_rules": [
      {
        "required_group_keys": ["saml_admins"],
        "tenant_id": "tenant_a",
        "workspace_id": "workspace_a",
        "policy_revision_hash": "sha256:policy",
        "all_scopes": true
      }
    ]
  }
]`
