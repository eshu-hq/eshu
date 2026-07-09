// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

func testSAMLDBProviderKeyring(t *testing.T) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 11)
	}
	kr, err := secretcrypto.NewKeyring("k1", map[secretcrypto.KeyID][]byte{"k1": key})
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

// testSPSigningMaterialPEM generates a fresh RSA key and self-signed
// certificate, PEM-encoded exactly as an admin would submit via
// sp_private_key/sp_certificate.
func testSPSigningMaterialPEM(t *testing.T) (privateKeyPEM string, certificatePEM string, key *rsa.PrivateKey, cert *x509.Certificate) {
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
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &rsaKey.PublicKey, rsaKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	parsedCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return string(keyPEM), string(certPEM), rsaKey, parsedCert
}

// TestResolveSealedProviderConfigDecryptsSPSigningMaterial proves
// ResolveSealedProviderConfig round-trips a sealed sp_private_key/
// sp_certificate correctly (the #4978 "open sealed key only in the authn
// path, then sign/validate" contract): the decrypted key and certificate are
// a genuine matching pair usable by crewjam's dsig signing context, not just
// parseable PEM.
func TestResolveSealedProviderConfigDecryptsSPSigningMaterial(t *testing.T) {
	t.Parallel()
	kr := testSAMLDBProviderKeyring(t)
	keyPEM, certPEM, wantKey, wantCert := testSPSigningMaterialPEM(t)
	secretJSON, err := json.Marshal(samlConnectionTestSecret{SPPrivateKey: keyPEM, SPCertificate: certPEM})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	sealed, err := kr.Seal(secretJSON, []byte(ProviderSecretAAD("pc_1", "rev_1")))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	configJSON := `{"entity_id":"https://idp.example.test","metadata_xml":"<md/>","group_attribute":"groups","service_provider_entity_id":"https://eshu.example.test/api/v0/auth/saml/providers/pc_1/metadata","service_provider_acs_url":"https://eshu.example.test/api/v0/auth/saml/providers/pc_1/acs"}`

	provider, err := ResolveSealedProviderConfig(kr, "pc_1", "rev_1", configJSON, sealed)
	if err != nil {
		t.Fatalf("ResolveSealedProviderConfig() error = %v", err)
	}
	if provider.ProviderConfigID != "pc_1" {
		t.Fatalf("ProviderConfigID = %q, want pc_1", provider.ProviderConfigID)
	}
	if provider.ServiceProvider.EntityID != "https://eshu.example.test/api/v0/auth/saml/providers/pc_1/metadata" {
		t.Fatalf("ServiceProvider.EntityID = %q, want the configured SP entity id", provider.ServiceProvider.EntityID)
	}
	if provider.ServiceProvider.ACSURL != "https://eshu.example.test/api/v0/auth/saml/providers/pc_1/acs" {
		t.Fatalf("ServiceProvider.ACSURL = %q, want the configured ACS url", provider.ServiceProvider.ACSURL)
	}
	if provider.ExpectedIdentityProviderEntityID != "https://idp.example.test" {
		t.Fatalf("ExpectedIdentityProviderEntityID = %q, want the configured idp entity id", provider.ExpectedIdentityProviderEntityID)
	}
	if len(provider.GroupMapping.GroupAttributeNames) != 1 || provider.GroupMapping.GroupAttributeNames[0] != "groups" {
		t.Fatalf("GroupMapping = %+v, want [groups]", provider.GroupMapping)
	}
	if provider.GroupMapping.HashScope != "pc_1" {
		t.Fatalf("GroupMapping.HashScope = %q, want pc_1 (default to provider_config_id)", provider.GroupMapping.HashScope)
	}

	// The decrypted key/certificate must be the SAME pair that was sealed,
	// not merely "parses as PEM" — prove it by comparing the certificate DER
	// and by proving the returned Key.Public() matches the certificate's
	// public key (a genuine sign/validate proof: an unrelated key parsed
	// successfully would still fail this).
	if provider.ServiceProvider.Certificate == nil || !bytes.Equal(provider.ServiceProvider.Certificate.Raw, wantCert.Raw) {
		t.Fatalf("Certificate did not round-trip to the sealed certificate")
	}
	signer := provider.ServiceProvider.Key
	if signer == nil {
		t.Fatal("Key = nil, want the decrypted SP private key")
	}
	gotPub, ok := signer.Public().(*rsa.PublicKey)
	if !ok {
		t.Fatalf("Key.Public() type = %T, want *rsa.PublicKey", signer.Public())
	}
	if gotPub.N.Cmp(wantKey.N) != 0 || gotPub.E != wantKey.E {
		t.Fatal("Key.Public() does not match the sealed private key's public component")
	}
	certPub, ok := provider.ServiceProvider.Certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("Certificate.PublicKey type = %T, want *rsa.PublicKey", provider.ServiceProvider.Certificate.PublicKey)
	}
	if gotPub.N.Cmp(certPub.N) != 0 || gotPub.E != certPub.E {
		t.Fatal("resolved Key does not match resolved Certificate's public key — not a usable signing pair")
	}
}

// TestResolveSealedProviderConfigFailsClosedOnAADMismatch proves that
// resolving with the wrong provider_config_id or revision_id (which changes
// the AAD) fails closed rather than returning a wrong or partial secret.
func TestResolveSealedProviderConfigFailsClosedOnAADMismatch(t *testing.T) {
	t.Parallel()
	kr := testSAMLDBProviderKeyring(t)
	keyPEM, certPEM, _, _ := testSPSigningMaterialPEM(t)
	secretJSON, err := json.Marshal(samlConnectionTestSecret{SPPrivateKey: keyPEM, SPCertificate: certPEM})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	sealed, err := kr.Seal(secretJSON, []byte(ProviderSecretAAD("pc_1", "rev_1")))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	configJSON := `{"entity_id":"https://idp.example.test","metadata_xml":"<md/>","service_provider_entity_id":"https://eshu.example.test/metadata","service_provider_acs_url":"https://eshu.example.test/acs"}`

	if _, err := ResolveSealedProviderConfig(kr, "pc_1", "rev_WRONG", configJSON, sealed); err == nil {
		t.Fatal("ResolveSealedProviderConfig() with wrong revision_id error = nil, want ErrDecrypt-wrapped failure")
	}
	if _, err := ResolveSealedProviderConfig(kr, "pc_OTHER", "rev_1", configJSON, sealed); err == nil {
		t.Fatal("ResolveSealedProviderConfig() with wrong provider_config_id error = nil, want ErrDecrypt-wrapped failure")
	}
}

// TestResolveSealedProviderConfigRequiresConfiguration proves missing
// entity_id/service_provider_entity_id/service_provider_acs_url/metadata_xml
// in the configuration JSON is rejected rather than silently building an
// unusable provider (mirrors oidclogin's equivalent redirect_url guard).
func TestResolveSealedProviderConfigRequiresConfiguration(t *testing.T) {
	t.Parallel()
	kr := testSAMLDBProviderKeyring(t)
	keyPEM, certPEM, _, _ := testSPSigningMaterialPEM(t)
	secretJSON, err := json.Marshal(samlConnectionTestSecret{SPPrivateKey: keyPEM, SPCertificate: certPEM})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	sealed, err := kr.Seal(secretJSON, []byte(ProviderSecretAAD("pc_1", "rev_1")))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	tests := []struct {
		name       string
		configJSON string
	}{
		{"missing entity_id", `{"metadata_xml":"<md/>","service_provider_entity_id":"https://eshu.example.test/metadata","service_provider_acs_url":"https://eshu.example.test/acs"}`},
		{"missing service_provider_entity_id", `{"entity_id":"https://idp.example.test","metadata_xml":"<md/>","service_provider_acs_url":"https://eshu.example.test/acs"}`},
		{"missing service_provider_acs_url", `{"entity_id":"https://idp.example.test","metadata_xml":"<md/>","service_provider_entity_id":"https://eshu.example.test/metadata"}`},
		{"missing metadata_xml (metadata_url only)", `{"entity_id":"https://idp.example.test","metadata_url":"https://idp.example.test/metadata","service_provider_entity_id":"https://eshu.example.test/metadata","service_provider_acs_url":"https://eshu.example.test/acs"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ResolveSealedProviderConfig(kr, "pc_1", "rev_1", tc.configJSON, sealed); err == nil {
				t.Fatalf("ResolveSealedProviderConfig() error = nil, want error for %s", tc.name)
			}
		})
	}
}

// TestParseSPSigningMaterialRejectsInvalidPEM proves malformed key/certificate
// PEM fails closed rather than returning a partial or zero-value result.
func TestParseSPSigningMaterialRejectsInvalidPEM(t *testing.T) {
	t.Parallel()
	_, certPEM, _, _ := testSPSigningMaterialPEM(t)

	if _, _, err := ParseSPSigningMaterial("not pem", certPEM); err == nil {
		t.Fatal("ParseSPSigningMaterial() with invalid key PEM error = nil, want error")
	}
	if !strings.Contains(errString(t, "not pem", certPEM), "sp_private_key") {
		t.Fatal("error should identify the private key as the invalid field")
	}
}

func errString(t *testing.T, keyPEM, certPEM string) string {
	t.Helper()
	_, _, err := ParseSPSigningMaterial(keyPEM, certPEM)
	if err == nil {
		return ""
	}
	return err.Error()
}
