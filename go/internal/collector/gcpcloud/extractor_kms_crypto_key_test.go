// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const kmsCryptoKeyFullResourceName = "//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1"

func kmsCryptoKeyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: kmsCryptoKeyFullResourceName,
		AssetType:        assetTypeKMSCryptoKey,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestKMSCryptoKeyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeKMSCryptoKey); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeKMSCryptoKey)
	}
}

func TestExtractKMSCryptoKeyFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		"purpose": "ENCRYPT_DECRYPT",
		"createTime": "2024-06-01T00:00:00Z",
		"nextRotationTime": "2026-09-01T00:00:00Z",
		"rotationPeriod": "7776000s",
		"versionTemplate": {
			"protectionLevel": "SOFTWARE",
			"algorithm": "GOOGLE_SYMMETRIC_ENCRYPTION"
		},
		"primary": {
			"name": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1/cryptoKeyVersions/1",
			"state": "ENABLED",
			"createTime": "2024-06-01T00:00:05Z"
		},
		"labels": {"team": "platform"}
	}`

	got, err := extractKMSCryptoKey(kmsCryptoKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"key_ring":           "projects/demo-project/locations/us/keyRings/rk",
		"purpose":            "ENCRYPT_DECRYPT",
		"protection_level":   "SOFTWARE",
		"algorithm":          "GOOGLE_SYMMETRIC_ENCRYPTION",
		"rotation_period":    "7776000s",
		"next_rotation_time": "2026-09-01T00:00:00Z",
		"primary_state":      "ENABLED",
		"creation_time":      "2024-06-01T00:00:00Z",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	wantAnchors := []string{"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk"}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 keyring edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeCryptoKeyInKeyRing,
		"//cloudkms.googleapis.com/projects/demo-project/locations/us/keyRings/rk", assetTypeKMSKeyRing)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != kmsCryptoKeyFullResourceName {
		t.Errorf("relationship source = %q, want crypto key full name", rel.SourceFullResourceName)
	}
	if rel.SourceAssetType != assetTypeKMSCryptoKey {
		t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeKMSCryptoKey)
	}
}

func TestExtractKMSCryptoKeyAsymmetricSign(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/sign-key",
		"purpose": "ASYMMETRIC_SIGN",
		"versionTemplate": {
			"protectionLevel": "HSM",
			"algorithm": "RSA_SIGN_PSS_4096_SHA512"
		}
	}`
	got, err := extractKMSCryptoKey(kmsCryptoKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["purpose"] != "ASYMMETRIC_SIGN" {
		t.Errorf("purpose = %v, want ASYMMETRIC_SIGN", got.Attributes["purpose"])
	}
	if got.Attributes["protection_level"] != "HSM" {
		t.Errorf("protection_level = %v, want HSM", got.Attributes["protection_level"])
	}
	if got.Attributes["algorithm"] != "RSA_SIGN_PSS_4096_SHA512" {
		t.Errorf("algorithm = %v, want RSA_SIGN_PSS_4096_SHA512", got.Attributes["algorithm"])
	}
	if _, ok := got.Attributes["rotation_period"]; ok {
		t.Errorf("asymmetric key must not report a rotation_period: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["next_rotation_time"]; ok {
		t.Errorf("asymmetric key must not report a next_rotation_time: %#v", got.Attributes)
	}
}

func TestExtractKMSCryptoKeyNoPrimaryOmitsPrimaryState(t *testing.T) {
	// A key pending its first version has no primary yet; primary_state must be
	// omitted rather than fabricated as an empty string.
	const data = `{"name": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/new-key", "purpose": "ENCRYPT_DECRYPT"}`
	got, err := extractKMSCryptoKey(kmsCryptoKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["primary_state"]; ok {
		t.Errorf("key with no primary version must omit primary_state: %#v", got.Attributes)
	}
}

func TestExtractKMSCryptoKeyDerivesKeyRingFromOwnName(t *testing.T) {
	// The KeyRing edge and correlation anchor derive from the CryptoKey's own
	// resource name path, not from a separate field CAI does not report.
	const data = `{"name": "projects/p/locations/l/keyRings/ring-a/cryptoKeys/k"}`
	got, err := extractKMSCryptoKey(kmsCryptoKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantBare := "projects/p/locations/l/keyRings/ring-a"
	if got.Attributes["key_ring"] != wantBare {
		t.Errorf("key_ring = %v, want %v", got.Attributes["key_ring"], wantBare)
	}
	wantFullName := "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/ring-a"
	assertRelationship(t, got.Relationships, relationshipTypeCryptoKeyInKeyRing, wantFullName, assetTypeKMSKeyRing)
}

func TestExtractKMSCryptoKeyNeverPersistsKeyMaterial(t *testing.T) {
	// CAI never reports key material for a CryptoKey, but guard the boundary
	// anyway: if a caller ever hands us a blob with a stray key-material-shaped
	// field, it must not leak.
	const data = `{
		"name": "projects/demo-project/locations/us/keyRings/rk/cryptoKeys/key1",
		"purpose": "ENCRYPT_DECRYPT",
		"keyMaterial": "c3VwZXItc2VjcmV0LWtleS1tYXRlcmlhbA==",
		"pemCrt": "-----BEGIN CERTIFICATE-----fake-----END CERTIFICATE-----"
	}`
	got, err := extractKMSCryptoKey(kmsCryptoKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, token := range []string{"c3VwZXItc2VjcmV0LWtleS1tYXRlcmlhbA==", "BEGIN CERTIFICATE", "keyMaterial", "pemCrt"} {
		if containsString(string(blob), token) {
			t.Fatalf("crypto key extraction leaked key material token %q: %s", token, blob)
		}
	}
}

func TestExtractKMSCryptoKeyEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractKMSCryptoKey(kmsCryptoKeyContext(`{}`))
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

func TestExtractKMSCryptoKeyMalformedDataErrors(t *testing.T) {
	if _, err := extractKMSCryptoKey(kmsCryptoKeyContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestKMSKeyRingRelativeNameFromCryptoKeyName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"relative crypto key name",
			"projects/p/locations/l/keyRings/r/cryptoKeys/k",
			"projects/p/locations/l/keyRings/r",
		},
		{
			"full resource name",
			"//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k",
			"projects/p/locations/l/keyRings/r",
		},
		{"missing keyRings segment", "projects/p/locations/l/somethingElse/r/cryptoKeys/k", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kmsKeyRingRelativeNameFromCryptoKeyName(tc.in); got != tc.want {
				t.Errorf("kmsKeyRingRelativeNameFromCryptoKeyName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
