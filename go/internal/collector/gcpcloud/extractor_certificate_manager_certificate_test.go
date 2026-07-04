// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const certManagerCertificateFullName = "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/certificates/api-cert"

func certManagerCertificateContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: certManagerCertificateFullName,
		AssetType:        assetTypeCertificateManagerCertificate,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCertificateManagerCertificateExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCertificateManagerCertificate); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCertificateManagerCertificate)
	}
}

func TestExtractCertificateManagerCertificateManagedFullAttributes(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/api-cert",
		"scope": "DEFAULT",
		"managed": {
			"domains": ["api.example.com", "www.example.com"],
			"dnsAuthorizations": [
				"projects/demo-project/locations/us-central1/dnsAuthorizations/api-auth"
			],
			"issuanceConfig": "projects/demo-project/locations/us-central1/certificateIssuanceConfigs/private-ca-config",
			"state": "ACTIVE"
		},
		"sanDnsnames": ["api.example.com", "www.example.com"],
		"expireTime": "2026-06-01T00:00:00.000-07:00",
		"createTime": "2024-06-01T00:00:00.000-07:00",
		"updateTime": "2024-06-02T00:00:00.000-07:00",
		"labels": {"env": "prod"}
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":                    "MANAGED",
		"scope":                   "DEFAULT",
		"managed_state":           "ACTIVE",
		"managed_domain_count":    2,
		"dns_authorization_count": 1,
		"san_count":               2,
		"expire_time":             "2026-06-01T07:00:00Z",
		"create_time":             "2024-06-01T07:00:00Z",
		"update_time":             "2024-06-02T07:00:00Z",
		"label_count":             1,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAuthName := "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/dnsAuthorizations/api-auth"
	wantIssuanceName := "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/certificateIssuanceConfigs/private-ca-config"
	wantAnchors := []string{wantAuthName, wantIssuanceName}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 relationships, got %#v", got.Relationships)
	}
	dnsAuthRel := got.Relationships[0]
	if dnsAuthRel.RelationshipType != relationshipTypeCertManagerCertificateUsesDNSAuthorization {
		t.Errorf("dns auth relationship type = %q, want %q", dnsAuthRel.RelationshipType, relationshipTypeCertManagerCertificateUsesDNSAuthorization)
	}
	if dnsAuthRel.TargetFullResourceName != wantAuthName {
		t.Errorf("dns auth target = %q, want %q", dnsAuthRel.TargetFullResourceName, wantAuthName)
	}
	if dnsAuthRel.TargetAssetType != assetTypeCertificateManagerDNSAuthorization {
		t.Errorf("dns auth target type = %q, want %q", dnsAuthRel.TargetAssetType, assetTypeCertificateManagerDNSAuthorization)
	}
	if dnsAuthRel.SourceFullResourceName != certManagerCertificateFullName {
		t.Errorf("dns auth source = %q, want %q", dnsAuthRel.SourceFullResourceName, certManagerCertificateFullName)
	}

	issuanceRel := got.Relationships[1]
	if issuanceRel.RelationshipType != relationshipTypeCertManagerCertificateUsesIssuanceConfig {
		t.Errorf("issuance relationship type = %q, want %q", issuanceRel.RelationshipType, relationshipTypeCertManagerCertificateUsesIssuanceConfig)
	}
	if issuanceRel.TargetFullResourceName != wantIssuanceName {
		t.Errorf("issuance target = %q, want %q", issuanceRel.TargetFullResourceName, wantIssuanceName)
	}
	if issuanceRel.TargetAssetType != assetTypeCertificateManagerCertificateIssuanceConfig {
		t.Errorf("issuance target type = %q, want %q", issuanceRel.TargetAssetType, assetTypeCertificateManagerCertificateIssuanceConfig)
	}
}

func TestExtractCertificateManagerCertificateUsedByCertificateMapEntry(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/map-served-cert",
		"selfManaged": {},
		"usedBy": [
			{"name": "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/certificateMaps/prod-map/certificateMapEntries/prod-entry"}
		]
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantEntryName := "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/certificateMaps/prod-map/certificateMapEntries/prod-entry"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %#v", got.Relationships)
	}
	rel := got.Relationships[0]
	if rel.RelationshipType != relationshipTypeCertManagerCertificateUsedByCertificateMapEntry {
		t.Errorf("relationship type = %q, want %q", rel.RelationshipType, relationshipTypeCertManagerCertificateUsedByCertificateMapEntry)
	}
	if rel.TargetFullResourceName != wantEntryName {
		t.Errorf("target = %q, want %q", rel.TargetFullResourceName, wantEntryName)
	}
	if rel.TargetAssetType != assetTypeCertificateManagerCertificateMapEntry {
		t.Errorf("target type = %q, want %q", rel.TargetAssetType, assetTypeCertificateManagerCertificateMapEntry)
	}
	if rel.SourceFullResourceName != certManagerCertificateFullName {
		t.Errorf("source = %q, want %q", rel.SourceFullResourceName, certManagerCertificateFullName)
	}
	if !containsStringSlice(got.CorrelationAnchors, wantEntryName) {
		t.Errorf("expected anchor %q in %#v", wantEntryName, got.CorrelationAnchors)
	}
}

func TestExtractCertificateManagerCertificateUsedByTargetHTTPSProxy(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/classic-served-cert",
		"selfManaged": {},
		"usedBy": [
			{"name": "//compute.googleapis.com/projects/demo-project/global/targetHttpsProxies/prod-proxy"}
		]
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantProxyName := "//compute.googleapis.com/projects/demo-project/global/targetHttpsProxies/prod-proxy"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %#v", got.Relationships)
	}
	rel := got.Relationships[0]
	if rel.RelationshipType != relationshipTypeCertManagerCertificateUsedByTargetHTTPSProxy {
		t.Errorf("relationship type = %q, want %q", rel.RelationshipType, relationshipTypeCertManagerCertificateUsedByTargetHTTPSProxy)
	}
	if rel.TargetFullResourceName != wantProxyName {
		t.Errorf("target = %q, want %q", rel.TargetFullResourceName, wantProxyName)
	}
	if rel.TargetAssetType != assetTypeComputeTargetHTTPSProxy {
		t.Errorf("target type = %q, want %q", rel.TargetAssetType, assetTypeComputeTargetHTTPSProxy)
	}
}

func TestExtractCertificateManagerCertificateUsedByMultipleEntriesDeduped(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/multi-used-cert",
		"usedBy": [
			{"name": "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/certificateMaps/prod-map/certificateMapEntries/prod-entry"},
			{"name": "//certificatemanager.googleapis.com/projects/demo-project/locations/us-central1/certificateMaps/prod-map/certificateMapEntries/prod-entry"},
			{"name": "//compute.googleapis.com/projects/demo-project/global/targetHttpsProxies/prod-proxy"}
		]
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 deduped relationships, got %#v", got.Relationships)
	}
}

func TestExtractCertificateManagerCertificateUsedByUnresolvableNameEmitsNoEdge(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/untrusted-cert",
		"usedBy": [
			{"name": "not-a-recognized-resource-name"},
			{"name": "//storage.googleapis.com/projects/demo-project/buckets/some-bucket"},
			{"name": ""}
		]
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for unresolvable/untrusted usedBy names, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for unresolvable/untrusted usedBy names, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractCertificateManagerCertificateSelfManagedOmitsManagedFields(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/self-managed-cert",
		"scope": "DEFAULT",
		"selfManaged": {},
		"sanDnsnames": ["a.example.com", "b.example.com", "c.example.com"],
		"createTime": "2024-01-01T00:00:00.000-07:00"
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":        "SELF_MANAGED",
		"scope":       "DEFAULT",
		"san_count":   3,
		"create_time": "2024-01-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for a self-managed certificate, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for a self-managed certificate, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractCertificateManagerCertificateManagedIdentityDerivesType(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/mi-cert",
		"managedIdentity": {
			"identity": "spiffe://demo-project.svc.id.goog/ns/default/sa/workload",
			"state": "ACTIVE"
		},
		"createTime": "2024-02-01T00:00:00.000-07:00"
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":          "MANAGED_IDENTITY",
		"managed_state": "ACTIVE",
		"create_time":   "2024-02-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
}

func TestExtractCertificateManagerCertificateNoDNSAuthorizationsOmitsCount(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/no-auth-cert",
		"managed": {"domains": ["api.example.com"], "state": "PROVISIONING"}
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["dns_authorization_count"]; ok {
		t.Errorf("dns_authorization_count should be omitted when the list is empty, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships when dnsAuthorizations is empty, got %#v", got.Relationships)
	}
}

func TestExtractCertificateManagerCertificatePartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"type": "SELF_MANAGED"}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractCertificateManagerCertificateMalformedDataErrors(t *testing.T) {
	if _, err := extractCertificateManagerCertificate(certManagerCertificateContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractCertificateManagerCertificateZeroDomainsOmitsCount(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/empty-managed-cert",
		"managed": {"domains": []}
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["managed_domain_count"]; ok {
		t.Errorf("managed_domain_count should be omitted when the managed domain list is empty, got %#v", got.Attributes)
	}
}

func TestExtractCertificateManagerCertificateDedupesDNSAuthorizations(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/dup-auth-cert",
		"managed": {
			"domains": ["api.example.com"],
			"dnsAuthorizations": [
				"projects/demo-project/locations/us-central1/dnsAuthorizations/api-auth",
				"projects/demo-project/locations/us-central1/dnsAuthorizations/api-auth"
			]
		}
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["dns_authorization_count"] != 2 {
		t.Errorf("dns_authorization_count = %v, want 2 (raw provider count, pre-dedup)", got.Attributes["dns_authorization_count"])
	}
	if len(got.Relationships) != 1 {
		t.Errorf("expected 1 deduped relationship, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 1 {
		t.Errorf("expected 1 deduped anchor, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractCertificateManagerCertificateNeverLeaksKeyMaterialOrRawDomains(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/leak-check-cert",
		"pemCertificate": "-----BEGIN CERTIFICATE-----FAKECERTDATA-----END CERTIFICATE-----",
		"selfManaged": {
			"pemCertificate": "-----BEGIN CERTIFICATE-----FAKESELFCERTDATA-----END CERTIFICATE-----",
			"pemPrivateKey": "-----BEGIN PRIVATE KEY-----FAKEPRIVATEKEYDATA-----END PRIVATE KEY-----"
		},
		"managed": {
			"domains": ["secret-internal-host.example.com"],
			"dnsAuthorizations": ["projects/demo-project/locations/us-central1/dnsAuthorizations/api-auth"]
		},
		"managedIdentity": {
			"identity": "spiffe://demo-project.svc.id.goog/ns/default/sa/secret-workload"
		},
		"sanDnsnames": ["secret-internal-host.example.com"]
	}`

	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	bannedTokens := []string{
		"FAKECERTDATA",
		"FAKESELFCERTDATA",
		"FAKEPRIVATEKEYDATA",
		"BEGIN CERTIFICATE",
		"BEGIN PRIVATE KEY",
		"secret-internal-host.example.com",
		"spiffe://",
		"secret-workload",
		"pemCertificate",
		"pemPrivateKey",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}

func TestExtractCertificateManagerCertificateAdversarialRedactionSweep(t *testing.T) {
	// Full-struct JSON marshal + banned-token sweep per repo convention: any
	// secret-shaped, key-material, or raw-domain token anywhere in the
	// extraction output is a redaction failure regardless of which field it
	// leaked through.
	const data = `{
		"name": "projects/demo-project/locations/us-central1/certificates/adversarial-cert",
		"scope": "EDGE_CACHE",
		"pemCertificate": "-----BEGIN CERTIFICATE-----ADVERSARIALCERT-----END CERTIFICATE-----",
		"managed": {
			"domains": ["internal-admin.example.com", "billing.example.com"],
			"dnsAuthorizations": [
				"projects/demo-project/locations/us-central1/dnsAuthorizations/dns-auth-01"
			],
			"issuanceConfig": "projects/demo-project/locations/us-central1/certificateIssuanceConfigs/internal-ca",
			"state": "PROVISIONING",
			"provisioningIssue": {"reason": "AUTHORIZATION_ISSUE", "details": "internal-admin.example.com DNS challenge failed"}
		},
		"selfManaged": {
			"pemCertificate": "-----BEGIN CERTIFICATE-----ADVERSARIALSELFCERT-----END CERTIFICATE-----",
			"pemPrivateKey": "-----BEGIN PRIVATE KEY-----ADVERSARIALKEY-----END PRIVATE KEY-----"
		},
		"managedIdentity": {
			"identity": "spiffe://demo-project.svc.id.goog/ns/billing/sa/internal-admin"
		},
		"sanDnsnames": ["internal-admin.example.com"],
		"labels": {"team": "platform-secops"},
		"expireTime": "2026-06-01T00:00:00.000-07:00",
		"createTime": "2024-06-01T00:00:00.000-07:00"
	}`
	got, err := extractCertificateManagerCertificate(certManagerCertificateContext(data))
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
		"ADVERSARIALSELFCERT",
		"ADVERSARIALKEY",
		"BEGIN CERTIFICATE",
		"BEGIN PRIVATE KEY",
		"spiffe://",
		"DNS challenge failed",
		"AUTHORIZATION_ISSUE",
		"pemCertificate",
		"pemPrivateKey",
		"provisioningIssue",
		"selfManaged",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}
