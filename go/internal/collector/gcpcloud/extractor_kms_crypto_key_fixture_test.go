// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestKMSCryptoKeyOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for KMS CryptoKey through parse -> normalize -> attribute extraction
// -> generation -> envelope, proving the redaction-safe typed-depth attributes,
// correlation anchors, and the KeyRing edge reach durable facts without any live
// GCP call, and that no key material ever lands on a fact.
func TestKMSCryptoKeyOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_kms_crypto_key.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(page.Resources))
	}

	gen := NewGeneration(attributesTestBoundary(), redact.Key{})
	if err := gen.AddPage(page.Resources); err != nil {
		t.Fatalf("add page: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("build generation: %v", err)
	}

	resourceCount := 0
	keyRingEdges := 0
	var symmetricKeyAttrs map[string]any
	var signKeyAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			switch env.Payload["full_resource_name"] {
			case kmsCryptoKeyFullResourceName:
				symmetricKeyAttrs, _ = env.Payload["attributes"].(map[string]any)
			case "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/sig-rk/cryptoKeys/sign-key":
				signKeyAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeCryptoKeyInKeyRing {
				keyRingEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if symmetricKeyAttrs == nil {
		t.Fatalf("key1 carried no attributes")
	}
	if symmetricKeyAttrs["purpose"] != "ENCRYPT_DECRYPT" {
		t.Errorf("key1 purpose = %v, want ENCRYPT_DECRYPT", symmetricKeyAttrs["purpose"])
	}
	if symmetricKeyAttrs["rotation_period"] != "7776000s" {
		t.Errorf("key1 rotation_period = %v, want 7776000s", symmetricKeyAttrs["rotation_period"])
	}
	if symmetricKeyAttrs["primary_state"] != "ENABLED" {
		t.Errorf("key1 primary_state = %v, want ENABLED", symmetricKeyAttrs["primary_state"])
	}
	if signKeyAttrs == nil {
		t.Fatalf("sign-key carried no attributes")
	}
	if signKeyAttrs["purpose"] != "ASYMMETRIC_SIGN" {
		t.Errorf("sign-key purpose = %v, want ASYMMETRIC_SIGN", signKeyAttrs["purpose"])
	}
	if _, ok := signKeyAttrs["rotation_period"]; ok {
		t.Errorf("sign-key must not report rotation_period: %#v", signKeyAttrs)
	}
	if keyRingEdges != 2 {
		t.Errorf("kms_crypto_key_in_key_ring edges = %d, want 2", keyRingEdges)
	}

	// No key material or certificate token may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"keyMaterial", "pemCrt", "BEGIN CERTIFICATE"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked key material token %q", token)
		}
	}
}
