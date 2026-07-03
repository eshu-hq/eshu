// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const backendServiceFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/backendServices/internal-backend"

func backendServiceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: backendServiceFullName,
		AssetType:        assetTypeComputeBackendService,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBackendServiceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeBackendService); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeBackendService)
	}
}

func TestBackendServiceExtractorIsRegisteredForRegionAssetType(t *testing.T) {
	// The CAI list/export/monitor/query path this collector uses emits
	// regional backend services under the distinct RegionBackendService asset
	// type; it must dispatch to the same extractor as the global/search-API
	// BackendService asset type, mirroring the
	// ForwardingRule/GlobalForwardingRule registration.
	got, ok := lookupAssetExtractor(assetTypeComputeRegionBackendService)
	if !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeRegionBackendService)
	}
	want, ok := lookupAssetExtractor(assetTypeComputeBackendService)
	if !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeBackendService)
	}
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
		t.Fatalf("expected %q and %q to dispatch to the same extractor function", assetTypeComputeRegionBackendService, assetTypeComputeBackendService)
	}
}

func TestExtractBackendServiceFullAttributes(t *testing.T) {
	const data = `{
		"name": "internal-backend",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"protocol": "HTTP",
		"loadBalancingScheme": "INTERNAL_MANAGED",
		"portName": "http",
		"timeoutSec": 30,
		"enableCDN": true,
		"sessionAffinity": "CLIENT_IP",
		"securityPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/securityPolicies/waf-policy",
		"healthChecks": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/healthChecks/http-hc"
		],
		"backends": [
			{"group": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instanceGroups/ig-1"},
			{"group": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/networkEndpointGroups/neg-1"}
		],
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":                "us-central1",
		"protocol":              "HTTP",
		"load_balancing_scheme": "INTERNAL_MANAGED",
		"port_name":             "http",
		"timeout_sec":           int64(30),
		"enable_cdn":            true,
		"session_affinity":      "CLIENT_IP",
		"backend_count":         2,
		"creation_time":         "2024-06-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantPolicy := "//compute.googleapis.com/projects/demo-project/global/securityPolicies/waf-policy"
	wantHealthCheck := "//compute.googleapis.com/projects/demo-project/global/healthChecks/http-hc"
	wantInstanceGroup := "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instanceGroups/ig-1"
	wantNEG := "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/networkEndpointGroups/neg-1"

	if len(got.Relationships) != 4 {
		t.Fatalf("expected 4 edges (security policy, health check, 2 backends), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesSecurityPolicy, wantPolicy, assetTypeComputeSecurityPolicy)
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesHealthCheck, wantHealthCheck, assetTypeComputeHealthCheck)
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceHasBackend, wantInstanceGroup, assetTypeComputeInstanceGroup)
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceHasBackend, wantNEG, assetTypeComputeNetworkEndpointGroup)

	wantAnchors := []string{wantPolicy, wantHealthCheck, wantInstanceGroup, wantNEG}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractBackendServiceGlobalOmitsRegion(t *testing.T) {
	const data = `{
		"name": "global-backend",
		"protocol": "HTTPS",
		"loadBalancingScheme": "EXTERNAL_MANAGED"
	}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["region"]; ok {
		t.Errorf("region should be omitted for a global backend service, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships, got %#v", got.Relationships)
	}
}

func TestExtractBackendServiceEdgeSecurityPolicyOnlyEmitsEdge(t *testing.T) {
	// A backend service protected only by an edge security policy (no regular
	// securityPolicy) must still emit a security-policy anchor and edge —
	// edgeSecurityPolicy is a distinct Compute field from securityPolicy.
	const data = `{
		"name": "edge-armor-backend",
		"edgeSecurityPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/securityPolicies/edge-waf-policy"
	}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantEdgePolicy := "//compute.googleapis.com/projects/demo-project/global/securityPolicies/edge-waf-policy"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (edge security policy), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesEdgeSecurityPolicy, wantEdgePolicy, assetTypeComputeSecurityPolicy)

	wantAnchors := []string{wantEdgePolicy}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractBackendServiceBothSecurityPoliciesEmitBothEdges(t *testing.T) {
	// A backend service can carry both a regular and an edge security policy
	// simultaneously; both must resolve to independent edges/anchors.
	const data = `{
		"name": "both-armor-backend",
		"securityPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/securityPolicies/waf-policy",
		"edgeSecurityPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/securityPolicies/edge-waf-policy"
	}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPolicy := "//compute.googleapis.com/projects/demo-project/global/securityPolicies/waf-policy"
	wantEdgePolicy := "//compute.googleapis.com/projects/demo-project/global/securityPolicies/edge-waf-policy"
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (security policy + edge security policy), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesSecurityPolicy, wantPolicy, assetTypeComputeSecurityPolicy)
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesEdgeSecurityPolicy, wantEdgePolicy, assetTypeComputeSecurityPolicy)
}

func TestExtractBackendServiceMultipleHealthChecks(t *testing.T) {
	const data = `{
		"name": "multi-hc-backend",
		"healthChecks": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/global/healthChecks/hc-1",
			"https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/healthChecks/hc-2"
		]
	}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantHC1 := "//compute.googleapis.com/projects/demo-project/global/healthChecks/hc-1"
	wantHC2 := "//compute.googleapis.com/projects/demo-project/regions/us-central1/healthChecks/hc-2"
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 health-check edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesHealthCheck, wantHC1, assetTypeComputeHealthCheck)
	assertRelationship(t, got.Relationships, relationshipTypeBackendServiceUsesHealthCheck, wantHC2, assetTypeComputeHealthCheck)
}

func TestExtractBackendServiceUnresolvableBackendGroupOmitted(t *testing.T) {
	const data = `{
		"name": "bad-backend",
		"backends": [
			{"group": ""},
			{"group": "not-a-valid-reference"}
		]
	}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for unresolvable backend groups, got %#v", got.Relationships)
	}
	// backend_count reflects raw entries even when unresolvable, since it is a
	// Terraform/drift-useful cardinality signal independent of edge resolution.
	if got.Attributes["backend_count"] != 2 {
		t.Errorf("backend_count = %v, want 2", got.Attributes["backend_count"])
	}
}

func TestExtractBackendServicePartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractBackendService(backendServiceContext(`{}`))
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

func TestExtractBackendServiceMalformedDataErrors(t *testing.T) {
	if _, err := extractBackendService(backendServiceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractBackendServiceEnableCDNFalseIsKept(t *testing.T) {
	// enableCDN is a *bool: an explicit false must be distinguishable from an
	// absent field, since false is a meaningful posture (CDN disabled) rather
	// than a zero-value default that should be omitted.
	const data = `{"name": "no-cdn-backend", "enableCDN": false}`

	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := got.Attributes["enable_cdn"]
	if !ok {
		t.Fatalf("expected enable_cdn to be present when explicitly false, got %#v", got.Attributes)
	}
	if v != false {
		t.Errorf("enable_cdn = %v, want false", v)
	}
}

func TestExtractBackendServiceNeverPersistsRawSecurityPolicyOrBackendData(t *testing.T) {
	const data = `{
		"name": "leak-check-backend",
		"backends": [
			{"group": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instanceGroups/ig-1", "balancingMode": "UTILIZATION", "maxUtilization": 0.8}
		],
		"securityPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/securityPolicies/waf-policy",
		"iap": {"enabled": true, "oauth2ClientSecret": "super-secret-value"}
	}`
	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "super-secret-value") {
		t.Fatalf("backend service extraction leaked IAP oauth2ClientSecret: %s", blob)
	}
	if containsString(string(blob), "UTILIZATION") {
		t.Fatalf("backend service extraction leaked raw balancingMode: %s", blob)
	}
}

func TestExtractBackendServiceAdversarialRedactionSweep(t *testing.T) {
	// Full-struct JSON marshal + banned-token sweep per repo convention: any
	// secret-shaped or data-plane token anywhere in the extraction output is a
	// redaction failure regardless of which field it leaked through.
	const data = `{
		"name": "adversarial-backend",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"protocol": "HTTPS",
		"loadBalancingScheme": "EXTERNAL_MANAGED",
		"portName": "https",
		"timeoutSec": 60,
		"enableCDN": true,
		"sessionAffinity": "GENERATED_COOKIE",
		"securityPolicy": "https://www.googleapis.com/compute/v1/projects/demo-project/global/securityPolicies/waf-policy",
		"healthChecks": ["https://www.googleapis.com/compute/v1/projects/demo-project/global/healthChecks/http-hc"],
		"backends": [{"group": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instanceGroups/ig-1"}],
		"iap": {"enabled": true, "oauth2ClientId": "client-id-value", "oauth2ClientSecret": "AKIA_FAKE_SECRET_VALUE"},
		"cdnPolicy": {"signedUrlKeyNames": ["key-1"], "cacheKeyPolicy": {"includeHost": true}},
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`
	got, err := extractBackendService(backendServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	bannedTokens := []string{
		"AKIA_FAKE_SECRET_VALUE",
		"client-id-value",
		"oauth2ClientSecret",
		"oauth2ClientId",
		"signedUrlKeyNames",
		"key-1",
		"cacheKeyPolicy",
		"iap",
	}
	for _, token := range bannedTokens {
		if containsString(string(blob), token) {
			t.Errorf("extraction output leaked banned token %q: %s", token, blob)
		}
	}
}
