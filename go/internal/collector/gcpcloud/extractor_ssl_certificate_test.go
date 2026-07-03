// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const sslCertificateFullName = "//compute.googleapis.com/projects/demo-project/global/sslCertificates/api-cert"

func sslCertificateContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: sslCertificateFullName,
		AssetType:        assetTypeComputeSslCertificate,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSslCertificateExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeSslCertificate); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeSslCertificate)
	}
}

func TestExtractSslCertificateManagedFullAttributes(t *testing.T) {
	const data = `{
		"name": "api-cert",
		"type": "MANAGED",
		"managed": {
			"domains": ["api.example.com", "www.example.com"],
			"status": "ACTIVE"
		},
		"expireTime": "2026-06-01T00:00:00.000-07:00",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractSslCertificate(sslCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":           "MANAGED",
		"managed_status": "ACTIVE",
		"domain_count":   2,
		"expire_time":    "2026-06-01T07:00:00Z",
		"creation_time":  "2024-06-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no outbound relationships (edges are inbound from target proxies), got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no correlation anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractSslCertificateSelfManagedOmitsManagedFields(t *testing.T) {
	const data = `{
		"name": "self-managed-cert",
		"type": "SELF_MANAGED",
		"selfManaged": {},
		"subjectAlternativeNames": ["a.example.com", "b.example.com", "c.example.com"],
		"creationTimestamp": "2024-01-01T00:00:00.000-07:00"
	}`

	got, err := extractSslCertificate(sslCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":          "SELF_MANAGED",
		"san_count":     3,
		"creation_time": "2024-01-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if _, ok := got.Attributes["managed_status"]; ok {
		t.Errorf("managed_status should be omitted for a self-managed certificate, got %#v", got.Attributes)
	}
	if _, ok := got.Attributes["domain_count"]; ok {
		t.Errorf("domain_count should be omitted for a self-managed certificate, got %#v", got.Attributes)
	}
}

func TestExtractSslCertificateNoExpireTimeOmitted(t *testing.T) {
	const data = `{
		"name": "no-expiry-cert",
		"type": "SELF_MANAGED"
	}`

	got, err := extractSslCertificate(sslCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["expire_time"]; ok {
		t.Errorf("expire_time should be omitted when absent, got %#v", got.Attributes)
	}
}

func TestExtractSslCertificatePartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractSslCertificate(sslCertificateContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractSslCertificateMalformedDataErrors(t *testing.T) {
	if _, err := extractSslCertificate(sslCertificateContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractSslCertificateZeroDomainsOmitsCount(t *testing.T) {
	const data = `{
		"name": "empty-managed-cert",
		"type": "MANAGED",
		"managed": {"domains": []}
	}`

	got, err := extractSslCertificate(sslCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["domain_count"]; ok {
		t.Errorf("domain_count should be omitted when the managed domain list is empty, got %#v", got.Attributes)
	}
}

func TestExtractSslCertificateNeverLeaksRawKeyMaterialOrDomains(t *testing.T) {
	const data = `{
		"name": "leak-check-cert",
		"type": "SELF_MANAGED",
		"selfManaged": {
			"certificate": "-----BEGIN CERTIFICATE-----FAKECERTDATA-----END CERTIFICATE-----",
			"privateKey": "-----BEGIN PRIVATE KEY-----FAKEPRIVATEKEYDATA-----END PRIVATE KEY-----"
		},
		"subjectAlternativeNames": ["secret-internal-host.example.com"]
	}`

	got, err := extractSslCertificate(sslCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	bannedTokens := []string{
		"FAKECERTDATA",
		"FAKEPRIVATEKEYDATA",
		"BEGIN CERTIFICATE",
		"BEGIN PRIVATE KEY",
		"secret-internal-host.example.com",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}

func TestExtractSslCertificateAdversarialRedactionSweep(t *testing.T) {
	// Full-struct JSON marshal + banned-token sweep per repo convention: any
	// secret-shaped, key-material, or raw-domain token anywhere in the
	// extraction output is a redaction failure regardless of which field it
	// leaked through.
	const data = `{
		"name": "adversarial-cert",
		"type": "MANAGED",
		"managed": {
			"domains": ["internal-admin.example.com", "billing.example.com"],
			"status": "PROVISIONING"
		},
		"selfManaged": {
			"certificate": "-----BEGIN CERTIFICATE-----ADVERSARIALCERT-----END CERTIFICATE-----",
			"privateKey": "-----BEGIN PRIVATE KEY-----ADVERSARIALKEY-----END PRIVATE KEY-----"
		},
		"subjectAlternativeNames": ["internal-admin.example.com"],
		"expireTime": "2026-06-01T00:00:00.000-07:00",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`
	got, err := extractSslCertificate(sslCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	bannedTokens := []string{
		"internal-admin.example.com",
		"billing.example.com",
		"ADVERSARIALCERT",
		"ADVERSARIALKEY",
		"BEGIN CERTIFICATE",
		"BEGIN PRIVATE KEY",
		"selfManaged",
		"certificate",
		"privateKey",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}
