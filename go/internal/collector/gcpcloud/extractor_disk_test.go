// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const diskFullName = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/disks/data-disk"

func diskContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: diskFullName,
		AssetType:        assetTypeComputeDisk,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDiskExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeDisk); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeDisk)
	}
}

func TestExtractDiskFullResource(t *testing.T) {
	const data = `{
		"name": "data-disk",
		"zone": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a",
		"sizeGb": "500",
		"type": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/diskTypes/pd-ssd",
		"status": "READY",
		"users": [
			"https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instances/web-1",
			"https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a/instances/web-2"
		],
		"sourceImage": "https://www.googleapis.com/compute/v1/projects/debian-cloud/global/images/debian-12",
		"sourceSnapshot": "https://www.googleapis.com/compute/v1/projects/demo-project/global/snapshots/nightly",
		"diskEncryptionKey": {"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key/cryptoKeyVersions/3"},
		"physicalBlockSizeBytes": "4096",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"labels": {"team": "platform"}
	}`

	got, err := extractDisk(diskContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"zone":                      "us-central1-a",
		"size_gb":                   int64(500),
		"disk_type":                 "pd-ssd",
		"status":                    "READY",
		"attached_instance_count":   2,
		"physical_block_size_bytes": int64(4096),
		"creation_time":             "2024-06-01T07:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const (
		instance1 = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/web-1"
		instance2 = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/instances/web-2"
		image     = "//compute.googleapis.com/projects/debian-cloud/global/images/debian-12"
		snapshot  = "//compute.googleapis.com/projects/demo-project/global/snapshots/nightly"
		cryptoKey = "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/ring/cryptoKeys/key"
	)

	wantAnchors := []string{instance1, instance2, image, snapshot, cryptoKey}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 5 {
		t.Fatalf("expected 5 edges (2 instances, image, snapshot, kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDiskAttachedToInstance, instance1, assetTypeComputeInstance)
	assertRelationship(t, got.Relationships, relationshipTypeDiskAttachedToInstance, instance2, assetTypeComputeInstance)
	assertRelationship(t, got.Relationships, relationshipTypeDiskCreatedFromImage, image, assetTypeComputeImage)
	assertRelationship(t, got.Relationships, relationshipTypeDiskCreatedFromSnapshot, snapshot, assetTypeComputeSnapshot)
	assertRelationship(t, got.Relationships, relationshipTypeDiskEncryptedByKey, cryptoKey, assetTypeKMSCryptoKey)

	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != diskFullName {
			t.Errorf("relationship source = %q, want disk full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeComputeDisk {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComputeDisk)
		}
	}
}

func TestExtractDiskRegionalDiskUsesRegion(t *testing.T) {
	const data = `{
		"name": "regional-disk",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"sizeGb": "200",
		"status": "READY"
	}`
	got, err := extractDisk(diskContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["region"] != "us-central1" {
		t.Errorf("region = %v, want us-central1", got.Attributes["region"])
	}
	if _, ok := got.Attributes["zone"]; ok {
		t.Errorf("regional disk must not carry a zone attribute: %#v", got.Attributes)
	}
}

func TestExtractDiskSizeAsNumber(t *testing.T) {
	// CAI may render integer control-plane fields either as JSON strings (the
	// compute API convention) or as bare JSON numbers; both must parse.
	const data = `{"zone": "projects/p/zones/z", "sizeGb": 10, "physicalBlockSizeBytes": 512}`
	got, err := extractDisk(diskContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["size_gb"] != int64(10) {
		t.Errorf("size_gb = %v, want 10", got.Attributes["size_gb"])
	}
	if got.Attributes["physical_block_size_bytes"] != int64(512) {
		t.Errorf("physical_block_size_bytes = %v, want 512", got.Attributes["physical_block_size_bytes"])
	}
}

func TestExtractDiskPartialData(t *testing.T) {
	// Only a source image present: the image edge resolves and posture fields
	// are omitted rather than fabricated as zero values.
	const data = `{"sourceImage": "projects/debian-cloud/global/images/debian-12"}`
	got, err := extractDisk(diskContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Fatalf("expected no attributes for image-only data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected the source-image edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDiskCreatedFromImage,
		"//compute.googleapis.com/projects/debian-cloud/global/images/debian-12", assetTypeComputeImage)
}

func TestExtractDiskEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDisk(diskContext(`{}`))
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

func TestExtractDiskMalformedDataErrors(t *testing.T) {
	if _, err := extractDisk(diskContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestDiskComputeResourceFullName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		segment string
		want    string
	}{
		{"instance self link", "https://www.googleapis.com/compute/v1/projects/p/zones/z/instances/i", "instances", "//compute.googleapis.com/projects/p/zones/z/instances/i"},
		{"image partial", "projects/p/global/images/img", "images", "//compute.googleapis.com/projects/p/global/images/img"},
		{"leading slash", "/projects/p/global/snapshots/s", "snapshots", "//compute.googleapis.com/projects/p/global/snapshots/s"},
		{"blank", "", "instances", ""},
		{"wrong segment", "projects/p/zones/z/instances/i", "images", ""},
		{"no projects", "zones/z/instances/i", "instances", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeResourceFullName(tc.in, tc.segment); got != tc.want {
				t.Errorf("computeResourceFullName(%q, %q) = %q, want %q", tc.in, tc.segment, got, tc.want)
			}
		})
	}
}

func TestDiskKMSCryptoKeyFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"with version stripped", "projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/5", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"without version", "projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"leading slash", "/projects/p/locations/l/keyRings/r/cryptoKeys/k", "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"},
		{"blank", "", ""},
		{"no cryptoKeys segment", "projects/p/locations/l/keyRings/r", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kmsCryptoKeyFullName(tc.in); got != tc.want {
				t.Errorf("kmsCryptoKeyFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDiskParseFlexibleInt64(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   int64
		wantOK bool
	}{
		{"json string", `"500"`, 500, true},
		{"json number", `4096`, 4096, true},
		{"empty string", `""`, 0, false},
		{"absent", ``, 0, false},
		{"non numeric", `"abc"`, 0, false},
		{"null", `null`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlexibleInt64(json.RawMessage(tc.in))
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("parseFlexibleInt64(%q) = (%d,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDiskZoneName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-a", "us-central1-a"},
		{"projects/p/zones/europe-west1-b", "europe-west1-b"},
		{"us-east1-c", "us-east1-c"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := computeZoneName(tc.in); got != tc.want {
			t.Errorf("computeZoneName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
