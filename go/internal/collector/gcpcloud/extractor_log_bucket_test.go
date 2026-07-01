// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const logBucketFullName = "//logging.googleapis.com/projects/demo-project/locations/global/buckets/_Default"

func logBucketContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: logBucketFullName,
		AssetType:        logBucketAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestLogBucketExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(logBucketAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", logBucketAssetType)
	}
}

func TestExtractLogBucketFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/global/buckets/_Default",
		"retentionDays": 30,
		"locked": true,
		"analyticsEnabled": true,
		"createTime": "2024-06-01T00:00:00Z",
		"cmekSettings": {"kmsKeyName": "projects/demo-project/locations/global/keyRings/logs/cryptoKeys/primary"}
	}`
	got, err := extractLogBucket(logBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"retention_days":              30,
		"locked":                      true,
		"analytics_enabled":           true,
		"creation_time":               "2024-06-01T00:00:00Z",
		"customer_managed_encryption": true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 CMEK edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeLogBucketEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/global/keyRings/logs/cryptoKeys/primary", assetTypeKMSCryptoKey)
	wantAnchors := []string{"//cloudkms.googleapis.com/projects/demo-project/locations/global/keyRings/logs/cryptoKeys/primary"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractLogBucketNoCMEK(t *testing.T) {
	const data = `{"retentionDays": 400, "locked": false, "analyticsEnabled": false}`
	got, err := extractLogBucket(logBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["retention_days"] != 400 {
		t.Errorf("retention_days = %v, want 400", got.Attributes["retention_days"])
	}
	if got.Attributes["locked"] != false {
		t.Errorf("locked = %v, want false", got.Attributes["locked"])
	}
	if _, ok := got.Attributes["customer_managed_encryption"]; ok {
		t.Errorf("no CMEK present; flag must be omitted: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("no CMEK; expected no edges, got %#v", got.Relationships)
	}
}

func TestExtractLogBucketAbsentPointersOmitted(t *testing.T) {
	// locked and analyticsEnabled are pointers: absent fields are omitted, distinct
	// from a present false.
	const data = `{"retentionDays": 30}`
	got, err := extractLogBucket(logBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["locked"]; ok {
		t.Errorf("absent locked must be omitted: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["analytics_enabled"]; ok {
		t.Errorf("absent analyticsEnabled must be omitted: %#v", got.Attributes)
	}
}

func TestExtractLogBucketEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractLogBucket(logBucketContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
}

func TestExtractLogBucketMalformedDataErrors(t *testing.T) {
	if _, err := extractLogBucket(logBucketContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
