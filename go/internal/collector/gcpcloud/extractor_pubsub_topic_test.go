// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const pubSubTopicFullName = "//pubsub.googleapis.com/projects/demo-project/topics/orders"

func pubSubTopicContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: pubSubTopicFullName,
		AssetType:        assetTypePubSubTopic,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestPubSubTopicExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypePubSubTopic); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypePubSubTopic)
	}
}

func TestExtractPubSubTopicFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/topics/orders",
		"labels": {"team": "platform", "env": "prod"},
		"kmsKeyName": "projects/demo-project/locations/us-central1/keyRings/pubsub/cryptoKeys/topics",
		"messageStoragePolicy": {"allowedPersistenceRegions": ["us-east1", "us-central1", "us-central1"], "enforceInTransit": true},
		"schemaSettings": {"schema": "projects/demo-project/schemas/order-event", "encoding": "JSON"},
		"messageRetentionDuration": "86400s",
		"state": "ACTIVE"
	}`

	got, err := extractPubSubTopic(pubSubTopicContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"state":                       "ACTIVE",
		"customer_managed_encryption": true,
		"message_storage_regions":     []string{"us-central1", "us-east1"},
		"message_storage_enforced":    true,
		"schema_encoding":             "JSON",
		"message_retention_duration":  "86400s",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/pubsub/cryptoKeys/topics",
		"//pubsub.googleapis.com/projects/demo-project/schemas/order-event",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected 1 KMS edge + 1 schema edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTopicEncryptedByKMSKey,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/pubsub/cryptoKeys/topics", assetTypeKMSCryptoKey)
	assertRelationship(t, got.Relationships, relationshipTypeTopicUsesSchema,
		"//pubsub.googleapis.com/projects/demo-project/schemas/order-event", assetTypePubSubSchema)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != pubSubTopicFullName {
		t.Errorf("relationship source = %q, want topic full name", rel.SourceFullResourceName)
	}
	if rel.SourceAssetType != assetTypePubSubTopic {
		t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypePubSubTopic)
	}
}

func TestExtractPubSubTopicBareTopicYieldsNothing(t *testing.T) {
	// A topic with no CMEK, schema, storage policy, or retention is a valid
	// resource that simply carries no typed depth beyond the base observation.
	const data = `{"name": "projects/demo-project/topics/plain", "state": "ACTIVE"}`
	got, err := extractPubSubTopic(pubSubTopicContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"state": "ACTIVE"}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for a bare topic, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractPubSubTopicEmptyStoragePolicyOmitsRegions(t *testing.T) {
	// An empty allowedPersistenceRegions list must not fabricate a regions
	// attribute; enforceInTransit=false is a default and is also omitted.
	const data = `{"messageStoragePolicy": {"allowedPersistenceRegions": [], "enforceInTransit": false}}`
	got, err := extractPubSubTopic(pubSubTopicContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["message_storage_regions"]; ok {
		t.Errorf("empty regions must omit message_storage_regions: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["message_storage_enforced"]; ok {
		t.Errorf("enforceInTransit=false must omit message_storage_enforced: %#v", got.Attributes)
	}
}

func TestExtractPubSubTopicSchemaWithoutEncoding(t *testing.T) {
	// A schemaSettings block naming a schema but no encoding still resolves the
	// schema edge; the encoding attribute is simply omitted.
	const data = `{"schemaSettings": {"schema": "projects/demo-project/schemas/s"}}`
	got, err := extractPubSubTopic(pubSubTopicContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["schema_encoding"]; ok {
		t.Errorf("missing encoding must omit schema_encoding: %#v", got.Attributes)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTopicUsesSchema,
		"//pubsub.googleapis.com/projects/demo-project/schemas/s", assetTypePubSubSchema)
}

func TestExtractPubSubTopicDeletedSchemaEmitsNoEdge(t *testing.T) {
	// Pub/Sub reports a detached/deleted schema as the sentinel "_deleted-schema_".
	// That is not a resolvable Schema resource, so no edge or anchor is emitted.
	const data = `{"schemaSettings": {"schema": "_deleted-schema_", "encoding": "JSON"}}`
	got, err := extractPubSubTopic(pubSubTopicContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("deleted schema sentinel must emit no edge, got %#v", got.Relationships)
	}
	if got.Attributes["schema_encoding"] != "JSON" {
		t.Errorf("schema_encoding should still be captured: %#v", got.Attributes)
	}
}

func TestExtractPubSubTopicEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractPubSubTopic(pubSubTopicContext(`{}`))
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

func TestExtractPubSubTopicMalformedDataErrors(t *testing.T) {
	if _, err := extractPubSubTopic(pubSubTopicContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	// Empty/nil resource data (unexpected end of JSON input) must also error
	// rather than silently yield a zero extraction.
	if _, err := extractPubSubTopic(pubSubTopicContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

// TestExtractPubSubTopicWrongDomainKMSKeyEmitsNoEdgeOrAnchor proves the Pub/Sub
// Topic extractor now drops a wrong-domain absolute kmsKeyName after converging
// onto the shared strict cmekKeyFullResourceName. Before consolidation this
// extractor used a permissive helper that returned any //-prefixed value
// unchanged, which would have minted a bogus encryption edge to a non-KMS
// endpoint. Real Cloud Asset Inventory never emits such a value for kmsKeyName,
// so this hardens a latent fabricated-edge path without changing valid-input
// behavior. The valid-input normalization itself is covered by
// TestCMEKKeyFullResourceName.
func TestExtractPubSubTopicWrongDomainKMSKeyEmitsNoEdgeOrAnchor(t *testing.T) {
	raw := `{"kmsKeyName": "//pubsub.googleapis.com/projects/p/topics/t"}`
	got, err := extractPubSubTopic(ExtractContext{
		FullResourceName: "//pubsub.googleapis.com/projects/p/topics/topic",
		AssetType:        assetTypePubSubTopic,
		ProjectID:        "p",
		Data:             []byte(raw),
	})
	if err != nil {
		t.Fatalf("extractPubSubTopic returned error: %v", err)
	}
	if _, ok := got.Attributes["customer_managed_encryption"]; ok {
		t.Errorf("wrong-domain kmsKeyName must not set customer_managed_encryption: %#v", got.Attributes)
	}
	for _, anchor := range got.CorrelationAnchors {
		if anchor == "//pubsub.googleapis.com/projects/p/topics/t" {
			t.Errorf("wrong-domain kmsKeyName leaked as anchor: %q", anchor)
		}
	}
	for _, rel := range got.Relationships {
		if rel.RelationshipType == relationshipTypeTopicEncryptedByKMSKey {
			t.Errorf("wrong-domain kmsKeyName minted an encryption edge: %+v", rel)
		}
	}
}

func TestPubSubTopicSchemaFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative schema", "projects/p/schemas/s", "//pubsub.googleapis.com/projects/p/schemas/s"},
		{"leading slash", "/projects/p/schemas/s", "//pubsub.googleapis.com/projects/p/schemas/s"},
		{"already full name", "//pubsub.googleapis.com/projects/p/schemas/s", "//pubsub.googleapis.com/projects/p/schemas/s"},
		{"deleted sentinel", "_deleted-schema_", ""},
		{"not a schema", "projects/p/topics/t", ""},
		{"leading slash non-schema", "/projects/p/topics/t", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pubSubTopicSchemaFullName(tc.in); got != tc.want {
				t.Errorf("pubSubTopicSchemaFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
