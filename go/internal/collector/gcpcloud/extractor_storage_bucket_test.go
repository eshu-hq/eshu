// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const storageBucketFullResourceName = "//storage.googleapis.com/projects/_/buckets/demo-bucket"

func storageBucketContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: storageBucketFullResourceName,
		AssetType:        assetTypeStorageBucket,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestStorageBucketExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeStorageBucket); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeStorageBucket)
	}
}

func TestExtractStorageBucketFullAttributes(t *testing.T) {
	const data = `{
		"id": "demo-bucket",
		"name": "demo-bucket",
		"location": "US-CENTRAL1",
		"locationType": "region",
		"storageClass": "STANDARD",
		"timeCreated": "2024-06-01T00:00:00.000Z",
		"updated": "2026-06-24T12:00:00.000Z",
		"iamConfiguration": {
			"uniformBucketLevelAccess": {"enabled": true},
			"publicAccessPrevention": "enforced"
		},
		"encryption": {"defaultKmsKeyName": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1"},
		"versioning": {"enabled": true},
		"lifecycle": {"rule": [
			{"action": {"type": "Delete"}, "condition": {"age": 30}},
			{"action": {"type": "SetStorageClass", "storageClass": "NEARLINE"}, "condition": {"age": 90}}
		]},
		"logging": {"logBucket": "audit-logs", "logObjectPrefix": "demo-bucket"},
		"retentionPolicy": {"retentionPeriod": "2592000", "isLocked": true}
	}`

	got, err := extractStorageBucket(storageBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location":                    "US-CENTRAL1",
		"location_type":               "region",
		"storage_class":               "STANDARD",
		"time_created":                "2024-06-01T00:00:00Z",
		"updated":                     "2026-06-24T12:00:00Z",
		"uniform_bucket_level_access": true,
		"public_access_prevention":    "enforced",
		"kms_key_name":                "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		"versioning_enabled":          true,
		"lifecycle_rule_count":        2,
		"retention_period_seconds":    int64(2592000),
		"retention_policy_locked":     true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		"//storage.googleapis.com/projects/_/buckets/audit-logs",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	assertRelationship(t, got.Relationships, relationshipTypeStorageBucketKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeStorageBucketLogsToBucket,
		"//storage.googleapis.com/projects/_/buckets/audit-logs", assetTypeStorageBucket)
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != storageBucketFullResourceName {
			t.Errorf("relationship source = %q, want bucket full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeStorageBucket {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeStorageBucket)
		}
	}
}

func TestExtractStorageBucketMinimal(t *testing.T) {
	const data = `{
		"id": "raw-bucket",
		"name": "raw-bucket",
		"location": "US",
		"storageClass": "STANDARD"
	}`
	got, err := extractStorageBucket(storageBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["location"] != "US" {
		t.Errorf("location = %v, want US", got.Attributes["location"])
	}
	if _, ok := got.Attributes["uniform_bucket_level_access"]; ok {
		t.Errorf("expected no uniform_bucket_level_access when iamConfiguration absent, got %#v", got.Attributes)
	}
	if _, ok := got.Attributes["kms_key_name"]; ok {
		t.Errorf("expected no kms_key_name when encryption absent, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges without KMS/logging, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors without KMS/logging, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractStorageBucketPublicAccessPosture(t *testing.T) {
	const data = `{
		"iamConfiguration": {
			"uniformBucketLevelAccess": {"enabled": false},
			"publicAccessPrevention": "inherited"
		}
	}`
	got, err := extractStorageBucket(storageBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["uniform_bucket_level_access"] != false {
		t.Errorf("uniform_bucket_level_access = %v, want false", got.Attributes["uniform_bucket_level_access"])
	}
	if got.Attributes["public_access_prevention"] != "inherited" {
		t.Errorf("public_access_prevention = %v, want inherited", got.Attributes["public_access_prevention"])
	}
}

func TestExtractStorageBucketNoRawIAMPolicyOrObjectDataLeakage(t *testing.T) {
	const data = `{
		"id": "demo-bucket",
		"name": "demo-bucket",
		"location": "US",
		"acl": [{"entity": "user-secret.person@example.com", "role": "OWNER"}],
		"iamConfiguration": {"uniformBucketLevelAccess": {"enabled": true}}
	}`
	got, err := extractStorageBucket(storageBucketContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"secret.person@example.com", "user-secret.person", "acl", "entity"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked ACL/member token %q: %s", banned, blob)
		}
	}
}

func TestExtractStorageBucketMalformedDataErrors(t *testing.T) {
	_, err := extractStorageBucket(storageBucketContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
