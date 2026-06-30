// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const computeNetworkTestFullName = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"

func computeNetworkContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: computeNetworkTestFullName,
		AssetType:        assetTypeComputeNetwork,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestComputeNetworkExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeNetwork); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeNetwork)
	}
}

func TestExtractComputeNetworkCustomMode(t *testing.T) {
	const data = `{
		"name": "prod-vpc",
		"creationTimestamp": "2026-06-20T10:00:00.000-07:00",
		"selfLink": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc",
		"autoCreateSubnetworks": false,
		"mtu": 1460,
		"routingConfig": {"routingMode": "GLOBAL"},
		"subnetworks": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/prod-us-central1",
			"https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-east1/subnetworks/prod-us-east1"
		],
		"peerings": [
			{"name": "peer-to-shared", "network": "https://www.googleapis.com/compute/v1/projects/shared-project/global/networks/shared-vpc", "state": "ACTIVE"}
		]
	}`

	got, err := extractComputeNetwork(computeNetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"auto_create_subnetworks": false,
		"routing_mode":            "GLOBAL",
		"mtu":                     int64(1460),
		"creation_timestamp":      "2026-06-20T10:00:00.000-07:00",
		"subnetwork_count":        2,
		"peering_count":           1,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/prod-us-central1",
		"//compute.googleapis.com/projects/demo-project/regions/us-east1/subnetworks/prod-us-east1",
		"//compute.googleapis.com/projects/shared-project/global/networks/shared-vpc",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	assertRelationship(t, got.Relationships, relationshipTypeNetworkContainsSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/prod-us-central1", assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeNetworkContainsSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-east1/subnetworks/prod-us-east1", assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeNetworkPeersWithNetwork,
		"//compute.googleapis.com/projects/shared-project/global/networks/shared-vpc", assetTypeComputeNetwork)
	if len(got.Relationships) != 3 {
		t.Fatalf("expected 3 relationships (2 subnets, 1 peering), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != computeNetworkTestFullName {
			t.Errorf("relationship source = %q, want network full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeComputeNetwork {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComputeNetwork)
		}
		if rel.SupportState != RelationshipSupportSupported {
			t.Errorf("relationship support state = %q, want %q", rel.SupportState, RelationshipSupportSupported)
		}
	}
}

func TestExtractComputeNetworkAutoMode(t *testing.T) {
	// Auto-mode network with no explicit subnetwork list and no peerings: bounded
	// attributes are present, but no typed edges are emitted.
	const data = `{
		"name": "default",
		"autoCreateSubnetworks": true,
		"mtu": 1460,
		"routingConfig": {"routingMode": "REGIONAL"}
	}`

	got, err := extractComputeNetwork(computeNetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["auto_create_subnetworks"] != true {
		t.Errorf("auto_create_subnetworks = %v, want true", got.Attributes["auto_create_subnetworks"])
	}
	if got.Attributes["routing_mode"] != "REGIONAL" {
		t.Errorf("routing_mode = %v, want REGIONAL", got.Attributes["routing_mode"])
	}
	if _, ok := got.Attributes["subnetwork_count"]; ok {
		t.Errorf("subnetwork_count should be omitted when no subnetworks present, got %#v", got.Attributes["subnetwork_count"])
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for auto-mode network, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for auto-mode network, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractComputeNetworkNoLeakageOfDataPlane(t *testing.T) {
	// Legacy/data-plane fields (IP ranges, gateway IPs, peering export flags) must
	// never reach the extraction output.
	const data = `{
		"name": "legacy-vpc",
		"IPv4Range": "10.240.0.0/16",
		"gatewayIPv4": "10.240.0.1",
		"autoCreateSubnetworks": false,
		"subnetworks": ["https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/s1"],
		"peerings": [
			{"name": "p1", "network": "https://www.googleapis.com/compute/v1/projects/other/global/networks/n1", "state": "ACTIVE", "exportSubnetRoutesWithPublicIp": true}
		]
	}`
	got, err := extractComputeNetwork(computeNetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"10.240.0.0", "10.240.0.1", "exportSubnetRoutesWithPublicIp"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked data-plane token %q: %s", banned, blob)
		}
	}
}

func TestExtractComputeNetworkEmptyData(t *testing.T) {
	got, err := extractComputeNetwork(computeNetworkContext(`{}`))
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

func TestExtractComputeNetworkMalformedDataErrors(t *testing.T) {
	_, err := extractComputeNetwork(computeNetworkContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractComputeNetworkSkipsInactivePeering(t *testing.T) {
	// Only ACTIVE peerings exchange routes/connectivity, so only an ACTIVE peering
	// may emit a materializing edge and correlation anchor. An INACTIVE peering is
	// still counted in peering_count (it is a configured peering) but must not
	// produce a graph edge.
	const data = `{
		"name": "prod-vpc",
		"peerings": [
			{"name": "active-peer", "network": "https://www.googleapis.com/compute/v1/projects/p-active/global/networks/n-active", "state": "ACTIVE"},
			{"name": "inactive-peer", "network": "https://www.googleapis.com/compute/v1/projects/p-inactive/global/networks/n-inactive", "state": "INACTIVE"}
		]
	}`
	got, err := extractComputeNetwork(computeNetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["peering_count"] != 2 {
		t.Errorf("peering_count = %v, want 2 (total configured peerings)", got.Attributes["peering_count"])
	}
	peeringEdges := 0
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeNetworkPeersWithNetwork {
			peeringEdges++
		}
		if rel.TargetFullResourceName == "//compute.googleapis.com/projects/p-inactive/global/networks/n-inactive" {
			t.Errorf("INACTIVE peering emitted a materializing edge: %#v", rel)
		}
	}
	if peeringEdges != 1 {
		t.Fatalf("expected 1 peering edge (the ACTIVE one), got %d: %#v", peeringEdges, got.Relationships)
	}
	for _, anchor := range got.CorrelationAnchors {
		if anchor == "//compute.googleapis.com/projects/p-inactive/global/networks/n-inactive" {
			t.Errorf("INACTIVE peer network surfaced as a correlation anchor: %q", anchor)
		}
	}
}

func TestExtractComputeNetworkNormalizesPartialURLs(t *testing.T) {
	// CAI may carry partial peering/subnetwork URLs: a project-qualified partial
	// (projects/p/...) and a project-less partial (regions/.../subnetworks/...)
	// that resolves against the source network's project. Both must resolve to CAI
	// full resource names, not be silently dropped.
	const data = `{
		"name": "prod-vpc",
		"subnetworks": ["regions/us-central1/subnetworks/local-subnet"],
		"peerings": [
			{"name": "partial-peer", "network": "projects/other-project/global/networks/other-net", "state": "ACTIVE"}
		]
	}`
	got, err := extractComputeNetwork(computeNetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeNetworkContainsSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/local-subnet", assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeNetworkPeersWithNetwork,
		"//compute.googleapis.com/projects/other-project/global/networks/other-net", assetTypeComputeNetwork)
}

func TestComputeFullResourceNameFromSelfLink(t *testing.T) {
	cases := []struct {
		name      string
		link      string
		projectID string
		want      string
	}{
		{"empty", "", "demo-project", ""},
		{"already cai", "//compute.googleapis.com/projects/p/global/networks/n", "demo-project", "//compute.googleapis.com/projects/p/global/networks/n"},
		{"full selflink v1", "https://www.googleapis.com/compute/v1/projects/p/regions/r/subnetworks/s", "demo-project", "//compute.googleapis.com/projects/p/regions/r/subnetworks/s"},
		{"full selflink beta", "https://compute.googleapis.com/compute/beta/projects/p/global/networks/n", "demo-project", "//compute.googleapis.com/projects/p/global/networks/n"},
		{"partial with project", "projects/p/global/networks/n", "demo-project", "//compute.googleapis.com/projects/p/global/networks/n"},
		{"partial project-less global", "global/networks/n", "demo-project", "//compute.googleapis.com/projects/demo-project/global/networks/n"},
		{"partial project-less region", "regions/r/subnetworks/s", "demo-project", "//compute.googleapis.com/projects/demo-project/regions/r/subnetworks/s"},
		{"project-less without source project", "global/networks/n", "", ""},
		{"unrecognized", "garbage", "demo-project", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeFullResourceNameFromSelfLink(tc.link, tc.projectID); got != tc.want {
				t.Fatalf("computeFullResourceNameFromSelfLink(%q, %q) = %q, want %q", tc.link, tc.projectID, got, tc.want)
			}
		})
	}
}

func TestExtractComputeNetworkSkipsSelfPeering(t *testing.T) {
	// A peering whose target network resolves to the source network itself must
	// not produce a self-edge.
	const data = `{
		"name": "prod-vpc",
		"peerings": [
			{"name": "self", "network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc", "state": "ACTIVE"}
		]
	}`
	got, err := extractComputeNetwork(computeNetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, rel := range got.Relationships {
		if rel.TargetFullResourceName == computeNetworkTestFullName {
			t.Fatalf("self-peering produced a self-edge: %#v", rel)
		}
	}
}
