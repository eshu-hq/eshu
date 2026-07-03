// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"strings"
	"testing"
)

const targetHTTPSProxyFullName = "//compute.googleapis.com/projects/demo-project/global/targetHttpsProxies/web-proxy"

func targetHTTPSProxyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: targetHTTPSProxyFullName,
		AssetType:        assetTypeComputeTargetHTTPSProxy,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestTargetHTTPSProxyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeTargetHTTPSProxy); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeTargetHTTPSProxy)
	}
}

func TestExtractTargetHTTPSProxyFullAttributes(t *testing.T) {
	const data = `{
		"name": "web-proxy",
		"urlMap": "https://www.googleapis.com/compute/v1/projects/demo-project/global/urlMaps/web-map",
		"sslCertificates": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/cert-1",
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/cert-2"
		],
		"sslPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/sslPolicies/modern-tls",
		"quicOverride": "ENABLE",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"quic_override": "ENABLE",
		"creation_time": "2024-06-01T07:00:00Z",
	}
	if len(got.Attributes) != len(wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	for k, v := range wantAttrs {
		if got.Attributes[k] != v {
			t.Errorf("attribute %q = %#v, want %#v", k, got.Attributes[k], v)
		}
	}

	wantURLMap := "//compute.googleapis.com/projects/demo-project/global/urlMaps/web-map"
	wantCert1 := "//compute.googleapis.com/projects/demo-project/global/sslCertificates/cert-1"
	wantCert2 := "//compute.googleapis.com/projects/demo-project/global/sslCertificates/cert-2"
	wantPolicy := "//compute.googleapis.com/projects/demo-project/global/sslPolicies/modern-tls"

	if len(got.Relationships) != 4 {
		t.Fatalf("expected 4 edges (url map, 2 ssl certificates, ssl policy), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesURLMap, wantURLMap, assetTypeComputeUrlMap)
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesSSLCertificate, wantCert1, assetTypeComputeSSLCertificate)
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesSSLCertificate, wantCert2, assetTypeComputeSSLCertificate)
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesSSLPolicy, wantPolicy, assetTypeComputeSSLPolicy)

	wantAnchors := []string{wantURLMap, wantCert1, wantCert2, wantPolicy}
	if len(got.CorrelationAnchors) != len(wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
	for i, anchor := range wantAnchors {
		if got.CorrelationAnchors[i] != anchor {
			t.Errorf("anchor[%d] = %q, want %q", i, got.CorrelationAnchors[i], anchor)
		}
	}
}

func TestExtractTargetHTTPSProxyMinimalNoSSLPolicy(t *testing.T) {
	// sslPolicy is optional: GCP applies the default TLS profile when it is
	// absent, so no edge or anchor should be emitted for it.
	const data = `{
		"name": "web-proxy",
		"urlMap": "https://www.googleapis.com/compute/v1/projects/demo-project/global/urlMaps/web-map",
		"sslCertificates": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/cert-1"
		],
		"quicOverride": "NONE",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeTargetHTTPSProxyUsesSSLPolicy {
			t.Fatalf("expected no ssl policy edge when sslPolicy is absent, got %#v", rel)
		}
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (url map, 1 ssl certificate), got %d: %#v", len(got.Relationships), got.Relationships)
	}
}

func TestExtractTargetHTTPSProxyEmptyDataProducesNoRelationships(t *testing.T) {
	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Fatalf("expected no attributes for an empty resource, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for an empty resource, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for an empty resource, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractTargetHTTPSProxyMalformedDataErrors(t *testing.T) {
	_, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractTargetHTTPSProxyDedupesRepeatedSSLCertificate(t *testing.T) {
	// The same certificate self-link listed twice must emit only one edge and
	// one anchor, mirroring the URL Map extractor's relationship dedupe.
	const data = `{
		"name": "web-proxy",
		"sslCertificates": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/cert-1",
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/cert-1"
		]
	}`

	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected deduped single ssl certificate edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
}

func TestExtractTargetHTTPSProxyCertificateMapSuppressesSSLCertificates(t *testing.T) {
	// The Compute API ignores sslCertificates when certificateMap is set, so
	// the extractor must emit only the CertificateMap edge/anchor and drop the
	// classic sslCertificates edges — otherwise it would surface a stale
	// relationship to a certificate GCP is not serving.
	const data = `{
		"name": "web-proxy",
		"urlMap": "https://www.googleapis.com/compute/v1/projects/demo-project/global/urlMaps/web-map",
		"sslCertificates": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/cert-1"
		],
		"certificateMap": "//certificatemanager.googleapis.com/projects/demo-project/locations/global/certificateMaps/web-map-certs",
		"quicOverride": "NONE",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeTargetHTTPSProxyUsesSSLCertificate {
			t.Fatalf("expected sslCertificates edges suppressed when certificateMap is set, got %#v", rel)
		}
	}

	wantMap := "//certificatemanager.googleapis.com/projects/demo-project/locations/global/certificateMaps/web-map-certs"
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesCertificateMap, wantMap, assetTypeCertificateManagerCertificateMap)

	// url map + certificate map only (no ssl certificate edge).
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (url map, certificate map), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	foundMapAnchor := false
	for _, anchor := range got.CorrelationAnchors {
		if anchor == wantMap {
			foundMapAnchor = true
		}
		if strings.Contains(anchor, "/sslCertificates/") {
			t.Fatalf("expected no sslCertificate anchor when certificateMap is set, got %q", anchor)
		}
	}
	if !foundMapAnchor {
		t.Fatalf("expected certificate map anchor %q, got %#v", wantMap, got.CorrelationAnchors)
	}
}

func TestExtractTargetHTTPSProxyCertificateManagerCertInSSLCertificates(t *testing.T) {
	// A Certificate Manager certificate listed in sslCertificates (no
	// certificateMap) resolves to a certificatemanager Certificate asset via
	// its own relationship type, while a classic Compute cert in the same list
	// resolves to a compute SslCertificate.
	const data = `{
		"name": "web-proxy",
		"sslCertificates": [
			"//certificatemanager.googleapis.com/projects/demo-project/locations/global/certificates/cm-cert",
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/sslCertificates/classic-cert"
		]
	}`

	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantCMCert := "//certificatemanager.googleapis.com/projects/demo-project/locations/global/certificates/cm-cert"
	wantClassic := "//compute.googleapis.com/projects/demo-project/global/sslCertificates/classic-cert"

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (cert-manager cert, classic ssl cert), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesCertManagerCertificate, wantCMCert, assetTypeCertificateManagerCertificate)
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesSSLCertificate, wantClassic, assetTypeComputeSSLCertificate)
}

func TestExtractTargetHTTPSProxyCertificateMapRelativePath(t *testing.T) {
	// certificateMap may arrive as a project/location-qualified relative path
	// (no //certificatemanager.googleapis.com/ prefix); it must still resolve
	// to the absolute CAI full resource name.
	const data = `{
		"name": "web-proxy",
		"certificateMap": "projects/demo-project/locations/global/certificateMaps/web-map-certs"
	}`

	got, err := extractTargetHTTPSProxy(targetHTTPSProxyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantMap := "//certificatemanager.googleapis.com/projects/demo-project/locations/global/certificateMaps/web-map-certs"
	assertRelationship(t, got.Relationships, relationshipTypeTargetHTTPSProxyUsesCertificateMap, wantMap, assetTypeCertificateManagerCertificateMap)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (certificate map), got %d: %#v", len(got.Relationships), got.Relationships)
	}
}
