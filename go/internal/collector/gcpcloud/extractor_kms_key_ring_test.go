// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const kmsKeyRingFullResourceName = "//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk"

func kmsKeyRingContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: kmsKeyRingFullResourceName,
		AssetType:        assetTypeKMSKeyRing,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestKMSKeyRingExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeKMSKeyRing); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeKMSKeyRing)
	}
}

// TestExtractKMSKeyRingRealCAIShape uses the real Cloud KMS v1 KeyRing
// resource shape: per Google's projects.locations.keyRings REST reference,
// the resource has exactly two fields, name and createTime. The location is
// not a separate field; it is derived from the resource-name path segment
// (projects/*/locations/*/keyRings/*), the same derivation pattern used by
// the sibling CryptoKey extractor for its key_ring attribute.
func TestExtractKMSKeyRingRealCAIShape(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us/keyRings/rk",
		"createTime": "2024-06-01T00:00:00Z"
	}`

	got, err := extractKMSKeyRing(kmsKeyRingContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location":      "us",
		"creation_time": "2024-06-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("KeyRing extractor must emit no outbound edges (CryptoKeys point in via kms_crypto_key_in_key_ring), got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("KeyRing extractor must emit no anchors, got %#v", got.CorrelationAnchors)
	}
}

// TestExtractKMSKeyRingLocationFallsBackToFullResourceName proves a sparse or
// stripped CAI resource.data.name (missing on some CAI pages) does not
// silently drop the location attribute: the extractor falls back to the
// asset's own normalized ctx.FullResourceName, mirroring the CryptoKey
// extractor's KeyRing-derivation fallback.
func TestExtractKMSKeyRingLocationFallsBackToFullResourceName(t *testing.T) {
	got, err := extractKMSKeyRing(kmsKeyRingContext(`{"createTime": "2024-06-01T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["location"] != "us" {
		t.Errorf("location = %v, want us (derived from ctx.FullResourceName fallback)", got.Attributes["location"])
	}
}

func TestExtractKMSKeyRingEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractKMSKeyRing(ExtractContext{
		FullResourceName: "",
		AssetType:        assetTypeKMSKeyRing,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data and no resolvable name, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships, got %#v", got.Relationships)
	}
}

func TestExtractKMSKeyRingMalformedDataErrors(t *testing.T) {
	if _, err := extractKMSKeyRing(kmsKeyRingContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

// TestExtractKMSKeyRingIgnoresStrayCryptoKeyShape proves the KeyRing
// extractor never surfaces a cryptoKeys/purpose-shaped attribute even if a
// stray field of that shape appears in the blob, since CryptoKeys are a
// separate CAI asset type owned by extractor_kms_crypto_key.go and the
// KeyRing->CryptoKey relationship is inbound-only from the CryptoKey side.
func TestExtractKMSKeyRingIgnoresStrayCryptoKeyShape(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us/keyRings/rk",
		"createTime": "2024-06-01T00:00:00Z",
		"cryptoKeys": [{"name": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1", "purpose": "ENCRYPT_DECRYPT"}]
	}`
	got, err := extractKMSKeyRing(kmsKeyRingContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("KeyRing extractor must not emit an outbound edge from a stray cryptoKeys shape: %#v", got.Relationships)
	}
	if _, ok := got.Attributes["purpose"]; ok {
		t.Fatalf("KeyRing extractor must not surface a CryptoKey-shaped attribute: %#v", got.Attributes)
	}
}
