// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const (
	wifProviderFullName = "//iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/demo-pool/providers/aws-ci"
	wifProviderPool     = "//iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/demo-pool"
)

func wifProviderContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: wifProviderFullName,
		AssetType:        workloadIdentityPoolProviderAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestWorkloadIdentityPoolProviderExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(workloadIdentityPoolProviderAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", workloadIdentityPoolProviderAssetType)
	}
}

func TestExtractWIFProviderAWS(t *testing.T) {
	const data = `{
		"name": "projects/123456789/locations/global/workloadIdentityPools/demo-pool/providers/aws-ci",
		"state": "ACTIVE",
		"disabled": false,
		"attributeMapping": {"google.subject": "assertion.arn", "attribute.aws_role": "assertion.arn"},
		"attributeCondition": "assertion.account=='123456789012'",
		"aws": {"accountId": "123456789012"}
	}`
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"state":                       "ACTIVE",
		"disabled":                    false,
		"provider_type":               "aws",
		"aws_account_id":              "123456789012",
		"attribute_mapping_key_count": 2,
		"has_attribute_condition":     true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 provider->pool edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeWIFProviderOfPool, wifProviderPool, workloadIdentityPoolAssetType)
	wantAnchors := []string{"123456789012"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractWIFProviderOIDC(t *testing.T) {
	const data = `{
		"state": "ACTIVE",
		"oidc": {"issuerUri": "https://token.actions.githubusercontent.com", "allowedAudiences": ["a", "b"], "jwksJson": "{\"keys\":[\"SECRET-KEY-MATERIAL\"]}"},
		"attributeMapping": {"google.subject": "assertion.sub"}
	}`
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["provider_type"] != "oidc" {
		t.Errorf("provider_type = %v, want oidc", got.Attributes["provider_type"])
	}
	if got.Attributes["oidc_issuer_uri"] != "https://token.actions.githubusercontent.com" {
		t.Errorf("oidc_issuer_uri = %v", got.Attributes["oidc_issuer_uri"])
	}
	if got.Attributes["oidc_allowed_audience_count"] != 2 {
		t.Errorf("oidc_allowed_audience_count = %v, want 2", got.Attributes["oidc_allowed_audience_count"])
	}
	if _, ok := got.Attributes["has_attribute_condition"]; ok {
		t.Errorf("no attributeCondition present; flag must be omitted: %#v", got.Attributes)
	}
	// jwks key material and the attribute-mapping value must never leak.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"jwksJson", "SECRET-KEY-MATERIAL", "keys", "assertion.sub"} {
		if containsString(string(blob), token) {
			t.Fatalf("WIF provider extraction leaked oidc/mapping token %q: %s", token, blob)
		}
	}
	wantAnchors := []string{"https://token.actions.githubusercontent.com"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractWIFProviderSAMLNeverPersistsMetadata(t *testing.T) {
	const data = `{
		"state": "ACTIVE",
		"saml": {"idpMetadataXml": "<EntityDescriptor>SECRET-CERT-BLOB</EntityDescriptor>"}
	}`
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["provider_type"] != "saml" {
		t.Errorf("provider_type = %v, want saml", got.Attributes["provider_type"])
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"idpMetadataXml", "SECRET-CERT-BLOB", "EntityDescriptor"} {
		if containsString(string(blob), token) {
			t.Fatalf("WIF provider extraction leaked saml metadata token %q: %s", token, blob)
		}
	}
}

func TestExtractWIFProviderX509(t *testing.T) {
	// X.509 providers must surface a bounded provider_type while the trust-store
	// certificate material is never decoded.
	const data = `{
		"state": "ACTIVE",
		"x509": {"trustStore": {"trustAnchors": [{"pemCertificate": "-----BEGIN CERTIFICATE-----SECRET-CERT-----END CERTIFICATE-----"}]}}
	}`
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["provider_type"] != "x509" {
		t.Errorf("provider_type = %v, want x509", got.Attributes["provider_type"])
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"trustStore", "pemCertificate", "SECRET-CERT", "BEGIN CERTIFICATE"} {
		if containsString(string(blob), token) {
			t.Fatalf("WIF provider extraction leaked x509 trust-store token %q: %s", token, blob)
		}
	}
}

func TestExtractWIFProviderAWSDefaultAttributeMapping(t *testing.T) {
	// An AWS provider with no explicit attributeMapping still has IAM's default
	// two-key mapping; the effective count must be reported, not omitted.
	const data = `{"state": "ACTIVE", "aws": {"accountId": "123456789012"}}`
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["attribute_mapping_key_count"] != awsDefaultAttributeMappingKeyCount {
		t.Errorf("attribute_mapping_key_count = %v, want %d (AWS default)", got.Attributes["attribute_mapping_key_count"], awsDefaultAttributeMappingKeyCount)
	}
}

func TestExtractWIFProviderOIDCEmptyMappingOmitsCount(t *testing.T) {
	// A non-AWS provider with no attributeMapping must not fabricate a count.
	const data = `{"state": "ACTIVE", "oidc": {"issuerUri": "https://example.com"}}`
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["attribute_mapping_key_count"]; ok {
		t.Errorf("non-AWS empty mapping must omit attribute_mapping_key_count: %#v", got.Attributes)
	}
}

func TestExtractWIFProviderEmptyDataYieldsOnlyPoolEdge(t *testing.T) {
	got, err := extractWorkloadIdentityPoolProvider(wifProviderContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Errorf("expected the provider->pool edge from the full name, got %#v", got.Relationships)
	}
}

func TestExtractWIFProviderMalformedDataErrors(t *testing.T) {
	if _, err := extractWorkloadIdentityPoolProvider(wifProviderContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestParentWorkloadIdentityPoolFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"provider name", wifProviderFullName, wifProviderPool},
		{"no providers segment", wifProviderPool, ""},
		{"trailing providers marker, no id", wifProviderPool + "/providers/", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parentWorkloadIdentityPoolFullName(tc.in); got != tc.want {
				t.Errorf("parentWorkloadIdentityPoolFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
