// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const computeNetworkFullName = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"

func computeNetworkContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: computeNetworkFullName,
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
		if rel.SourceFullResourceName != computeNetworkFullName {
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
		if rel.TargetFullResourceName == computeNetworkFullName {
			t.Fatalf("self-peering produced a self-edge: %#v", rel)
		}
	}
}
