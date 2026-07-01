// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const routeFullName = "//compute.googleapis.com/projects/demo-project/global/routes/to-instance"

func routeContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: routeFullName,
		AssetType:        assetTypeComputeRoute,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestRouteExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeRoute); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeRoute)
	}
}

func TestExtractRouteNextHopInstance(t *testing.T) {
	const data = `{
		"name": "to-instance",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc",
		"destRange": "10.128.0.0/9",
		"priority": 1000,
		"nextHopInstance": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instances/nat-gw",
		"tags": ["web", "web"],
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractRoute(routeContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"dest_prefix_length": int64(9),
		"priority":           int64(1000),
		"network_tags":       []string{"web"},
		"creation_time":      "2024-06-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const (
		network  = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"
		instance = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/nat-gw"
	)
	wantAnchors := []string{network, instance}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (network, instance), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRouteInNetwork, network, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeRouteNextHopInstance, instance, assetTypeComputeInstance)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != routeFullName || rel.SourceAssetType != assetTypeComputeRoute {
			t.Errorf("relationship source = %q/%q, want route", rel.SourceFullResourceName, rel.SourceAssetType)
		}
		if rel.SupportState != RelationshipSupportSupported {
			t.Errorf("relationship support state = %q, want %q", rel.SupportState, RelationshipSupportSupported)
		}
	}
}

func TestExtractRouteDefaultInternetGateway(t *testing.T) {
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"destRange": "0.0.0.0/0",
		"priority": 1000,
		"nextHopGateway": "projects/demo-project/global/gateways/default-internet-gateway"
	}`
	got, err := extractRoute(routeContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["dest_is_default"] != true {
		t.Errorf("dest_is_default = %v, want true", got.Attributes["dest_is_default"])
	}
	if got.Attributes["dest_prefix_length"] != int64(0) {
		t.Errorf("dest_prefix_length = %v, want 0", got.Attributes["dest_prefix_length"])
	}
	if got.Attributes["next_hop_gateway"] != "default-internet-gateway" {
		t.Errorf("next_hop_gateway = %v, want default-internet-gateway", got.Attributes["next_hop_gateway"])
	}
	// The gateway is not a resolvable CAI asset -> no edge, only the network edge.
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the network edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRouteInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractRouteVpnTunnelAndIlbEdges(t *testing.T) {
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"destRange": "192.168.0.0/16",
		"nextHopVpnTunnel": "projects/demo-project/regions/us-central1/vpnTunnels/tunnel-1",
		"nextHopIlb": "projects/demo-project/regions/us-central1/forwardingRules/ilb-fr"
	}`
	got, err := extractRoute(routeContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const (
		vpnTunnel = "//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnTunnels/tunnel-1"
		ilb       = "//compute.googleapis.com/projects/demo-project/regions/us-central1/forwardingRules/ilb-fr"
	)
	assertRelationship(t, got.Relationships, relationshipTypeRouteNextHopVpnTunnel, vpnTunnel, assetTypeComputeVpnTunnel)
	assertRelationship(t, got.Relationships, relationshipTypeRouteNextHopIlb, ilb, assetTypeComputeForwardingRule)
	if got.Attributes["dest_prefix_length"] != int64(16) {
		t.Errorf("dest_prefix_length = %v, want 16", got.Attributes["dest_prefix_length"])
	}
}

func TestExtractRouteProjectlessNextHops(t *testing.T) {
	// The Route REST contract allows project-less next-hop forms
	// (regions/r/forwardingRules/fr, zones/z/instances/i); they must resolve
	// against the route's project rather than being dropped.
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"nextHopInstance": "zones/us-central1-a/instances/nat-gw",
		"nextHopIlb": "regions/us-central1/forwardingRules/ilb-fr"
	}`
	got, err := extractRoute(routeContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRouteNextHopInstance,
		"//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/nat-gw", assetTypeComputeInstance)
	assertRelationship(t, got.Relationships, relationshipTypeRouteNextHopIlb,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/forwardingRules/ilb-fr", assetTypeComputeForwardingRule)
}

func TestExtractRouteIPv6DefaultRoute(t *testing.T) {
	for _, dest := range []string{"::/0", " ::/0 "} {
		const netRef = "projects/p/global/networks/n"
		got, err := extractRoute(routeContext(`{"network":"` + netRef + `","destRange":"` + dest + `"}`))
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", dest, err)
		}
		if got.Attributes["dest_is_default"] != true {
			t.Errorf("destRange %q: dest_is_default = %v, want true", dest, got.Attributes["dest_is_default"])
		}
	}
}

func TestExtractRouteNextHopIPNeverLeaks(t *testing.T) {
	const data = `{
		"network": "projects/p/global/networks/n",
		"destRange": "10.5.6.7/32",
		"nextHopIp": "10.9.9.9"
	}`
	got, err := extractRoute(routeContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["has_next_hop_ip"] != true {
		t.Errorf("has_next_hop_ip = %v, want true", got.Attributes["has_next_hop_ip"])
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"10.9.9.9", "10.5.6.7"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked an IP value %q: %s", forbidden, blob)
		}
	}
}

func TestExtractRouteDropsNonResolvableNextHops(t *testing.T) {
	// A next-hop ILB given as a bare IP, and a next-hop instance given as an
	// unrecognized token, resolve to no CAI endpoint: no edge, no anchor, and the
	// raw value never reaches the extraction output.
	const data = `{
		"network": "projects/p/global/networks/n",
		"nextHopIlb": "10.0.0.250",
		"nextHopInstance": "garbage-token"
	}`
	got, err := extractRoute(routeContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeRouteNextHopIlb || rel.RelationshipType == relationshipTypeRouteNextHopInstance {
			t.Errorf("unexpected next-hop edge for unresolvable ref: %#v", rel)
		}
	}
	// Only the network edge resolves.
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the network edge, got %#v", got.Relationships)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"10.0.0.250", "garbage-token"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked an unresolvable next-hop value %q: %s", forbidden, blob)
		}
	}
}

func TestRouteGatewayLeafName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"self link", "https://www.googleapis.com/compute/v1/projects/p/global/gateways/default-internet-gateway", "default-internet-gateway"},
		{"partial", "projects/p/global/gateways/default-internet-gateway", "default-internet-gateway"},
		{"suffix after leaf", "projects/p/global/gateways/default-internet-gateway/extra", "default-internet-gateway"},
		{"bare name", "default-internet-gateway", "default-internet-gateway"},
		{"unrelated path", "foo/bar", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatewayLeafName(tc.in); got != tc.want {
				t.Errorf("gatewayLeafName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractRouteEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractRoute(routeContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected nothing for empty data, got %#v", got)
	}
}

func TestExtractRouteMalformedDataErrors(t *testing.T) {
	if _, err := extractRoute(routeContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
