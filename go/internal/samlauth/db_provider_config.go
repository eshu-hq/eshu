// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// dbSAMLConfiguration mirrors the query package's private
// samlConfigurationFields shape, plus the SP endpoints a DB-backed provider
// config now also carries (service_provider_entity_id /
// service_provider_acs_url, #4966 follow-up #4978). This package cannot
// import go/internal/query for it without an import cycle (query already
// imports samlauth for its login-time types), so the shape is duplicated
// here; both sides are documented to keep it in sync — same rationale as
// oidclogin's dbProviderConfiguration.
type dbSAMLConfiguration struct {
	MetadataURL             string `json:"metadata_url,omitempty"`
	MetadataXML             string `json:"metadata_xml,omitempty"`
	EntityID                string `json:"entity_id"`
	GroupAttribute          string `json:"group_attribute,omitempty"`
	ServiceProviderEntityID string `json:"service_provider_entity_id,omitempty"`
	ServiceProviderACSURL   string `json:"service_provider_acs_url,omitempty"`
}

// DBSAMLProvider is a DB-backed SAML provider config resolved for the login
// runtime (#4966, epic #4962; completes #4978), field-for-field the same
// shape as query.SAMLProviderConfig — the caller (cmd/api's
// samlDBProviderResolver, the only consumer) converts it 1:1. Defined in this
// package instead of returning query.SAMLProviderConfig directly because
// query already imports samlauth (query -> samlauth); samlauth returning a
// query type would create an import cycle.
type DBSAMLProvider struct {
	ProviderConfigID                 string
	ServiceProvider                  ServiceProviderConfig
	IdentityProviderMetadataXML      []byte
	ExpectedIdentityProviderEntityID string
	GroupMapping                     ClaimMapping
	ClockSkew                        time.Duration
}

// ResolveSealedProviderConfig decrypts a DB-backed SAML provider's sealed SP
// private key / certificate and combines it with its non-secret
// configuration to build a usable DBSAMLProvider for the login runtime
// (#4966, epic #4962; completes #4978).
//
// This is one of exactly two (*secretcrypto.Keyring).Open call sites in this
// codebase for provider-config secrets — the other is TestConnection
// (provider_connection_test_probe.go), used by the admin test-connection
// endpoint. Both are confined to this package per the epic #4962 boundary
// (go/internal/query never imports secretcrypto — see
// secretcrypto_open_boundary_test.go). The decrypted key/certificate are held
// only in the returned DBSAMLProvider for the duration of building one
// crewjam saml.ServiceProvider (query.newCrewjamServiceProvider); they are
// never logged, returned outside the caller, or persisted.
//
// providerConfigID and revisionID must be exactly the values the caller read
// the sealed_secret envelope for — they reconstruct the AAD Seal bound the
// envelope to (see postgres.providerSecretAAD /
// identity_provider_config_writes.go); a mismatch fails closed with
// secretcrypto.ErrDecrypt.
//
// ResolveSealedProviderConfig requires configurationJSON to carry
// service_provider_entity_id, service_provider_acs_url, and metadata_xml.
// These are optional at provider-config write/test-connection time (an admin
// may create or test-connect a provider before supplying them) — the same
// optional-at-write, required-at-login-resolution treatment
// oidclogin.ResolveSealedProviderConfig gives redirect_url. A provider
// missing any of them fails closed here rather than silently building an
// unusable or partially-configured login target. metadata_url-only providers
// (no inline metadata_xml) are rejected: the login runtime resolves
// synchronously with no fetch step, matching the fact that env-config SAML
// has never supported a metadata_url either (only inline XML via
// samlProviderEnvConfig.IdentityProviderMetadataXMLEnv).
func ResolveSealedProviderConfig(
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID string,
	configurationJSON, sealedSecret string,
) (DBSAMLProvider, error) {
	if keyring == nil {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: resolve db provider requires a configured keyring")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	revisionID = strings.TrimSpace(revisionID)
	if providerConfigID == "" || revisionID == "" {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: resolve db provider requires provider_config_id and revision_id")
	}

	var cfg dbSAMLConfiguration
	if err := json.Unmarshal([]byte(configurationJSON), &cfg); err != nil {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: decode db provider configuration: %w", err)
	}
	cfg.MetadataXML = strings.TrimSpace(cfg.MetadataXML)
	cfg.EntityID = strings.TrimSpace(cfg.EntityID)
	cfg.GroupAttribute = strings.TrimSpace(cfg.GroupAttribute)
	cfg.ServiceProviderEntityID = strings.TrimSpace(cfg.ServiceProviderEntityID)
	cfg.ServiceProviderACSURL = strings.TrimSpace(cfg.ServiceProviderACSURL)
	if cfg.EntityID == "" || cfg.ServiceProviderEntityID == "" || cfg.ServiceProviderACSURL == "" {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: db provider configuration is missing entity_id, service_provider_entity_id, or service_provider_acs_url")
	}
	if cfg.MetadataXML == "" {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: db provider configuration is missing metadata_xml; metadata_url-only providers cannot resolve for login")
	}

	plaintext, err := keyring.Open(sealedSecret, []byte(ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: open db provider secret: %w", err)
	}
	var secret samlConnectionTestSecret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: decode db provider secret: %w", err)
	}
	key, cert, err := ParseSPSigningMaterial(secret.SPPrivateKey, secret.SPCertificate)
	if err != nil {
		return DBSAMLProvider{}, fmt.Errorf("samlauth: parse db provider sp signing material: %w", err)
	}

	var groupAttributes []string
	if cfg.GroupAttribute != "" {
		groupAttributes = []string{cfg.GroupAttribute}
	}

	return DBSAMLProvider{
		ProviderConfigID: providerConfigID,
		ServiceProvider: ServiceProviderConfig{
			EntityID:    cfg.ServiceProviderEntityID,
			ACSURL:      cfg.ServiceProviderACSURL,
			Key:         key,
			Certificate: cert,
		},
		IdentityProviderMetadataXML:      []byte(cfg.MetadataXML),
		ExpectedIdentityProviderEntityID: cfg.EntityID,
		GroupMapping: ClaimMapping{
			GroupAttributeNames: groupAttributes,
			RequireGroups:       false,
			HashScope:           providerConfigID,
		},
	}, nil
}

// ParseSPSigningMaterial parses a decrypted SP private key and certificate
// (PEM-encoded) into the crypto.Signer/*x509.Certificate pair crewjam's
// saml.ServiceProvider needs. Supports PKCS#1, PKCS#8, and SEC1/EC private
// keys — the same formats TestConnection's parseableSPSigningMaterial accepts
// as "valid PEM" without fully parsing the key; this function is the one call
// site in this package that actually builds a usable crypto.Signer from them.
// Returns an error rather than a partial result on any parse failure.
func ParseSPSigningMaterial(privateKeyPEM, certificatePEM string) (crypto.Signer, *x509.Certificate, error) {
	keyBlock, _ := pem.Decode([]byte(privateKeyPEM))
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("sp_private_key is not valid PEM")
	}
	key, err := parseSPPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("sp_private_key: %w", err)
	}
	certBlock, _ := pem.Decode([]byte(certificatePEM))
	if certBlock == nil {
		return nil, nil, fmt.Errorf("sp_certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("sp_certificate: %w", err)
	}
	return key, cert, nil
}

// parseSPPrivateKey tries every private-key DER encoding crewjam/dsig can
// consume as a crypto.Signer: PKCS#1 (RSA), SEC1 (EC), then PKCS#8 (either).
func parseSPPrivateKey(der []byte) (crypto.Signer, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("unsupported or malformed private key: %w", err)
	}
	switch key := parsed.(type) {
	case *rsa.PrivateKey:
		return key, nil
	case *ecdsa.PrivateKey:
		return key, nil
	case crypto.Signer:
		return key, nil
	default:
		return nil, fmt.Errorf("unsupported private key type %T", parsed)
	}
}
