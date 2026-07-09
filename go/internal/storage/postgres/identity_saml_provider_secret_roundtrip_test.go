// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
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
)

// testSAMLSigningMaterialPEM generates a fresh RSA key and self-signed
// certificate PEM pair, exactly as an admin's sp_private_key/sp_certificate
// write-only submission would look.
func testSAMLSigningMaterialPEM(t *testing.T) (privateKeyPEM string, certificatePEM string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "eshu-sp.example.test"},
		NotBefore:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 6, 22, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return string(keyPEM), string(certPEM)
}

// TestSealedSAMLProviderSecretRoundTripsThroughSamlauthOpen is the
// cross-package AAD-agreement proof this package's own doc comments promise:
// providerSecretAAD (Seal, here) and samlauth.ProviderSecretAAD (Open) "must
// construct this identically or decryption fails closed with ErrDecrypt" —
// this test exercises the REAL production Seal call
// ((*IdentitySubjectStore).sealProviderSecret, the same unexported method
// CreateProviderConfig and UpdateProviderConfig call for a real write) and
// hands its ciphertext to the REAL production Open call
// (samlauth.ResolveSealedProviderConfig), rather than each package only
// round-tripping against its own locally-reconstructed AAD string. If the two
// AAD builders ever drift, this fails closed here instead of only failing
// silently for every DB-backed SAML login in production.
func TestSealedSAMLProviderSecretRoundTripsThroughSamlauthOpen(t *testing.T) {
	t.Parallel()
	kr := testKeyring(t)
	store := NewIdentitySubjectStore(newProviderConfigFakeDB())
	store.SetProviderSecretKeyring(kr)

	const providerConfigID = "pc_saml_roundtrip_1"
	const revisionID = "rev_1"

	keyPEM, certPEM := testSAMLSigningMaterialPEM(t)
	secretJSON, err := json.Marshal(struct {
		SPPrivateKey  string `json:"sp_private_key"`
		SPCertificate string `json:"sp_certificate"`
	}{SPPrivateKey: keyPEM, SPCertificate: certPEM})
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}

	sealed, err := store.sealProviderSecret(providerConfigID, revisionID, string(secretJSON))
	if err != nil {
		t.Fatalf("sealProviderSecret() error = %v", err)
	}

	configJSON := `{"entity_id":"https://idp.example.test","metadata_xml":"<md/>","service_provider_entity_id":"https://eshu.example.test/api/v0/auth/saml/providers/pc_saml_roundtrip_1/metadata","service_provider_acs_url":"https://eshu.example.test/api/v0/auth/saml/providers/pc_saml_roundtrip_1/acs"}`

	provider, err := samlauth.ResolveSealedProviderConfig(kr, providerConfigID, revisionID, configJSON, sealed)
	if err != nil {
		t.Fatalf("samlauth.ResolveSealedProviderConfig() error = %v, want the storage-layer Seal and samlauth Open to agree on AAD", err)
	}
	if provider.ServiceProvider.Key == nil || provider.ServiceProvider.Certificate == nil {
		t.Fatal("resolved provider missing SP signing material")
	}
}
