// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const forwardingRuleFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/forwardingRules/frontend-lb"

func forwardingRuleContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: forwardingRuleFullName,
		AssetType:        assetTypeComputeForwardingRule,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestForwardingRuleExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeForwardingRule); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeForwardingRule)
	}
}

func TestForwardingRuleExtractorIsRegisteredForGlobalAssetType(t *testing.T) {
	// Cloud Asset Inventory reports global load-balancer frontends as the
	// distinct GlobalForwardingRule asset type; it must dispatch to the same
	// extractor as the regional ForwardingRule asset type, mirroring the
	// Address/GlobalAddress registration.
	got, ok := lookupAssetExtractor(assetTypeComputeGlobalForwardingRule)
	if !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeGlobalForwardingRule)
	}
	want, ok := lookupAssetExtractor(assetTypeComputeForwardingRule)
	if !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeForwardingRule)
	}
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
		t.Fatalf("expected %q and %q to dispatch to the same extractor function", assetTypeComputeGlobalForwardingRule, assetTypeComputeForwardingRule)
	}
}

func TestExtractForwardingRuleExternalToTargetPool(t *testing.T) {
	const data = `{
		"name": "frontend-lb",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"loadBalancingScheme": "EXTERNAL",
		"IPAddress": "203.0.113.10",
		"IPProtocol": "TCP",
		"portRange": "80-80",
		"target": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/targetPools/frontend-pool",
		"network": "projects/demo-project/global/networks/main-vpc",
		"subnetwork": "projects/demo-project/regions/us-central1/subnetworks/private-subnet",
		"ipVersion": "IPV4",
		"allPorts": false,
		"networkTier": "PREMIUM",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":                "us-central1",
		"load_balancing_scheme": "EXTERNAL",
		"is_external":           true,
		"ip_protocol":           "TCP",
		"port_range":            "80-80",
		"ip_version":            "IPV4",
		"all_ports":             false,
		"network_tier":          "PREMIUM",
		"creation_time":         "2024-06-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantTargetPool := "//compute.googleapis.com/projects/demo-project/regions/us-central1/targetPools/frontend-pool"
	wantNetwork := "//compute.googleapis.com/projects/demo-project/global/networks/main-vpc"
	wantSubnet := "//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/private-subnet"

	wantAnchors := []string{wantTargetPool, wantNetwork, wantSubnet}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 3 {
		t.Fatalf("expected target, network, and subnetwork edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsTargetPool, wantTargetPool, assetTypeComputeTargetPool)
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleInNetwork, wantNetwork, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleInSubnetwork, wantSubnet, assetTypeComputeSubnetwork)
}

func TestExtractForwardingRuleTargetsBackendService(t *testing.T) {
	const data = `{
		"name": "internal-lb",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"loadBalancingScheme": "INTERNAL",
		"IPProtocol": "TCP",
		"ports": ["80", "443"],
		"backendService": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/backendServices/internal-backend",
		"network": "projects/demo-project/global/networks/main-vpc",
		"subnetwork": "projects/demo-project/regions/us-central1/subnetworks/private-subnet"
	}`

	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["load_balancing_scheme"] != "INTERNAL" {
		t.Errorf("load_balancing_scheme = %v, want INTERNAL", got.Attributes["load_balancing_scheme"])
	}
	if got.Attributes["is_external"] != false {
		t.Errorf("is_external = %v, want false", got.Attributes["is_external"])
	}
	wantPorts := []string{"80", "443"}
	if !reflect.DeepEqual(got.Attributes["ports"], wantPorts) {
		t.Errorf("ports = %#v, want %#v", got.Attributes["ports"], wantPorts)
	}

	wantBackend := "//compute.googleapis.com/projects/demo-project/regions/us-central1/backendServices/internal-backend"
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsBackendService, wantBackend, assetTypeComputeBackendService)
}

func TestExtractForwardingRuleTargetsTargetHTTPSProxy(t *testing.T) {
	const data = `{
		"name": "global-lb",
		"loadBalancingScheme": "EXTERNAL_MANAGED",
		"IPProtocol": "TCP",
		"portRange": "443-443",
		"target": "https://www.googleapis.com/compute/v1/projects/demo-project/global/targetHttpsProxies/global-proxy"
	}`

	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Global forwarding rules carry no region segment; region is omitted rather
	// than fabricated.
	if _, ok := got.Attributes["region"]; ok {
		t.Errorf("region should be omitted for a global forwarding rule, got %#v", got.Attributes)
	}

	wantProxy := "//compute.googleapis.com/projects/demo-project/global/targetHttpsProxies/global-proxy"
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsTargetProxy, wantProxy, assetTypeComputeTargetHTTPSProxy)
}

func TestExtractForwardingRuleTargetsTargetInstance(t *testing.T) {
	const data = `{
		"name": "packet-mirror-fr",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"loadBalancingScheme": "",
		"IPProtocol": "TCP",
		"target": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/targetInstances/protocol-fwd-target"
	}`

	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantTarget := "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/targetInstances/protocol-fwd-target"
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsTargetInstance, wantTarget, assetTypeComputeTargetInstance)
	if !containsStringSlice(got.CorrelationAnchors, wantTarget) {
		t.Errorf("expected correlation anchor for target instance, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractForwardingRuleTargetsTargetVPNGateway(t *testing.T) {
	const data = `{
		"name": "classic-vpn-fr",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"IPProtocol": "ESP",
		"target": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/targetVpnGateways/classic-vpn-gw"
	}`

	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantTarget := "//compute.googleapis.com/projects/demo-project/regions/us-central1/targetVpnGateways/classic-vpn-gw"
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsTargetVPNGateway, wantTarget, assetTypeComputeTargetVPNGateway)
}

func TestExtractForwardingRuleTargetsServiceAttachment(t *testing.T) {
	const data = `{
		"name": "psc-consumer-fr",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"loadBalancingScheme": "INTERNAL",
		"IPProtocol": "TCP",
		"target": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/serviceAttachments/psc-producer-sa"
	}`

	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantTarget := "//compute.googleapis.com/projects/demo-project/regions/us-central1/serviceAttachments/psc-producer-sa"
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsServiceAttachment, wantTarget, assetTypeComputeServiceAttachment)
}

func TestExtractGlobalForwardingRuleUsesSameExtractor(t *testing.T) {
	const data = `{
		"name": "global-lb",
		"loadBalancingScheme": "EXTERNAL_MANAGED",
		"IPProtocol": "TCP",
		"portRange": "443-443",
		"target": "https://www.googleapis.com/compute/v1/projects/demo-project/global/targetHttpsProxies/global-proxy"
	}`
	ctx := forwardingRuleContext(data)
	ctx.AssetType = assetTypeComputeGlobalForwardingRule
	ctx.FullResourceName = "//compute.googleapis.com/projects/demo-project/global/forwardingRules/global-lb"

	got, err := extractForwardingRule(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantProxy := "//compute.googleapis.com/projects/demo-project/global/targetHttpsProxies/global-proxy"
	assertRelationship(t, got.Relationships, relationshipTypeForwardingRuleTargetsTargetProxy, wantProxy, assetTypeComputeTargetHTTPSProxy)
}

func TestExtractForwardingRuleNeverPersistsIPAddress(t *testing.T) {
	const data = `{
		"name": "frontend-lb",
		"IPAddress": "203.0.113.10",
		"loadBalancingScheme": "EXTERNAL",
		"target": "projects/demo-project/regions/us-central1/targetPools/frontend-pool"
	}`
	got, err := extractForwardingRule(forwardingRuleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "203.0.113.10") {
		t.Fatalf("forwarding rule extraction leaked IP address: %s", blob)
	}
	if _, ok := got.Attributes["ip_address"]; ok {
		t.Errorf("ip_address must never be persisted: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["IPAddress"]; ok {
		t.Errorf("IPAddress must never be persisted: %#v", got.Attributes)
	}
}

func TestExtractForwardingRulePartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractForwardingRule(forwardingRuleContext(`{}`))
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

func TestExtractForwardingRuleMalformedDataErrors(t *testing.T) {
	if _, err := extractForwardingRule(forwardingRuleContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractForwardingRuleTargetKindDispatch(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		wantType   string
		wantRelKey string
	}{
		{"target pool", "projects/p/regions/us-central1/targetPools/tp", assetTypeComputeTargetPool, relationshipTypeForwardingRuleTargetsTargetPool},
		{"target http proxy", "projects/p/global/targetHttpProxies/thp", assetTypeComputeTargetHTTPProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
		{"target https proxy", "projects/p/global/targetHttpsProxies/ths", assetTypeComputeTargetHTTPSProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
		{"target tcp proxy", "projects/p/global/targetTcpProxies/ttp", assetTypeComputeTargetTCPProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
		{"target ssl proxy", "projects/p/global/targetSslProxies/tsp", assetTypeComputeTargetSSLProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
		{"target grpc proxy", "projects/p/global/targetGrpcProxies/tgp", assetTypeComputeTargetGRPCProxy, relationshipTypeForwardingRuleTargetsTargetProxy},
		{"target instance", "projects/p/zones/us-central1-a/targetInstances/ti", assetTypeComputeTargetInstance, relationshipTypeForwardingRuleTargetsTargetInstance},
		{"target vpn gateway", "projects/p/regions/us-central1/targetVpnGateways/tvg", assetTypeComputeTargetVPNGateway, relationshipTypeForwardingRuleTargetsTargetVPNGateway},
		{"service attachment", "projects/p/regions/us-central1/serviceAttachments/sa", assetTypeComputeServiceAttachment, relationshipTypeForwardingRuleTargetsServiceAttachment},
		{"unrecognized", "projects/p/global/somethingElse/x", "", ""},
		{"blank", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotRelType, gotName := forwardingRuleTargetEdge(tc.target, "p")
			if tc.wantType == "" {
				if gotName != "" {
					t.Errorf("expected no resolvable target edge, got type=%q rel=%q name=%q", gotType, gotRelType, gotName)
				}
				return
			}
			if gotType != tc.wantType || gotRelType != tc.wantRelKey {
				t.Errorf("forwardingRuleTargetEdge(%q) = (%q,%q), want (%q,%q)", tc.target, gotType, gotRelType, tc.wantType, tc.wantRelKey)
			}
		})
	}
}
