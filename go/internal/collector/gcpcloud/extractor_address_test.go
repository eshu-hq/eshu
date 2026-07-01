// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const addressFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/addresses/web-ip"

func addressContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: addressFullName,
		AssetType:        assetTypeComputeAddress,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestAddressExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeAddress); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeAddress)
	}
}

func TestExtractAddressFullResource(t *testing.T) {
	const data = `{
		"name": "web-ip",
		"address": "203.0.113.50",
		"addressType": "EXTERNAL",
		"purpose": "GCE_ENDPOINT",
		"status": "IN_USE",
		"ipVersion": "IPV4",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc",
		"subnetwork": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/prod-subnet",
		"users": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/forwardingRules/web-fr",
			"https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instances/web-1"
		],
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractAddress(addressContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":        "us-central1",
		"address_type":  "EXTERNAL",
		"is_external":   true,
		"purpose":       "GCE_ENDPOINT",
		"status":        "IN_USE",
		"ip_version":    "IPV4",
		"creation_time": "2024-06-01T07:00:00Z",
		"user_count":    2,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const (
		network        = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"
		subnetwork     = "//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/prod-subnet"
		forwardingRule = "//compute.googleapis.com/projects/demo-project/regions/us-central1/forwardingRules/web-fr"
		instance       = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/web-1"
	)
	wantAnchors := []string{network, subnetwork, forwardingRule, instance}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 4 {
		t.Fatalf("expected 4 edges (network, subnetwork, fr, instance), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAddressInNetwork, network, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeAddressInSubnetwork, subnetwork, assetTypeComputeSubnetwork)
	assertRelationship(t, got.Relationships, relationshipTypeAddressUsedByForwardingRule, forwardingRule, assetTypeComputeForwardingRule)
	assertRelationship(t, got.Relationships, relationshipTypeAddressUsedByInstance, instance, assetTypeComputeInstance)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != addressFullName {
			t.Errorf("relationship source = %q, want address full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeComputeAddress {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComputeAddress)
		}
		if rel.SupportState != RelationshipSupportSupported {
			t.Errorf("relationship support state = %q, want %q", rel.SupportState, RelationshipSupportSupported)
		}
	}
}

func TestExtractAddressInternalNoIPLeak(t *testing.T) {
	const data = `{
		"address": "10.0.0.42",
		"addressType": "INTERNAL",
		"purpose": "GCE_ENDPOINT",
		"status": "RESERVED",
		"subnetwork": "projects/demo-project/regions/us-central1/subnetworks/prod-subnet"
	}`
	got, err := extractAddress(addressContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["is_external"] != false {
		t.Errorf("is_external = %v, want false", got.Attributes["is_external"])
	}
	if got.Attributes["address_type"] != "INTERNAL" {
		t.Errorf("address_type = %v, want INTERNAL", got.Attributes["address_type"])
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "10.0.0.42") {
		t.Fatalf("extraction leaked the reserved IP address: %s", blob)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAddressInSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/prod-subnet", assetTypeComputeSubnetwork)
}

func TestGlobalAddressExtractorIsRegistered(t *testing.T) {
	// Global static IPs are the distinct GlobalAddress CAI asset type and must
	// carry the same typed depth as regional addresses.
	if _, ok := lookupAssetExtractor(assetTypeComputeGlobalAddress); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeGlobalAddress)
	}
}

func TestExtractGlobalAddressNoRegion(t *testing.T) {
	// A global address has no region/subnetwork; the extractor still surfaces the
	// external-exposure posture and the network edge without fabricating a region.
	const data = `{
		"address": "203.0.113.99",
		"addressType": "EXTERNAL",
		"purpose": "VPC_PEERING",
		"status": "RESERVED",
		"ipVersion": "IPV4",
		"network": "projects/demo-project/global/networks/prod-vpc"
	}`
	ctx := ExtractContext{
		FullResourceName: "//compute.googleapis.com/projects/demo-project/global/addresses/psc-ip",
		AssetType:        assetTypeComputeGlobalAddress,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
	got, err := extractAddress(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["is_external"] != true {
		t.Errorf("is_external = %v, want true", got.Attributes["is_external"])
	}
	if _, ok := got.Attributes["region"]; ok {
		t.Errorf("global address must not carry a region attribute: %#v", got.Attributes)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "203.0.113.99") {
		t.Fatalf("extraction leaked the global address IP: %s", blob)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAddressInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractAddressDeduplicatesRepeatedUsers(t *testing.T) {
	// A CAI users[] list may repeat the same resource; user_count must match the
	// distinct used-by edges, not the raw list length.
	const data = `{
		"addressType": "EXTERNAL",
		"users": [
			"projects/demo-project/regions/us-central1/forwardingRules/web-fr",
			"projects/demo-project/regions/us-central1/forwardingRules/web-fr",
			"projects/demo-project/zones/us-central1-a/instances/web-1"
		]
	}`
	got, err := extractAddress(addressContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["user_count"] != 2 {
		t.Errorf("user_count = %v, want 2 (deduplicated)", got.Attributes["user_count"])
	}
	fr := 0
	inst := 0
	for _, rel := range got.Relationships {
		switch rel.RelationshipType {
		case relationshipTypeAddressUsedByForwardingRule:
			fr++
		case relationshipTypeAddressUsedByInstance:
			inst++
		}
	}
	if fr != 1 || inst != 1 {
		t.Errorf("edges = fr:%d inst:%d, want fr:1 inst:1 (no duplicate edge)", fr, inst)
	}
}

func TestExtractAddressEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractAddress(addressContext(`{}`))
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

func TestExtractAddressMalformedDataErrors(t *testing.T) {
	if _, err := extractAddress(addressContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
