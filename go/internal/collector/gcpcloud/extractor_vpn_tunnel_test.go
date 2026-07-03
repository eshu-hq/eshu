// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const vpnTunnelFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnTunnels/tunnel-1"

func vpnTunnelContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: vpnTunnelFullName,
		AssetType:        assetTypeComputeVpnTunnel,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestVpnTunnelExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeVpnTunnel); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeVpnTunnel)
	}
}

func TestExtractVpnTunnelHAGatewayAndPeerGcpGateway(t *testing.T) {
	// peerGcpGateway is the HA-VPN peer-to-peer topology: both this tunnel's own
	// gateway and its peer are compute.googleapis.com/VpnGateway resources (as
	// opposed to peerExternalGateway, which names an ExternalVpnGateway peer).
	const data = `{
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"vpnGateway": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1",
		"vpnGatewayInterface": 0,
		"peerGcpGateway": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/vpnGateways/peer-gw-1",
		"router": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/routers/router-1",
		"ikeVersion": 2,
		"status": "ESTABLISHED",
		"localTrafficSelector": ["10.0.0.0/16"],
		"remoteTrafficSelector": ["192.168.0.0/16", "192.168.1.0/24"],
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":                        "us-central1",
		"vpn_gateway_interface":         int64(0),
		"ike_version":                   int64(2),
		"status":                        "ESTABLISHED",
		"local_traffic_selector_count":  int64(1),
		"remote_traffic_selector_count": int64(2),
		"creation_time":                 "2024-06-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const (
		gateway = "//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1"
		peer    = "//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnGateways/peer-gw-1"
		router  = "//compute.googleapis.com/projects/demo-project/regions/us-central1/routers/router-1"
	)
	wantAnchors := []string{gateway, peer, router}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
	if len(got.Relationships) != 3 {
		t.Fatalf("expected 3 edges (vpn gateway, peer gcp gateway, router), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelUsesVpnGateway, gateway, assetTypeComputeVPNGateway)
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelPeersWithVpnGateway, peer, assetTypeComputeVPNGateway)
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelUsesRouter, router, assetTypeComputeRouter)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != vpnTunnelFullName || rel.SourceAssetType != assetTypeComputeVpnTunnel {
			t.Errorf("relationship source = %q/%q, want vpn tunnel", rel.SourceFullResourceName, rel.SourceAssetType)
		}
		if rel.SupportState != RelationshipSupportSupported {
			t.Errorf("relationship support state = %q, want %q", rel.SupportState, RelationshipSupportSupported)
		}
	}
}

func TestExtractVpnTunnelClassicTargetGatewayAndPeerExternalGateway(t *testing.T) {
	const data = `{
		"region": "projects/demo-project/regions/us-central1",
		"targetVpnGateway": "projects/demo-project/regions/us-central1/targetVpnGateways/classic-gw-1",
		"peerExternalGateway": "projects/demo-project/regions/us-central1/externalVpnGateways/peer-gw-2",
		"peerExternalGatewayInterface": 1,
		"ikeVersion": 1,
		"status": "FIRST_HANDSHAKE"
	}`

	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const (
		targetGW = "//compute.googleapis.com/projects/demo-project/regions/us-central1/targetVpnGateways/classic-gw-1"
		peerGW   = "//compute.googleapis.com/projects/demo-project/regions/us-central1/externalVpnGateways/peer-gw-2"
	)
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelUsesTargetVpnGateway, targetGW, assetTypeComputeTargetVPNGateway)
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelPeersWithVpnGateway, peerGW, assetTypeComputeExternalVPNGateway)
	if len(got.Relationships) != 2 {
		t.Fatalf("expected exactly 2 edges (classic target gateway + peer external gateway), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	if got.Attributes["peer_external_gateway_interface"] != int64(1) {
		t.Errorf("peer_external_gateway_interface = %v, want 1", got.Attributes["peer_external_gateway_interface"])
	}
	if got.Attributes["ike_version"] != int64(1) {
		t.Errorf("ike_version = %v, want 1", got.Attributes["ike_version"])
	}
	if got.Attributes["status"] != "FIRST_HANDSHAKE" {
		t.Errorf("status = %v, want FIRST_HANDSHAKE", got.Attributes["status"])
	}
	// No router configured -> no dynamic-routing edge or anchor.
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeVpnTunnelUsesRouter {
			t.Errorf("unexpected router edge with no router configured: %#v", rel)
		}
	}
}

func TestExtractVpnTunnelNoRouterMeansNoDynamicRoutingEdge(t *testing.T) {
	const data = `{
		"region": "projects/demo-project/regions/us-central1",
		"vpnGateway": "projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1",
		"status": "ESTABLISHED"
	}`
	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 edge (vpn gateway only), got %#v", got.Relationships)
	}
	if _, ok := got.Attributes["router"]; ok {
		t.Errorf("router attribute should not be fabricated when absent")
	}
}

func TestExtractVpnTunnelPeerIPNeverLeaks(t *testing.T) {
	const data = `{
		"region": "projects/demo-project/regions/us-central1",
		"vpnGateway": "projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1",
		"peerIp": "203.0.113.5",
		"sharedSecret": "top-secret-psk",
		"sharedSecretHash": "abc123hash",
		"status": "ESTABLISHED"
	}`
	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"203.0.113.5", "top-secret-psk", "abc123hash"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked a forbidden value %q: %s", forbidden, blob)
		}
	}
	if _, ok := got.Attributes["peer_ip"]; ok {
		t.Errorf("peer_ip must never be captured as an attribute")
	}
}

func TestExtractVpnTunnelTrafficSelectorsNeverLeakCIDRs(t *testing.T) {
	const data = `{
		"region": "projects/demo-project/regions/us-central1",
		"vpnGateway": "projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1",
		"localTrafficSelector": ["10.55.0.0/16"],
		"remoteTrafficSelector": ["172.16.99.0/24"],
		"status": "ESTABLISHED"
	}`
	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"10.55.0.0", "172.16.99.0"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked a traffic-selector CIDR %q: %s", forbidden, blob)
		}
	}
	if got.Attributes["local_traffic_selector_count"] != int64(1) {
		t.Errorf("local_traffic_selector_count = %v, want 1", got.Attributes["local_traffic_selector_count"])
	}
	if got.Attributes["remote_traffic_selector_count"] != int64(1) {
		t.Errorf("remote_traffic_selector_count = %v, want 1", got.Attributes["remote_traffic_selector_count"])
	}
}

func TestExtractVpnTunnelBareGatewayReferenceResolvesAgainstProject(t *testing.T) {
	// The Google-supported project-less/regionless partial form must resolve
	// against the tunnel's own project, mirroring Route's next-hop handling.
	const data = `{
		"region": "projects/demo-project/regions/us-central1",
		"vpnGateway": "regions/us-central1/vpnGateways/ha-gw-1",
		"router": "regions/us-central1/routers/router-1"
	}`
	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelUsesVpnGateway,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1", assetTypeComputeVPNGateway)
	assertRelationship(t, got.Relationships, relationshipTypeVpnTunnelUsesRouter,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/routers/router-1", assetTypeComputeRouter)
}

func TestExtractVpnTunnelDropsUnresolvableGatewayReferences(t *testing.T) {
	const data = `{
		"region": "projects/demo-project/regions/us-central1",
		"vpnGateway": "garbage-token",
		"status": "FAILED"
	}`
	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges for an unresolvable gateway reference, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for an unresolvable gateway reference, got %#v", got.CorrelationAnchors)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "garbage-token") {
		t.Fatalf("extraction leaked the unresolvable raw reference: %s", blob)
	}
}

// TestExtractVpnTunnelWrongSegmentReferencesProduceNoEdge proves each of the
// five gateway/router reference fields is checked against its own expected
// compute resource-path segment via computeResourceFullNameFromSelfLink, not
// merely resolved as any well-formed compute selfLink. A resolvable-but-wrong
// -kind reference (for example an externalVpnGateways selfLink placed in
// peerGcpGateway, which expects vpnGateways) must never emit the field's edge
// or anchor, matching the sibling ForwardingRule/Router segment-validation
// fix for the identical root-cause finding: without this check, a wrong
// selfLink placed in the wrong field would materialize a supported edge with
// the wrong target asset type.
func TestExtractVpnTunnelWrongSegmentReferencesProduceNoEdge(t *testing.T) {
	const routerSelfLink = "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/routers/router-1"
	const vpnGatewaySelfLink = "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/vpnGateways/gw-1"
	const targetVpnGatewaySelfLink = "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/targetVpnGateways/gw-1"
	const externalVpnGatewaySelfLink = "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/externalVpnGateways/gw-1"

	cases := []struct {
		name             string
		data             string
		wantRelationship string
	}{
		{
			name:             "vpnGateway field with a router selfLink",
			data:             `{"vpnGateway": "` + routerSelfLink + `"}`,
			wantRelationship: relationshipTypeVpnTunnelUsesVpnGateway,
		},
		{
			name:             "targetVpnGateway field with an HA vpnGateway selfLink",
			data:             `{"targetVpnGateway": "` + vpnGatewaySelfLink + `"}`,
			wantRelationship: relationshipTypeVpnTunnelUsesTargetVpnGateway,
		},
		{
			name:             "peerGcpGateway field with an externalVpnGateway selfLink",
			data:             `{"peerGcpGateway": "` + externalVpnGatewaySelfLink + `"}`,
			wantRelationship: relationshipTypeVpnTunnelPeersWithVpnGateway,
		},
		{
			name:             "peerExternalGateway field with a Classic targetVpnGateway selfLink",
			data:             `{"peerExternalGateway": "` + targetVpnGatewaySelfLink + `"}`,
			wantRelationship: relationshipTypeVpnTunnelPeersWithVpnGateway,
		},
		{
			name:             "router field with an HA vpnGateway selfLink",
			data:             `{"router": "` + vpnGatewaySelfLink + `"}`,
			wantRelationship: relationshipTypeVpnTunnelUsesRouter,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractVpnTunnel(vpnTunnelContext(tc.data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.Relationships) != 0 {
				t.Fatalf("expected no edge for a wrong-segment reference, got %#v", got.Relationships)
			}
			if len(got.CorrelationAnchors) != 0 {
				t.Fatalf("expected no anchor for a wrong-segment reference, got %#v", got.CorrelationAnchors)
			}
			for _, rel := range got.Relationships {
				if rel.RelationshipType == tc.wantRelationship {
					t.Fatalf("unexpected %q edge for a wrong-segment reference: %#v", tc.wantRelationship, rel)
				}
			}
		})
	}
}

func TestExtractVpnTunnelEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractVpnTunnel(vpnTunnelContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected nothing for empty data, got %#v", got)
	}
}

func TestExtractVpnTunnelMalformedDataErrors(t *testing.T) {
	if _, err := extractVpnTunnel(vpnTunnelContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

// TestExtractVpnTunnelFullStructNeverLeaksBannedTokens is the adversarial
// redaction test: it marshals the entire AttributeExtraction to JSON for a
// resource.data blob carrying every sensitive field this asset type can report
// (peer IP, pre-shared key material, and traffic-selector CIDRs) and greps the
// serialized output for each banned raw value, mirroring the Route extractor's
// TestExtractRouteNextHopIPNeverLeaks adversarial pattern.
func TestExtractVpnTunnelFullStructNeverLeaksBannedTokens(t *testing.T) {
	const data = `{
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"vpnGateway": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/vpnGateways/ha-gw-1",
		"vpnGatewayInterface": 0,
		"peerGcpGateway": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/vpnGateways/peer-gw-1",
		"peerIp": "198.51.100.42",
		"router": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/routers/router-1",
		"ikeVersion": 2,
		"status": "ESTABLISHED",
		"localTrafficSelector": ["10.0.0.0/8"],
		"remoteTrafficSelector": ["172.31.0.0/16"],
		"sharedSecret": "correct-horse-battery-staple",
		"sharedSecretHash": "deadbeef0123456789",
		"detailedStatus": "Tunnel is up and running with peer 198.51.100.42",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`
	got, err := extractVpnTunnel(vpnTunnelContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal full extraction: %v", err)
	}
	banned := []string{
		"198.51.100.42",
		"correct-horse-battery-staple",
		"deadbeef0123456789",
		"10.0.0.0/8",
		"172.31.0.0/16",
		"Tunnel is up and running",
	}
	for _, forbidden := range banned {
		if containsString(string(blob), forbidden) {
			t.Fatalf("full-struct adversarial scan: extraction leaked banned token %q: %s", forbidden, blob)
		}
	}
}
