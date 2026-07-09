// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestProviderConfigReadSurfacesNeverLeakSecretCanary seals the shared
// hosted-governance redaction registry's SensitiveSecretValue canary
// (redact.HostedGovernanceRegistry(), "correct-horse-redaction-canary") as a
// provider-config client secret, then exercises every read surface this
// package exposes — GetProviderConfigDetail, ListProviderConfigs,
// ListProviderConfigRevisions — and asserts the canary is absent from their
// JSON-encoded output using the same AssertNoForbiddenCanary mechanism the
// hosted-governance negative-leakage proof uses for every other surface. It
// deliberately does NOT check GetProviderConfigConnectionTestMaterial: that
// method's ciphertext-only contract is intentionally excluded from this
// surface set — see its doc comment for why it exists and who may call it.
func TestProviderConfigReadSurfacesNeverLeakSecretCanary(t *testing.T) {
	t.Parallel()

	registry := redact.HostedGovernanceRegistry()
	var canary string
	for _, sample := range registry.Canaries() {
		if sample.Class == redact.SensitiveSecretValue {
			canary = sample.Raw
			break
		}
	}
	if canary == "" {
		t.Fatal("hosted governance registry has no SensitiveSecretValue canary")
	}

	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID:  "pc_canary",
		TenantID:          "tenant_a",
		ProviderKind:      "external_oidc",
		ProviderKeyHash:   "hash_canary",
		RevisionID:        "rev_1",
		Configuration:     `{"issuer":"https://idp.example.test","client_id":"client-1"}`,
		ConfigurationHash: "cfg_hash_canary",
		PlaintextSecret:   `{"client_secret":"` + canary + `"}`,
		Now:               time.Now(),
	}); err != nil {
		t.Fatalf("CreateProviderConfig() error = %v", err)
	}

	assertNoCanary := func(t *testing.T, label string, v any) {
		t.Helper()
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: json.Marshal: %v", label, err)
		}
		if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, body); err != nil {
			t.Fatalf("%s leaked the secret canary: %v\nbody: %s", label, err, body)
		}
		if bytes.Contains(body, []byte(canary)) {
			t.Fatalf("%s body contains the raw canary directly: %s", label, body)
		}
	}

	detail, found, err := store.GetProviderConfigDetail(ctx, "pc_canary", "tenant_a")
	if err != nil || !found {
		t.Fatalf("GetProviderConfigDetail() = %+v, %v, %v", detail, found, err)
	}
	assertNoCanary(t, "GetProviderConfigDetail", detail)

	list, err := store.ListProviderConfigs(ctx, "tenant_a")
	if err != nil {
		t.Fatalf("ListProviderConfigs() error = %v", err)
	}
	assertNoCanary(t, "ListProviderConfigs", list)

	revisions, err := store.ListProviderConfigRevisions(ctx, "pc_canary", "tenant_a")
	if err != nil {
		t.Fatalf("ListProviderConfigRevisions() error = %v", err)
	}
	assertNoCanary(t, "ListProviderConfigRevisions", revisions)
}

// TestGetProviderConfigConnectionTestMaterialCiphertextIsNotPlaintext proves
// the connection-test-only material's SealedSecret field carries the ESK1
// ciphertext, never the plaintext canary — it is a distinct, narrowly scoped
// method (see its doc comment) intentionally excluded from the negative
// -leakage read-surface set above because its very purpose is to return
// ciphertext to the test-connection orchestration path.
func TestGetProviderConfigConnectionTestMaterialCiphertextIsNotPlaintext(t *testing.T) {
	t.Parallel()
	db := newProviderConfigFakeDB()
	store := NewIdentitySubjectStore(db)
	store.SetProviderSecretKeyring(testKeyring(t))
	ctx := context.Background()

	const plaintext = "correct-horse-redaction-canary"
	if _, err := store.CreateProviderConfig(ctx, ProviderConfigCreate{
		ProviderConfigID: "pc_material", TenantID: "tenant_a", ProviderKind: "external_oidc",
		ProviderKeyHash: "hash_material", RevisionID: "rev_1", ConfigurationHash: "h",
		Configuration:   `{"issuer":"https://idp.example.test"}`,
		PlaintextSecret: `{"client_secret":"` + plaintext + `"}`, Now: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	material, found, err := store.GetProviderConfigConnectionTestMaterial(ctx, "pc_material", "tenant_a")
	if err != nil || !found {
		t.Fatalf("GetProviderConfigConnectionTestMaterial() = %+v, %v, %v", material, found, err)
	}
	if bytes.Contains([]byte(material.SealedSecret), []byte(plaintext)) {
		t.Fatalf("connection test material carries plaintext: %q", material.SealedSecret)
	}

	// Round-trip proof: the ciphertext this method returns is exactly what
	// Open (the login/authn boundary, exercised here only to prove the
	// material is genuinely openable — this test file lives in the storage
	// package, not login/authn, and this is the one narrow exception to
	// prove the AAD/ciphertext pairing is correct end to end) recovers.
	kr := testKeyring(t)
	aad := providerSecretAAD("pc_material", material.RevisionID)
	opened, err := kr.Open(material.SealedSecret, []byte(aad))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if string(opened) != `{"client_secret":"`+plaintext+`"}` {
		t.Fatalf("Open() = %q, want the original secret JSON", opened)
	}
}
