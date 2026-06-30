// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const subnetworkFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/private-subnet"

func subnetworkContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: subnetworkFullName,
		AssetType:        assetTypeComputeSubnetwork,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSubnetworkExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeSubnetwork); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeSubnetwork)
	}
}

func TestExtractSubnetworkFullResource(t *testing.T) {
	const data = `{
		"name": "private-subnet",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/main-vpc",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"ipCidrRange": "10.0.0.0/24",
		"gatewayAddress": "10.0.0.1",
		"privateIpGoogleAccess": true,
		"purpose": "PRIVATE",
		"role": "ACTIVE",
		"stackType": "IPV4_ONLY",
		"enableFlowLogs": true,
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"secondaryIpRanges": [
			{"rangeName": "pods", "ipCidrRange": "10.1.0.0/16"},
			{"rangeName": "services", "ipCidrRange": "10.2.0.0/20"}
		]
	}`

	got, err := extractSubnetwork(subnetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":                   "us-central1",
		"purpose":                  "PRIVATE",
		"role":                     "ACTIVE",
		"private_ip_google_access": true,
		"stack_type":               "IPV4_ONLY",
		"enable_flow_logs":         true,
		"creation_time":            "2024-06-01T07:00:00Z",
		"ip_cidr_prefix_length":    int64(24),
		"secondary_range_count":    2,
		"secondary_range_names":    []string{"pods", "services"},
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{"//compute.googleapis.com/projects/demo-project/global/networks/main-vpc"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the parent-network edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSubnetworkInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/main-vpc", assetTypeComputeNetwork)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != subnetworkFullName {
		t.Errorf("relationship source = %q, want subnet full name", rel.SourceFullResourceName)
	}
	if rel.SourceAssetType != assetTypeComputeSubnetwork {
		t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComputeSubnetwork)
	}
}

func TestExtractSubnetworkNeverPersistsIPAddresses(t *testing.T) {
	const data = `{
		"name": "private-subnet",
		"network": "projects/demo-project/global/networks/main-vpc",
		"ipCidrRange": "172.16.5.0/24",
		"gatewayAddress": "172.16.5.1",
		"ipv6CidrRange": "fd20::/64",
		"internalIpv6Prefix": "fd20:abcd::/64",
		"secondaryIpRanges": [{"rangeName": "pods", "ipCidrRange": "192.168.0.0/16"}]
	}`
	got, err := extractSubnetwork(subnetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	banned := []string{
		"172.16.5.0", "172.16.5.0/24", "172.16.5.1", // primary CIDR + gateway host IP
		"192.168.0.0", "192.168.0.0/16", // secondary CIDR
		"fd20::", "fd20:abcd::", // IPv6 ranges
	}
	for _, token := range banned {
		if containsString(string(blob), token) {
			t.Fatalf("subnetwork extraction leaked IP token %q: %s", token, blob)
		}
	}
	// Prefix length (size, not address) is the redaction-safe derivation we keep.
	if got.Attributes["ip_cidr_prefix_length"] != int64(24) {
		t.Errorf("ip_cidr_prefix_length = %v, want 24", got.Attributes["ip_cidr_prefix_length"])
	}
	// gatewayAddress must never produce an attribute.
	if _, ok := got.Attributes["gateway_address"]; ok {
		t.Errorf("gateway_address must never be persisted: %#v", got.Attributes)
	}
}

func TestExtractSubnetworkPartialData(t *testing.T) {
	// Only network + cidr present: edge resolves, posture fields are omitted
	// rather than written as zero values.
	const data = `{
		"network": "projects/demo-project/global/networks/main-vpc",
		"ipCidrRange": "10.8.0.0/22"
	}`
	got, err := extractSubnetwork(subnetworkContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"ip_cidr_prefix_length": int64(22)}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected the parent-network edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSubnetworkInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/main-vpc", assetTypeComputeNetwork)
}

func TestExtractSubnetworkEmptyDataYieldsNothing(t *testing.T) {
	// A subnet cannot derive its parent network from its own name, so empty
	// resource data yields no attributes and no edges.
	got, err := extractSubnetwork(subnetworkContext(`{}`))
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

func TestExtractSubnetworkMalformedDataErrors(t *testing.T) {
	if _, err := extractSubnetwork(subnetworkContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestSubnetworkNetworkFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"full self link", "https://www.googleapis.com/compute/v1/projects/p/global/networks/n", "//compute.googleapis.com/projects/p/global/networks/n"},
		{"partial path", "projects/p/global/networks/n", "//compute.googleapis.com/projects/p/global/networks/n"},
		{"leading slash partial", "/projects/p/global/networks/n", "//compute.googleapis.com/projects/p/global/networks/n"},
		{"blank", "", ""},
		{"no networks segment", "projects/p/global/firewalls/f", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeNetworkFullName(tc.in); got != tc.want {
				t.Errorf("computeNetworkFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSubnetworkCIDRPrefixLength(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		wantOK bool
	}{
		{"10.0.0.0/24", 24, true},
		{"172.16.0.0/12", 12, true},
		{"fd20::/64", 64, true},
		{"10.0.0.0", 0, false},
		{"", 0, false},
		{"10.0.0.0/notanumber", 0, false},
	}
	for _, tc := range cases {
		got, ok := cidrPrefixLength(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("cidrPrefixLength(%q) = (%d,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestSubnetworkRegionName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://www.googleapis.com/compute/v1/projects/p/regions/us-central1", "us-central1"},
		{"projects/p/regions/europe-west1", "europe-west1"},
		{"us-east1", "us-east1"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := computeRegionName(tc.in); got != tc.want {
			t.Errorf("computeRegionName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
