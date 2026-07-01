// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const (
	secretVersionFullName = "//secretmanager.googleapis.com/projects/demo-project/secrets/api-token/versions/3"
	secretVersionParent   = "//secretmanager.googleapis.com/projects/demo-project/secrets/api-token"
)

func secretVersionContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: secretVersionFullName,
		AssetType:        secretVersionAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSecretVersionExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(secretVersionAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", secretVersionAssetType)
	}
}

func TestExtractSecretVersionFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/secrets/api-token/versions/3",
		"state": "ENABLED",
		"createTime": "2024-06-01T00:00:00Z",
		"replicationStatus": {
			"userManaged": {
				"replicas": [
					{"location": "us-central1", "customerManagedEncryption": {"kmsKeyVersionName": "projects/demo-project/locations/us-central1/keyRings/secrets/cryptoKeys/primary/cryptoKeyVersions/5"}}
				]
			}
		}
	}`
	got, err := extractSecretVersion(secretVersionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"state":                       "ENABLED",
		"creation_time":               "2024-06-01T00:00:00Z",
		"replication_type":            "user_managed",
		"customer_managed_encryption": true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected parent-secret + kms-key-version edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSecretVersionOfSecret, secretVersionParent, secretManagerSecretAssetType)
	assertRelationship(t, got.Relationships, relationshipTypeSecretVersionEncryptedByKMSKeyVersion,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/secrets/cryptoKeys/primary/cryptoKeyVersions/5", assetTypeKMSCryptoKeyVersion)
	wantAnchors := []string{"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/secrets/cryptoKeys/primary/cryptoKeyVersions/5"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractSecretVersionDestroyed(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/secrets/api-token/versions/1",
		"state": "DESTROYED",
		"createTime": "2023-01-01T00:00:00Z",
		"destroyTime": "2024-01-01T00:00:00Z",
		"replicationStatus": {"automatic": {}}
	}`
	got, err := extractSecretVersion(secretVersionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["state"] != "DESTROYED" {
		t.Errorf("state = %v, want DESTROYED", got.Attributes["state"])
	}
	if got.Attributes["destroy_time"] != "2024-01-01T00:00:00Z" {
		t.Errorf("destroy_time = %v", got.Attributes["destroy_time"])
	}
	if got.Attributes["replication_type"] != "automatic" {
		t.Errorf("replication_type = %v, want automatic", got.Attributes["replication_type"])
	}
	if _, ok := got.Attributes["customer_managed_encryption"]; ok {
		t.Errorf("no CMEK present; flag must be omitted: %#v", got.Attributes)
	}
	// Only the parent-secret edge, no KMS edge.
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the parent-secret edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSecretVersionOfSecret, secretVersionParent, secretManagerSecretAssetType)
}

func TestExtractSecretVersionNeverPersistsPayload(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/secrets/api-token/versions/3",
		"state": "ENABLED",
		"payload": {"data": "c3VwZXItc2VjcmV0LXZhbHVl"},
		"secretData": "super-secret-value"
	}`
	got, err := extractSecretVersion(secretVersionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"c3VwZXItc2VjcmV0LXZhbHVl", "super-secret-value", "payload", "secretData"} {
		if containsString(string(blob), token) {
			t.Fatalf("secret version extraction leaked payload token %q: %s", token, blob)
		}
	}
}

func TestExtractSecretVersionEmptyDataYieldsOnlyParentEdge(t *testing.T) {
	got, err := extractSecretVersion(secretVersionContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Errorf("expected the parent-secret edge from the full name, got %#v", got.Relationships)
	}
}

func TestExtractSecretVersionMalformedDataErrors(t *testing.T) {
	if _, err := extractSecretVersion(secretVersionContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestParentSecretFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"version name", secretVersionFullName, secretVersionParent},
		{"no versions segment", secretVersionParent, ""},
		{"trailing versions marker, no id", secretVersionParent + "/versions/", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parentSecretFullName(tc.in); got != tc.want {
				t.Errorf("parentSecretFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
