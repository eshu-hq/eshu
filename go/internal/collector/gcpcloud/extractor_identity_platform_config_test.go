// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const identityConfigFullName = "//identitytoolkit.googleapis.com/projects/123456789/config"

func identityConfigContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: identityConfigFullName,
		AssetType:        identityConfigAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestIdentityConfigExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(identityConfigAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", identityConfigAssetType)
	}
}

func TestExtractIdentityConfigFullResource(t *testing.T) {
	const data = `{
		"signIn": {
			"email": {"enabled": true, "passwordRequired": true},
			"phoneNumber": {"enabled": false},
			"anonymous": {"enabled": true}
		},
		"mfa": {"state": "ENABLED"},
		"multiTenant": {"allowTenants": true},
		"authorizedDomains": ["localhost", "demo.firebaseapp.com", "internal.example.com"]
	}`
	got, err := extractIdentityPlatformConfig(identityConfigContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"sign_in_methods_enabled":    []string{"anonymous", "email"},
		"mfa_state":                  "ENABLED",
		"multi_tenant_allow_tenants": true,
		"authorized_domain_count":    3,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("identity config derives no outbound edges, got %#v", got.Relationships)
	}
}

func TestExtractIdentityConfigNeverPersistsDomainsOrSecrets(t *testing.T) {
	const data = `{
		"authorizedDomains": ["internal-secret.example.com"],
		"client": {"apiKey": "AIzaSy-SECRET", "clientSecret": "SUPER-SECRET-CLIENT"},
		"blockingFunctions": {"triggers": {"beforeSignIn": {"functionUri": "https://internal.example.com/fn"}}}
	}`
	got, err := extractIdentityPlatformConfig(identityConfigContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"internal-secret.example.com", "AIzaSy-SECRET", "SUPER-SECRET-CLIENT", "clientSecret", "apiKey", "blockingFunctions", "authorizedDomains", "functionUri"} {
		if containsString(string(blob), token) {
			t.Fatalf("identity config extraction leaked sensitive token %q: %s", token, blob)
		}
	}
}

func TestExtractIdentityConfigMFAMandatory(t *testing.T) {
	const data = `{"mfa": {"state": "MANDATORY"}}`
	got, err := extractIdentityPlatformConfig(identityConfigContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["mfa_state"] != "MANDATORY" {
		t.Errorf("mfa_state = %v, want MANDATORY", got.Attributes["mfa_state"])
	}
	if _, ok := got.Attributes["sign_in_methods_enabled"]; ok {
		t.Errorf("no sign-in methods present; must be omitted: %#v", got.Attributes)
	}
}

func TestExtractIdentityConfigNoMethodsEnabledOmitsList(t *testing.T) {
	const data = `{"signIn": {"email": {"enabled": false}, "phoneNumber": {"enabled": false}}}`
	got, err := extractIdentityPlatformConfig(identityConfigContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["sign_in_methods_enabled"]; ok {
		t.Errorf("no enabled methods must omit sign_in_methods_enabled: %#v", got.Attributes)
	}
}

func TestExtractIdentityConfigEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractIdentityPlatformConfig(identityConfigContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
}

func TestExtractIdentityConfigMalformedDataErrors(t *testing.T) {
	if _, err := extractIdentityPlatformConfig(identityConfigContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
