// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

const routerFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/routers/edge-router"

func routerContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: routerFullName,
		AssetType:        assetTypeComputeRouter,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestRouterExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeRouter); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeRouter)
	}
}

func TestExtractRouterBasicAttributesAndNetworkEdge(t *testing.T) {
	const data = `{
		"name": "edge-router",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc",
		"encryptedInterconnectRouter": true,
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"bgp": {
			"asn": 65001,
			"advertiseMode": "DEFAULT"
		}
	}`

	got, err := extractRouter(routerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":                        "us-central1",
		"bgp_asn":                       int64(65001),
		"bgp_advertise_mode":            "DEFAULT",
		"encrypted_interconnect_router": true,
		"creation_time":                 "2024-06-01T07:00:00Z",
	}
	for k, v := range wantAttrs {
		if got.Attributes[k] != v {
			t.Errorf("attributes[%q] = %#v, want %#v", k, got.Attributes[k], v)
		}
	}

	const network = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"
	assertRelationship(t, got.Relationships, relationshipTypeRouterInNetwork, network, assetTypeComputeNetwork)
	if !containsStringSlice(got.CorrelationAnchors, network) {
		t.Errorf("expected network anchor %q in %#v", network, got.CorrelationAnchors)
	}
}

func TestExtractRouterBgpPeerSummaryNeverLeaksAddresses(t *testing.T) {
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"bgp": {"asn": 65001},
		"bgpPeers": [
			{
				"name": "peer-1",
				"peerAsn": 65002,
				"interfaceName": "if-0",
				"ipAddress": "169.254.0.1",
				"peerIpAddress": "169.254.0.2"
			},
			{
				"name": "peer-2",
				"peerAsn": 65003,
				"interfaceName": "if-1",
				"ipAddress": "169.254.1.1",
				"peerIpAddress": "169.254.1.2"
			}
		]
	}`

	got, err := extractRouter(routerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["bgp_peer_count"] != 2 {
		t.Fatalf("bgp_peer_count = %v, want 2", got.Attributes["bgp_peer_count"])
	}

	peers, ok := got.Attributes["bgp_peers"].([]map[string]any)
	if !ok || len(peers) != 2 {
		t.Fatalf("bgp_peers = %#v, want 2 bounded peer summaries", got.Attributes["bgp_peers"])
	}
	if peers[0]["name"] != "peer-1" || peers[0]["peer_asn"] != int64(65002) || peers[0]["interface_name"] != "if-0" {
		t.Errorf("unexpected peer summary: %#v", peers[0])
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"169.254.0.1", "169.254.0.2", "169.254.1.1", "169.254.1.2"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked a BGP peer address %q: %s", forbidden, blob)
		}
	}
}

func TestExtractRouterNatSummaryNeverLeaksAddresses(t *testing.T) {
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"nats": [
			{
				"name": "nat-1",
				"natIpAllocateOption": "AUTO_ONLY",
				"sourceSubnetworkIpRangesToNat": "ALL_SUBNETWORKS_ALL_IP_RANGES",
				"natIps": ["https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/addresses/nat-ip-1"]
			}
		]
	}`

	got, err := extractRouter(routerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["nat_count"] != 1 {
		t.Fatalf("nat_count = %v, want 1", got.Attributes["nat_count"])
	}
	nats, ok := got.Attributes["nats"].([]map[string]any)
	if !ok || len(nats) != 1 {
		t.Fatalf("nats = %#v, want 1 bounded nat summary", got.Attributes["nats"])
	}
	want := map[string]any{
		"name":                               "nat-1",
		"nat_ip_allocate_option":             "AUTO_ONLY",
		"source_subnetwork_ip_ranges_to_nat": "ALL_SUBNETWORKS_ALL_IP_RANGES",
	}
	for k, v := range want {
		if nats[0][k] != v {
			t.Errorf("nats[0][%q] = %#v, want %#v", k, nats[0][k], v)
		}
	}
	if _, present := nats[0]["nat_ips"]; present {
		t.Errorf("nat summary must never carry nat_ips (address data): %#v", nats[0])
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "nat-ip-1") {
		t.Fatalf("extraction leaked a NAT IP resource reference: %s", blob)
	}
}

func TestExtractRouterInterfaceEdgesToVpnTunnelAndInterconnectAttachment(t *testing.T) {
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"interfaces": [
			{
				"name": "if-0",
				"linkedVpnTunnel": "projects/demo-project/regions/us-central1/vpnTunnels/tunnel-1",
				"ipRange": "169.254.0.1/30"
			},
			{
				"name": "if-1",
				"linkedInterconnectAttachment": "projects/demo-project/regions/us-central1/interconnectAttachments/attach-1",
				"ipRange": "169.254.1.1/30"
			},
			{
				"name": "if-2",
				"subnetwork": "projects/demo-project/regions/us-central1/subnetworks/sub-1"
			}
		]
	}`

	got, err := extractRouter(routerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const (
		tunnel     = "//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnTunnels/tunnel-1"
		attachment = "//compute.googleapis.com/projects/demo-project/regions/us-central1/interconnectAttachments/attach-1"
		subnet     = "//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub-1"
	)
	assertRelationship(t, got.Relationships, relationshipTypeRouterInterfaceLinkedVpnTunnel, tunnel, assetTypeComputeVpnTunnel)
	assertRelationship(t, got.Relationships, relationshipTypeRouterInterfaceLinkedInterconnectAttachment, attachment, assetTypeComputeInterconnectAttachment)
	assertRelationship(t, got.Relationships, relationshipTypeRouterInterfaceSubnetwork, subnet, assetTypeComputeSubnetwork)

	if got.Attributes["interface_count"] != 3 {
		t.Fatalf("interface_count = %v, want 3", got.Attributes["interface_count"])
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"169.254.0.1", "169.254.1.1"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked an interface IP range %q: %s", forbidden, blob)
		}
	}
}

func TestExtractRouterEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractRouter(routerContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected nothing for empty data, got %#v", got)
	}
}

func TestExtractRouterMalformedDataErrors(t *testing.T) {
	if _, err := extractRouter(routerContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractRouterEncryptedInterconnectRouterOmittedWhenAbsent(t *testing.T) {
	// encryptedInterconnectRouter must never be fabricated as false when the
	// field is absent from a partial CAI page.
	got, err := extractRouter(routerContext(`{"network":"projects/p/global/networks/n"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := got.Attributes["encrypted_interconnect_router"]; present {
		t.Errorf("encrypted_interconnect_router must be omitted when absent, got %#v", got.Attributes["encrypted_interconnect_router"])
	}
}
