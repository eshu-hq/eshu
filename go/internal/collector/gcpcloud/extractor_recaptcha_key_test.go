// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const recaptchaKeyFullName = "//recaptchaenterprise.googleapis.com/projects/123456789/keys/demo-key"

func recaptchaKeyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: recaptchaKeyFullName,
		AssetType:        recaptchaKeyAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestRecaptchaKeyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(recaptchaKeyAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", recaptchaKeyAssetType)
	}
}

func TestExtractRecaptchaKeyWeb(t *testing.T) {
	const data = `{
		"displayName": "Demo Key",
		"createTime": "2024-06-01T00:00:00Z",
		"webKeySettings": {
			"integrationType": "SCORE",
			"allowAllDomains": false,
			"allowedDomains": ["example.com", "app.example.com"]
		},
		"wafSettings": {"wafService": "CA", "wafFeature": "CHALLENGE_PAGE"}
	}`
	got, err := extractRecaptchaKey(recaptchaKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"display_name":         "Demo Key",
		"creation_time":        "2024-06-01T00:00:00Z",
		"platform_type":        "web",
		"integration_type":     "SCORE",
		"allowed_domain_count": 2,
		"waf_service":          "CA",
		"waf_feature":          "CHALLENGE_PAGE",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("reCAPTCHA key derives no outbound edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("reCAPTCHA key derives no outbound anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractRecaptchaKeyNeverPersistsDomainsOrIdentifiers(t *testing.T) {
	const data = `{
		"webKeySettings": {"allowedDomains": ["internal-secret.example.com"]},
		"androidKeySettings": {"allowedPackageNames": ["com.internal.secretapp"]},
		"iosKeySettings": {"allowedBundleIds": ["com.internal.secretbundle"]}
	}`
	got, err := extractRecaptchaKey(recaptchaKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"internal-secret.example.com", "com.internal.secretapp", "com.internal.secretbundle", "allowedDomains", "allowedPackageNames", "allowedBundleIds"} {
		if containsString(string(blob), token) {
			t.Fatalf("reCAPTCHA key extraction leaked platform identifier token %q: %s", token, blob)
		}
	}
}

func TestExtractRecaptchaKeyAndroidAllowAll(t *testing.T) {
	const data = `{"androidKeySettings": {"allowAllPackageNames": true}}`
	got, err := extractRecaptchaKey(recaptchaKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["platform_type"] != "android" {
		t.Errorf("platform_type = %v, want android", got.Attributes["platform_type"])
	}
	if got.Attributes["allow_all_package_names"] != true {
		t.Errorf("allow_all_package_names = %v, want true", got.Attributes["allow_all_package_names"])
	}
	if _, ok := got.Attributes["allowed_package_name_count"]; ok {
		t.Errorf("no explicit package names; count must be omitted: %#v", got.Attributes)
	}
}

func TestExtractRecaptchaKeyIOS(t *testing.T) {
	const data = `{"iosKeySettings": {"allowedBundleIds": ["com.x.app", "com.y.app"]}}`
	got, err := extractRecaptchaKey(recaptchaKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["platform_type"] != "ios" {
		t.Errorf("platform_type = %v, want ios", got.Attributes["platform_type"])
	}
	if got.Attributes["allowed_bundle_id_count"] != 2 {
		t.Errorf("allowed_bundle_id_count = %v, want 2", got.Attributes["allowed_bundle_id_count"])
	}
}

func TestExtractRecaptchaKeyEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractRecaptchaKey(recaptchaKeyContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
}

func TestExtractRecaptchaKeyMalformedDataErrors(t *testing.T) {
	if _, err := extractRecaptchaKey(recaptchaKeyContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
