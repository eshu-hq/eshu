// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const redisInstanceFullName = "//redis.googleapis.com/projects/demo-project/locations/us-central1/instances/cache-primary"

func redisInstanceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: redisInstanceFullName,
		AssetType:        assetTypeRedisInstance,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestRedisInstanceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeRedisInstance); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeRedisInstance)
	}
}

func TestExtractRedisInstanceStandardHAWithNetworkAndCMEK(t *testing.T) {
	const data = `{
		"locationId": "us-central1-a",
		"redisVersion": "REDIS_7_0",
		"tier": "STANDARD_HA",
		"memorySizeGb": 5,
		"authorizedNetwork": "projects/demo-project/global/networks/prod-vpc",
		"connectMode": "PRIVATE_SERVICE_ACCESS",
		"transitEncryptionMode": "SERVER_AUTHENTICATION",
		"authEnabled": true,
		"state": "READY",
		"createTime": "2024-06-01T00:00:00Z",
		"replicaCount": 2,
		"readReplicasMode": "READ_REPLICAS_ENABLED",
		"customerManagedKey": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key",
		"persistenceConfig": {"persistenceMode": "RDB"},
		"host": "10.0.0.5",
		"port": 6379
	}`

	got, err := extractRedisInstance(redisInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location_id":             "us-central1-a",
		"redis_version":           "REDIS_7_0",
		"tier":                    "STANDARD_HA",
		"memory_size_gb":          int64(5),
		"connect_mode":            "PRIVATE_SERVICE_ACCESS",
		"transit_encryption_mode": "SERVER_AUTHENTICATION",
		"auth_enabled":            true,
		"state":                   "READY",
		"creation_time":           "2024-06-01T00:00:00Z",
		"replica_count":           int64(2),
		"read_replicas_mode":      "READ_REPLICAS_ENABLED",
		"customer_managed_key":    "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key",
		"persistence_mode":        "RDB",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 edges (network, kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRedisInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeRedisInstanceEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key", assetTypeKMSCryptoKey)

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractRedisInstanceBasicTierNoNetworkNoKMS(t *testing.T) {
	const data = `{
		"locationId": "us-east1-b",
		"redisVersion": "REDIS_6_X",
		"tier": "BASIC",
		"memorySizeGb": 1,
		"connectMode": "DIRECT_PEERING",
		"transitEncryptionMode": "DISABLED",
		"authEnabled": false,
		"state": "READY"
	}`

	got, err := extractRedisInstance(redisInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["auth_enabled"] != false {
		t.Errorf("auth_enabled = %v, want false", got.Attributes["auth_enabled"])
	}
	if got.Attributes["tier"] != "BASIC" {
		t.Errorf("tier = %v, want BASIC", got.Attributes["tier"])
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["replica_count"]; ok {
		t.Errorf("replica_count should be absent when unset: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["customer_managed_key"]; ok {
		t.Errorf("customer_managed_key should be absent when unset: %#v", got.Attributes)
	}
}

func TestExtractRedisInstanceAuthorizedNetworkFullSelfLink(t *testing.T) {
	const data = `{
		"authorizedNetwork": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc"
	}`

	got, err := extractRedisInstance(redisInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRedisInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractRedisInstanceAuthorizedNetworkProjectLessPartialResolvedAgainstProject(t *testing.T) {
	const data = `{
		"authorizedNetwork": "global/networks/prod-vpc"
	}`

	got, err := extractRedisInstance(redisInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeRedisInstanceInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc", assetTypeComputeNetwork)
}

func TestExtractRedisInstanceCMEKAlreadyCAIPrefixedNotDoublePrefixed(t *testing.T) {
	const data = `{
		"customerManagedKey": "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key"
	}`

	got, err := extractRedisInstance(redisInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRelationship(t, got.Relationships, relationshipTypeRedisInstanceEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key", assetTypeKMSCryptoKey)
	if got.Attributes["customer_managed_key"] != "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key" {
		t.Errorf("customer_managed_key = %v, want bare relative form matching the anchor/edge normalization", got.Attributes["customer_managed_key"])
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeRedisInstanceEncryptedByKMSKey {
			if got := rel.TargetFullResourceName; got != "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key" {
				t.Fatalf("kms target double-prefixed or malformed: %q", got)
			}
		}
	}
}

// TestExtractRedisInstanceCMEKLeadingSlashNormalizesSameAsBare proves a
// leading-slash customerManagedKey value normalizes identically to the same
// reference without the leading slash, for both the stored attribute and the
// anchor/edge target. Before the fix for the Copilot review on PR #4563, the
// attribute stored the raw (unnormalized) string while the anchor/edge used
// the normalized string, so a leading slash produced two different
// representations of the same underlying key.
func TestExtractRedisInstanceCMEKLeadingSlashNormalizesSameAsBare(t *testing.T) {
	const dataWithSlash = `{
		"customerManagedKey": "/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key"
	}`
	const dataBare = `{
		"customerManagedKey": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key"
	}`

	gotSlash, err := extractRedisInstance(redisInstanceContext(dataWithSlash))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotBare, err := extractRedisInstance(redisInstanceContext(dataBare))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttr := "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key"
	if gotSlash.Attributes["customer_managed_key"] != wantAttr {
		t.Errorf("leading-slash customer_managed_key = %v, want %v", gotSlash.Attributes["customer_managed_key"], wantAttr)
	}
	if gotSlash.Attributes["customer_managed_key"] != gotBare.Attributes["customer_managed_key"] {
		t.Errorf("leading-slash and bare customer_managed_key differ: %v vs %v",
			gotSlash.Attributes["customer_managed_key"], gotBare.Attributes["customer_managed_key"])
	}

	wantTarget := "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/redis-key"
	assertRelationship(t, gotSlash.Relationships, relationshipTypeRedisInstanceEncryptedByKMSKey, wantTarget, assetTypeKMSCryptoKey)
	assertRelationship(t, gotBare.Relationships, relationshipTypeRedisInstanceEncryptedByKMSKey, wantTarget, assetTypeKMSCryptoKey)

	if !reflect.DeepEqual(gotSlash.CorrelationAnchors, gotBare.CorrelationAnchors) {
		t.Errorf("leading-slash and bare anchors differ: %v vs %v", gotSlash.CorrelationAnchors, gotBare.CorrelationAnchors)
	}
}

func TestExtractRedisInstanceNeverPersistsHostPortOrIPRanges(t *testing.T) {
	const data = `{
		"host": "10.0.0.5",
		"port": 6379,
		"readEndpoint": "10.0.0.6",
		"readEndpointPort": 6380,
		"reservedIpRange": "10.0.0.0/29",
		"secondaryIpRange": "10.0.1.0/29"
	}`
	got, err := extractRedisInstance(redisInstanceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(blob)
	for _, token := range []string{"10.0.0.5", "10.0.0.6", "6379", "6380", "10.0.0.0/29", "10.0.1.0/29"} {
		if containsString(s, token) {
			t.Fatalf("redis instance extraction leaked sensitive token %q: %s", token, blob)
		}
	}
}

func TestExtractRedisInstanceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractRedisInstance(redisInstanceContext(`{}`))
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

func TestExtractRedisInstanceMalformedDataErrors(t *testing.T) {
	if _, err := extractRedisInstance(redisInstanceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
