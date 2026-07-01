// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const apiKeyFullName = "//apikeys.googleapis.com/projects/123456789/locations/global/keys/demo-key"

func apiKeyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: apiKeyFullName,
		AssetType:        apiKeyAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestAPIKeyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(apiKeyAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", apiKeyAssetType)
	}
}

func TestExtractAPIKeyFullResource(t *testing.T) {
	const data = `{
		"displayName": "Demo Key",
		"createTime": "2024-06-01T00:00:00Z",
		"restrictions": {
			"serverKeyRestrictions": {"allowedIps": ["203.0.113.4", "203.0.113.5"]},
			"apiTargets": [
				{"service": "translate.googleapis.com"},
				{"service": "maps-backend.googleapis.com"}
			]
		}
	}`
	got, err := extractAPIKey(apiKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"display_name":        "Demo Key",
		"creation_time":       "2024-06-01T00:00:00Z",
		"restriction_type":    "server",
		"api_target_count":    2,
		"api_target_services": []string{"maps-backend.googleapis.com", "translate.googleapis.com"},
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("API Key derives no outbound edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("API Key derives no outbound anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractAPIKeyNeverPersistsKeyStringOrIPs(t *testing.T) {
	const data = `{
		"displayName": "Demo Key",
		"keyString": "AIzaSy-SUPER-SECRET-KEY-STRING",
		"restrictions": {
			"serverKeyRestrictions": {"allowedIps": ["203.0.113.4"]},
			"browserKeyRestrictions": {"allowedReferrers": ["https://internal.example.com/*"]}
		}
	}`
	got, err := extractAPIKey(apiKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"AIzaSy-SUPER-SECRET-KEY-STRING", "keyString", "203.0.113.4", "allowedIps", "allowedReferrers", "internal.example.com"} {
		if containsString(string(blob), token) {
			t.Fatalf("API key extraction leaked sensitive token %q: %s", token, blob)
		}
	}
}

func TestExtractAPIKeyRestrictionTypes(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"browser", `{"restrictions": {"browserKeyRestrictions": {"allowedReferrers": ["https://x/*"]}}}`, "browser"},
		{"android", `{"restrictions": {"androidKeyRestrictions": {"allowedApplications": [{"packageName": "com.x", "sha1Fingerprint": "AA"}]}}}`, "android"},
		{"ios", `{"restrictions": {"iosKeyRestrictions": {"allowedBundleIds": ["com.x.app"]}}}`, "ios"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractAPIKey(apiKeyContext(tc.data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Attributes["restriction_type"] != tc.want {
				t.Errorf("restriction_type = %v, want %v", got.Attributes["restriction_type"], tc.want)
			}
		})
	}
}

func TestExtractAPIKeyUnrestricted(t *testing.T) {
	const data = `{"displayName": "Open Key", "createTime": "2024-06-01T00:00:00Z"}`
	got, err := extractAPIKey(apiKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["restriction_type"]; ok {
		t.Errorf("unrestricted key must omit restriction_type: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["api_target_count"]; ok {
		t.Errorf("no api targets must omit api_target_count: %#v", got.Attributes)
	}
}

func TestExtractAPIKeyEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractAPIKey(apiKeyContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
}

func TestExtractAPIKeyMalformedDataErrors(t *testing.T) {
	if _, err := extractAPIKey(apiKeyContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
