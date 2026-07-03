// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const dnsManagedZoneFullName = "//dns.googleapis.com/projects/demo-project/managedZones/prod-zone"

func dnsManagedZoneContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dnsManagedZoneFullName,
		AssetType:        dnsManagedZoneAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDNSManagedZoneExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(dnsManagedZoneAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", dnsManagedZoneAssetType)
	}
}

func TestExtractDNSManagedZonePublicZone(t *testing.T) {
	const data = `{
		"name": "prod-zone",
		"dnsName": "example.com.",
		"visibility": "public",
		"creationTime": "2024-06-01T00:00:00.000Z",
		"dnssecConfig": {"state": "on"},
		"labels": {"team": "platform"}
	}`

	got, err := extractDNSManagedZone(dnsManagedZoneContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"visibility":    "public",
		"dnssec_state":  "on",
		"creation_time": "2024-06-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if _, ok := got.Attributes["dns_name"]; ok {
		t.Fatalf("dns_name must never be persisted raw: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges for a public zone with no forwarding/peering, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for a public zone with no forwarding/peering, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractDNSManagedZonePrivateZoneWithNetworks(t *testing.T) {
	const data = `{
		"name": "internal-zone",
		"dnsName": "internal.corp.",
		"visibility": "private",
		"privateVisibilityConfig": {
			"networks": [
				{"networkUrl": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/vpc-a"},
				{"networkUrl": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/vpc-b"}
			]
		}
	}`

	got, err := extractDNSManagedZone(dnsManagedZoneContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["visibility"] != "private" {
		t.Errorf("visibility = %v, want private", got.Attributes["visibility"])
	}
	if got.Attributes["private_network_count"] != 2 {
		t.Errorf("private_network_count = %v, want 2", got.Attributes["private_network_count"])
	}

	const (
		vpcA = "//compute.googleapis.com/projects/demo-project/global/networks/vpc-a"
		vpcB = "//compute.googleapis.com/projects/demo-project/global/networks/vpc-b"
	)
	wantAnchors := []string{vpcA, vpcB}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 network edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeManagedZoneVisibleFromNetwork, vpcA, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeManagedZoneVisibleFromNetwork, vpcB, assetTypeComputeNetwork)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != dnsManagedZoneFullName {
			t.Errorf("relationship source = %q, want zone full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != dnsManagedZoneAssetType {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, dnsManagedZoneAssetType)
		}
	}
}

func TestExtractDNSManagedZonePeeringConfig(t *testing.T) {
	const data = `{
		"name": "peer-zone",
		"visibility": "private",
		"peeringConfig": {
			"targetNetwork": {
				"networkUrl": "https://www.googleapis.com/compute/v1/projects/other-project/global/networks/peer-vpc"
			}
		}
	}`

	got, err := extractDNSManagedZone(dnsManagedZoneContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v, _ := got.Attributes["is_peering_zone"].(bool); !v {
		t.Errorf("expected is_peering_zone=true, got %#v", got.Attributes)
	}
	const peerVPC = "//compute.googleapis.com/projects/other-project/global/networks/peer-vpc"
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 peering edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeManagedZonePeersWithNetwork, peerVPC, assetTypeComputeNetwork)
	wantAnchors := []string{peerVPC}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractDNSManagedZoneForwardingConfig(t *testing.T) {
	const data = `{
		"name": "forward-zone",
		"visibility": "private",
		"forwardingConfig": {
			"targetNameServers": [
				{"ipv4Address": "10.0.0.1", "forwardingPath": "private"},
				{"ipv4Address": "10.0.0.2", "forwardingPath": "default"}
			]
		}
	}`

	got, err := extractDNSManagedZone(dnsManagedZoneContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["forwarding_target_count"] != 2 {
		t.Errorf("forwarding_target_count = %v, want 2", got.Attributes["forwarding_target_count"])
	}
	if v, _ := got.Attributes["forwarding_enabled"].(bool); !v {
		t.Errorf("expected forwarding_enabled=true, got %#v", got.Attributes)
	}
	// Forwarding target IPs must never leak into any attribute, anchor, or
	// relationship, regardless of the Go type they might arrive in. Marshal
	// the entire extraction struct and scan the resulting JSON so a future
	// regression that stores the IP as json.RawMessage, []byte, or any other
	// non-string type is still caught (matches the full-struct-marshal leak
	// pattern used by sibling extractors in this package).
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "10.0.0.1") || containsString(string(blob), "10.0.0.2") {
		t.Fatalf("extraction leaked a forwarding target IP: %s", blob)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("forwarding target IPs are not resolvable CAI endpoints, expected no edges, got %#v", got.Relationships)
	}
}

func TestExtractDNSManagedZoneEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDNSManagedZone(dnsManagedZoneContext(`{}`))
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

func TestExtractDNSManagedZoneMalformedDataErrors(t *testing.T) {
	if _, err := extractDNSManagedZone(dnsManagedZoneContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractDNSManagedZoneDNSSECOff(t *testing.T) {
	const data = `{"visibility": "public", "dnssecConfig": {"state": "off"}}`
	got, err := extractDNSManagedZone(dnsManagedZoneContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["dnssec_state"] != "off" {
		t.Errorf("dnssec_state = %v, want off", got.Attributes["dnssec_state"])
	}
}

func TestExtractDNSManagedZoneNetworkURLResolutionSkipsUnresolvable(t *testing.T) {
	const data = `{
		"visibility": "private",
		"privateVisibilityConfig": {
			"networks": [
				{"networkUrl": ""},
				{"networkUrl": "not-a-valid-network-reference"}
			]
		}
	}`
	got, err := extractDNSManagedZone(dnsManagedZoneContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges for unresolvable network references, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for unresolvable network references, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["private_network_count"]; ok {
		t.Errorf("expected no private_network_count when no network resolved a URL, got %#v", got.Attributes)
	}
}
