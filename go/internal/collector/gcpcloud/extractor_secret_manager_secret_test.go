// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const secretManagerSecretFullName = "//secretmanager.googleapis.com/projects/demo-project/secrets/api-token"

func secretManagerSecretContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: secretManagerSecretFullName,
		AssetType:        secretManagerSecretAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSecretManagerSecretExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(secretManagerSecretAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", secretManagerSecretAssetType)
	}
}

func TestExtractSecretManagerSecretFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/secrets/api-token",
		"replication": {
			"userManaged": {
				"replicas": [
					{"location": "us-central1", "customerManagedEncryption": {"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/secrets/cryptoKeys/primary"}},
					{"location": "us-east1", "customerManagedEncryption": {"kmsKeyName": "projects/demo-project/locations/us-east1/keyRings/secrets/cryptoKeys/replica"}}
				]
			}
		},
		"createTime": "2024-06-01T00:00:00Z",
		"labels": {"team": "platform", "env": "prod"},
		"topics": [{"name": "projects/demo-project/topics/secret-rotation"}],
		"expireTime": "2027-01-01T00:00:00Z",
		"rotation": {"nextRotationTime": "2026-09-01T00:00:00Z", "rotationPeriod": "7776000s"},
		"versionAliases": {"current": "3", "previous": "2"}
	}`

	got, err := extractSecretManagerSecret(secretManagerSecretContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"replication_type":            "user_managed",
		"replication_location_count":  2,
		"customer_managed_encryption": true,
		"creation_time":               "2024-06-01T00:00:00Z",
		"expire_time":                 "2027-01-01T00:00:00Z",
		"next_rotation_time":          "2026-09-01T00:00:00Z",
		"rotation_period":             "7776000s",
		"topic_count":                 1,
		"version_alias_count":         2,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/secrets/cryptoKeys/primary",
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-east1/keyRings/secrets/cryptoKeys/replica",
		"//pubsub.googleapis.com/projects/demo-project/topics/secret-rotation",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 3 {
		t.Fatalf("expected 2 KMS edges + 1 topic edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSecretEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/secrets/cryptoKeys/primary", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeSecretEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-east1/keyRings/secrets/cryptoKeys/replica", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeSecretNotifiesTopic,
		"//pubsub.googleapis.com/projects/demo-project/topics/secret-rotation", assetTypePubSubTopic)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != secretManagerSecretFullName {
		t.Errorf("relationship source = %q, want secret full name", rel.SourceFullResourceName)
	}
	if rel.SourceAssetType != secretManagerSecretAssetType {
		t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, secretManagerSecretAssetType)
	}
}

func TestExtractSecretManagerSecretAutomaticReplication(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/secrets/legacy-key",
		"replication": {"automatic": {}},
		"createTime": "2023-01-15T09:30:00Z"
	}`
	got, err := extractSecretManagerSecret(secretManagerSecretContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{
		"replication_type": "automatic",
		"creation_time":    "2023-01-15T09:30:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for a CMEK-less automatic secret, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractSecretManagerSecretUserManagedEmptyReplicasOmitsCount(t *testing.T) {
	// A user-managed replication policy with no reported replicas must not
	// fabricate a "0 locations" posture: replication_type stays, the count is
	// omitted, consistent with the other *_count attributes.
	const data = `{"replication": {"userManaged": {"replicas": []}}}`
	got, err := extractSecretManagerSecret(secretManagerSecretContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["replication_type"] != "user_managed" {
		t.Errorf("replication_type = %v, want user_managed", got.Attributes["replication_type"])
	}
	if _, ok := got.Attributes["replication_location_count"]; ok {
		t.Errorf("empty replicas must omit replication_location_count: %#v", got.Attributes)
	}
}

func TestExtractSecretManagerSecretAutomaticCMEK(t *testing.T) {
	const data = `{
		"replication": {"automatic": {"customerManagedEncryption": {"kmsKeyName": "projects/demo-project/locations/global/keyRings/kr/cryptoKeys/k"}}}
	}`
	got, err := extractSecretManagerSecret(secretManagerSecretContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["replication_type"] != "automatic" {
		t.Errorf("replication_type = %v, want automatic", got.Attributes["replication_type"])
	}
	if got.Attributes["customer_managed_encryption"] != true {
		t.Errorf("customer_managed_encryption = %v, want true", got.Attributes["customer_managed_encryption"])
	}
	if _, ok := got.Attributes["replication_location_count"]; ok {
		t.Errorf("automatic replication must not report a location count: %#v", got.Attributes)
	}
	assertRelationship(t, got.Relationships, relationshipTypeSecretEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/global/keyRings/kr/cryptoKeys/k", assetTypeKMSCryptoKey)
}

func TestExtractSecretManagerSecretNeverPersistsPayloads(t *testing.T) {
	// A CAI Secret asset carries no payload, but guard the boundary anyway: if a
	// caller ever hands us a blob with a stray payload field, it must not leak.
	const data = `{
		"name": "projects/demo-project/secrets/api-token",
		"replication": {"automatic": {}},
		"payload": {"data": "c3VwZXItc2VjcmV0LXZhbHVl"},
		"secretData": "super-secret-value"
	}`
	got, err := extractSecretManagerSecret(secretManagerSecretContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, token := range []string{"c3VwZXItc2VjcmV0LXZhbHVl", "super-secret-value", "payload", "secretData"} {
		if containsString(string(blob), token) {
			t.Fatalf("secret extraction leaked payload token %q: %s", token, blob)
		}
	}
}

func TestExtractSecretManagerSecretEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractSecretManagerSecret(secretManagerSecretContext(`{}`))
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

func TestExtractSecretManagerSecretMalformedDataErrors(t *testing.T) {
	if _, err := extractSecretManagerSecret(secretManagerSecretContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestSecretManagerSecretTTLExpiration(t *testing.T) {
	// The ttl form of the expiration oneof is a duration string, kept verbatim.
	const data = `{"replication": {"automatic": {}}, "ttl": "86400s"}`
	got, err := extractSecretManagerSecret(secretManagerSecretContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["ttl"] != "86400s" {
		t.Errorf("ttl = %v, want 86400s", got.Attributes["ttl"])
	}
	if _, ok := got.Attributes["expire_time"]; ok {
		t.Errorf("ttl form must not also produce expire_time: %#v", got.Attributes)
	}
}

// TestExtractSecretManagerWrongDomainKMSKeyEmitsNoEdgeOrAnchor proves the Secret
// Manager Secret extractor now drops a wrong-domain absolute CMEK kmsKeyName
// after converging onto the shared strict cmekKeyFullResourceName. Before
// consolidation this extractor used a permissive helper that returned any
// //-prefixed value unchanged. Real Cloud Asset Inventory never emits such a
// value; the valid-input normalization is covered by TestCMEKKeyFullResourceName.
func TestExtractSecretManagerWrongDomainKMSKeyEmitsNoEdgeOrAnchor(t *testing.T) {
	raw := `{"replication":{"automatic":{"customerManagedEncryption":{"kmsKeyName":"//pubsub.googleapis.com/projects/p/topics/t"}}}}`
	got, err := extractSecretManagerSecret(ExtractContext{
		FullResourceName: "//secretmanager.googleapis.com/projects/p/secrets/s",
		AssetType:        secretManagerSecretAssetType,
		ProjectID:        "p",
		Data:             []byte(raw),
	})
	if err != nil {
		t.Fatalf("extractSecretManagerSecret returned error: %v", err)
	}
	for _, anchor := range got.CorrelationAnchors {
		if anchor == "//pubsub.googleapis.com/projects/p/topics/t" {
			t.Errorf("wrong-domain kmsKeyName leaked as anchor: %q", anchor)
		}
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeSecretEncryptedByKMSKey {
			t.Errorf("wrong-domain kmsKeyName minted an encryption edge: %+v", rel)
		}
	}
}

func TestSecretManagerTopicFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative topic", "projects/p/topics/t", "//pubsub.googleapis.com/projects/p/topics/t"},
		{"leading slash", "/projects/p/topics/t", "//pubsub.googleapis.com/projects/p/topics/t"},
		{"already full name", "//pubsub.googleapis.com/projects/p/topics/t", "//pubsub.googleapis.com/projects/p/topics/t"},
		{"not a topic", "projects/p/subscriptions/s", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secretManagerTopicFullName(tc.in); got != tc.want {
				t.Errorf("secretManagerTopicFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
