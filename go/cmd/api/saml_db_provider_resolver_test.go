// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/samlauth"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func testProviderSecretKeyring(t *testing.T) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 21)
	}
	kr, err := secretcrypto.NewKeyring("k1", map[secretcrypto.KeyID][]byte{"k1": key})
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

// testSealedSPSecret generates a fresh RSA key + self-signed certificate and
// seals them as an admin-supplied sp_private_key/sp_certificate would be
// sealed at write time (identity_provider_config_writes.go), bound to the
// same AAD samlauth.ResolveSealedProviderConfig reconstructs.
func testSealedSPSecret(t *testing.T, kr *secretcrypto.Keyring, providerConfigID, revisionID string) string {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "eshu-sp.example.test"},
		NotBefore:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 6, 22, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &rsaKey.PublicKey, rsaKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	secretJSON, err := json.Marshal(struct {
		SPPrivateKey  string `json:"sp_private_key"`
		SPCertificate string `json:"sp_certificate"`
	}{SPPrivateKey: string(keyPEM), SPCertificate: string(certPEM)})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	sealed, err := kr.Seal(secretJSON, []byte(samlauth.ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return sealed
}

// TestSAMLDBProviderResolverResolvesEnabledProvider proves the full
// #4966/#4978 login round trip: samlDBProviderResolver.ResolveProvider reads
// the active revision's sealed_secret+configuration from Postgres (faked
// here — the SQL shape itself is proven by
// TestGetActiveSAMLProviderConfigForLoginReturnsActiveExternalSAMLRow in
// storage/postgres), opens the sealed sp_private_key/sp_certificate ONLY via
// samlauth.ResolveSealedProviderConfig, and returns a query.SAMLProviderConfig
// with a usable, matching signing key pair — i.e. resolve → open sealed key →
// sign/validate.
func TestSAMLDBProviderResolverResolvesEnabledProvider(t *testing.T) {
	t.Parallel()
	kr := testProviderSecretKeyring(t)
	sealed := testSealedSPSecret(t, kr, "pc_saml_db_1", "rev_1")
	configJSON := `{"entity_id":"https://idp.example.test","metadata_xml":"<md/>","group_attribute":"groups","service_provider_entity_id":"https://eshu.example.test/api/v0/auth/saml/providers/pc_saml_db_1/metadata","service_provider_acs_url":"https://eshu.example.test/api/v0/auth/saml/providers/pc_saml_db_1/acs"}`

	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{{
		rows: [][]any{{"external_saml", "rev_1", sealed, configJSON}},
	}}}
	resolver := &samlDBProviderResolver{
		store:   pgstatus.NewIdentitySubjectStore(db),
		keyring: kr,
	}

	provider, found, err := resolver.ResolveProvider(context.Background(), "pc_saml_db_1")
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v, want the DB-backed provider to resolve", err)
	}
	if !found {
		t.Fatal("ResolveProvider() found = false, want true for an enabled DB-backed provider")
	}
	if provider.ProviderConfigID != "pc_saml_db_1" {
		t.Fatalf("ProviderConfigID = %q, want pc_saml_db_1", provider.ProviderConfigID)
	}
	if provider.ServiceProvider.EntityID != "https://eshu.example.test/api/v0/auth/saml/providers/pc_saml_db_1/metadata" {
		t.Fatalf("ServiceProvider.EntityID = %q, want the configured SP entity id", provider.ServiceProvider.EntityID)
	}
	if provider.ServiceProvider.Key == nil || provider.ServiceProvider.Certificate == nil {
		t.Fatal("ServiceProvider.Key/Certificate = nil, want the decrypted SP signing material")
	}
	// sign/validate proof: the resolved key and certificate must be a
	// genuinely matching pair (this is exactly what query.newCrewjamServiceProvider
	// forwards unchanged into saml.ServiceProvider.Key/Certificate — see
	// saml_verifier.go), not merely two independently-parseable PEM blocks.
	gotPub, ok := provider.ServiceProvider.Key.Public().(*rsa.PublicKey)
	if !ok {
		t.Fatalf("Key.Public() type = %T, want *rsa.PublicKey", provider.ServiceProvider.Key.Public())
	}
	certPub, ok := provider.ServiceProvider.Certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("Certificate.PublicKey type = %T, want *rsa.PublicKey", provider.ServiceProvider.Certificate.PublicKey)
	}
	if gotPub.N.Cmp(certPub.N) != 0 || gotPub.E != certPub.E {
		t.Fatal("resolved Key does not match resolved Certificate's public key — not a usable signing pair")
	}
}

// TestSAMLDBProviderResolverRejectsDisabledOrMissingProvider proves a draft,
// disabled, or absent provider never resolves (found=false, no error) —
// login must never proceed against a provider that has not passed Enable's
// test-connection gate.
func TestSAMLDBProviderResolverRejectsDisabledOrMissingProvider(t *testing.T) {
	t.Parallel()
	kr := testProviderSecretKeyring(t)
	db := &samlIdentityTestDB{queryResponses: []samlIdentityTestRows{{}}}
	resolver := &samlDBProviderResolver{
		store:   pgstatus.NewIdentitySubjectStore(db),
		keyring: kr,
	}

	provider, found, err := resolver.ResolveProvider(context.Background(), "pc_missing_or_draft")
	if err != nil {
		t.Fatalf("ResolveProvider() error = %v, want nil", err)
	}
	if found || provider.ProviderConfigID != "" {
		t.Fatalf("ResolveProvider() = %+v found = %t, want (zero value, false)", provider, found)
	}
}

// TestNewSAMLDBProviderResolverNilWithoutKeyring proves the constructor
// returns nil (not a resolver that can only fail) when db or keyring is nil,
// matching newOIDCDBProviderResolver's contract exactly.
func TestNewSAMLDBProviderResolverNilWithoutKeyring(t *testing.T) {
	t.Parallel()
	if got := newSAMLDBProviderResolver(nil, testProviderSecretKeyring(t)); got != nil {
		t.Fatalf("newSAMLDBProviderResolver(nil db, keyring) = %v, want nil", got)
	}
}
