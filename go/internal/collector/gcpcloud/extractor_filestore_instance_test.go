// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const filestoreInstanceFullName = "//file.googleapis.com/projects/demo-project/locations/us-central1-a/instances/nfs-share"

func filestoreInstanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: filestoreInstanceFullName,
		AssetType:        assetTypeFilestoreInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestFilestoreInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeFilestoreInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeFilestoreInstance)
	}
}

func TestExtractFilestoreInstanceFullAttributesWithNetworkAndCMEK(t *testing.T) {
	const data = `{
		"state": "READY",
		"tier": "ENTERPRISE",
		"createTime": "2024-06-01T00:00:00Z",
		"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key",
		"labels": {"env": "prod"},
		"fileShares": [
			{"name": "share1", "capacityGb": 2560},
			{"name": "share2", "capacityGb": 1024}
		],
		"networks": [
			{
				"network": "prod-vpc",
				"modes": ["MODE_IPV4"],
				"connectMode": "DIRECT_PEERING",
				"reservedIpRange": "10.0.0.0/29"
			}
		]
	}`

	got, err := extractFilestoreInstance(filestoreInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"state":            "READY",
		"tier":             "ENTERPRISE",
		"creation_time":    "2024-06-01T00:00:00Z",
		"file_share_count": int(2),
		"connect_mode":     "DIRECT_PEERING",
		"kms_key_name":     "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key",
		"label_count":      int(1),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (network, kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFilestoreInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeFilestoreInstanceEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key", assetTypeKMSCryptoKey)

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractFilestoreInstanceBasicTierNoNetworkNoKMS(t *testing.T) {
	const data = `{
		"state": "READY",
		"tier": "BASIC_HDD",
		"fileShares": [{"name": "share1", "capacityGb": 1024}]
	}`

	got, err := extractFilestoreInstance(filestoreInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["tier"] != "BASIC_HDD" {
		t.Errorf("tier = %v, want BASIC_HDD", got.Attributes["tier"])
	}
	if got.Attributes["file_share_count"] != int(1) {
		t.Errorf("file_share_count = %v, want 1", got.Attributes["file_share_count"])
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["kms_key_name"]; ok {
		t.Errorf("kms_key_name should be absent when unset: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["connect_mode"]; ok {
		t.Errorf("connect_mode should be absent when unset: %#v", got.Attributes)
	}
}

func TestExtractFilestoreInstanceNetworkFullSelfLink(t *testing.T) {
	const data = `{
		"networks": [
			{"network": "projects/demo-project/global/networks/prod-vpc", "connectMode": "PRIVATE_SERVICE_ACCESS"}
		]
	}`

	got, err := extractFilestoreInstance(filestoreInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFilestoreInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractFilestoreInstanceKMSKeyAlreadyCAIPrefixedNotDoublePrefixed(t *testing.T) {
	const data = `{
		"kmsKeyName": "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key"
	}`

	got, err := extractFilestoreInstance(filestoreInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeFilestoreInstanceEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key", assetTypeKMSCryptoKey)
	if got.Attributes["kms_key_name"] != "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key" {
		t.Errorf("kms_key_name = %v, want bare relative form matching the anchor/edge normalization", got.Attributes["kms_key_name"])
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeFilestoreInstanceEncryptedByKMSKey {
			if got := rel.TargetFullResourceName; got != "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/filestore-key" {
				t.Fatalf("kms target double-prefixed or malformed: %q", got)
			}
		}
	}
}

func TestExtractFilestoreInstanceMultipleNetworksEachEmitAnEdge(t *testing.T) {
	const data = `{
		"networks": [
			{"network": "prod-vpc", "connectMode": "DIRECT_PEERING"},
			{"network": "mgmt-vpc", "connectMode": "DIRECT_PEERING"}
		]
	}`

	got, err := extractFilestoreInstance(filestoreInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 network edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFilestoreInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeFilestoreInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/mgmt-vpc", assetTypeComputeNetwork)
}

func TestExtractFilestoreInstanceNeverPersistsReservedIPRange(t *testing.T) {
	const data = `{
		"networks": [
			{"network": "prod-vpc", "reservedIpRange": "10.0.0.0/29", "connectMode": "DIRECT_PEERING"}
		]
	}`
	got, err := extractFilestoreInstance(filestoreInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	if containsString(s, "10.0.0.0/29") {
		t.Fatalf("filestore instance extraction leaked reservedIpRange: %s", blob)
	}
}

func TestExtractFilestoreInstanceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractFilestoreInstance(filestoreInstanceContext(`{}`))
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

func TestExtractFilestoreInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractFilestoreInstance(filestoreInstanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
