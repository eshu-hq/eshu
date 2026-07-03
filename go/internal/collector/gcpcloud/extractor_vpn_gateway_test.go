// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const vpnGatewayFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/vpnGateways/ha-vpn-gw"

func vpnGatewayContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: vpnGatewayFullName,
		AssetType:        assetTypeComputeVPNGateway,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestVPNGatewayExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeVPNGateway); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeVPNGateway)
	}
}

func TestExtractVPNGatewayFullData(t *testing.T) {
	const data = `{
		"name": "ha-vpn-gw",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/main-vpc",
		"stackType": "IPV4_ONLY",
		"gatewayIpVersion": "IPV4",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"vpnInterfaces": [
			{"id": 0, "ipAddress": "203.0.113.10"},
			{"id": 1, "ipAddress": "203.0.113.11"}
		]
	}`

	got, err := extractVPNGateway(vpnGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":              "us-central1",
		"stack_type":          "IPV4_ONLY",
		"gateway_ip_version":  "IPV4",
		"creation_time":       "2024-06-01T07:00:00Z",
		"vpn_interface_count": 2,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantNetwork := "//compute.googleapis.com/projects/demo-project/global/networks/main-vpc"
	wantAnchors := []string{wantNetwork}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 network edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeVPNGatewayInNetwork, wantNetwork, assetTypeComputeNetwork)
}

func TestExtractVPNGatewayPartialProjectLessNetwork(t *testing.T) {
	// A bare/project-less network partial must resolve against the source
	// resource's own project rather than being dropped.
	const data = `{
		"name": "ha-vpn-gw",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"network": "global/networks/main-vpc"
	}`

	got, err := extractVPNGateway(vpnGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantNetwork := "//compute.googleapis.com/projects/demo-project/global/networks/main-vpc"
	assertRelationship(t, got.Relationships, relationshipTypeVPNGatewayInNetwork, wantNetwork, assetTypeComputeNetwork)
}

func TestExtractVPNGatewayNoInterfaces(t *testing.T) {
	// vpn_interface_count must be omitted, not fabricated as 0, when the CAI
	// page carries no vpnInterfaces entries.
	const data = `{
		"name": "ha-vpn-gw",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1"
	}`

	got, err := extractVPNGateway(vpnGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["vpn_interface_count"]; ok {
		t.Errorf("vpn_interface_count should be omitted when no interfaces present, got %#v", got.Attributes["vpn_interface_count"])
	}
}

func TestExtractVPNGatewayNeverPersistsInterfaceIPAddress(t *testing.T) {
	const data = `{
		"name": "ha-vpn-gw",
		"vpnInterfaces": [
			{"id": 0, "ipAddress": "203.0.113.10", "ipv6Address": "2001:db8::1"}
		]
	}`
	got, err := extractVPNGateway(vpnGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"203.0.113.10", "2001:db8::1"} {
		if containsString(string(blob), banned) {
			t.Fatalf("vpn gateway extraction leaked interface address %q: %s", banned, blob)
		}
	}
	if _, ok := got.Attributes["ip_address"]; ok {
		t.Errorf("ip_address must never be persisted: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["ipv6_address"]; ok {
		t.Errorf("ipv6_address must never be persisted: %#v", got.Attributes)
	}
}

func TestExtractVPNGatewayEmptyData(t *testing.T) {
	got, err := extractVPNGateway(vpnGatewayContext(`{}`))
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

func TestExtractVPNGatewayMalformedDataErrors(t *testing.T) {
	if _, err := extractVPNGateway(vpnGatewayContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractVPNGatewayNeverPersistsRawResourceDataBlob(t *testing.T) {
	// Adversarial redaction test: marshal the entire AttributeExtraction to
	// JSON and grep for banned tokens (IP addresses, label values would go
	// here too if labels were decoded).
	const data = `{
		"name": "ha-vpn-gw",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/main-vpc",
		"stackType": "IPV4_ONLY",
		"gatewayIpVersion": "IPV4",
		"labelFingerprint": "abc123==",
		"vpnInterfaces": [
			{"id": 0, "ipAddress": "198.51.100.5", "interconnectAttachment": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/interconnectAttachments/attach-1"}
		]
	}`
	got, err := extractVPNGateway(vpnGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"198.51.100.5", "abc123==", "interconnectAttachments/attach-1"} {
		if containsString(string(blob), banned) {
			t.Fatalf("vpn gateway extraction leaked banned token %q: %s", banned, blob)
		}
	}
}
