// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// ProviderSecretAAD builds the AAD text for a provider-config secret
// envelope, matching the epic #4962 shared crypto contract exactly:
// "eshu:provider-secret:v1|<provider_config_id>|<revision_id>". Both the
// storage-layer Seal call (postgres.IdentitySubjectStore, in
// identity_provider_config_writes.go) and this Open call must build this
// identically, or decryption fails closed with secretcrypto.ErrDecrypt.
func ProviderSecretAAD(providerConfigID, revisionID string) string {
	return "eshu:provider-secret:v1|" + providerConfigID + "|" + revisionID
}

// samlConnectionTestSecret mirrors the query package's private
// samlSecretFields shape. Duplicated to avoid an import cycle — see
// oidclogin's oidcConnectionTestSecret doc comment for the same rationale.
type samlConnectionTestSecret struct {
	SPPrivateKey  string `json:"sp_private_key"` // #nosec G101 -- JSON field name, not a credential
	SPCertificate string `json:"sp_certificate"`
}

// ConnectionTestResult reports a bounded, safe test-connection outcome.
// Detail never carries a secret or plaintext credential.
type ConnectionTestResult struct {
	OK     bool
	Detail string
}

// metadataFetcher abstracts fetching IdP metadata XML from a URL, so
// TestConnection is testable without a live network call.
type metadataFetcher func(ctx context.Context, url string) ([]byte, error)

// TestConnection validates SAML IdP metadata and the decrypted SP signing
// material's basic shape.
//
// This is the ONLY (*secretcrypto.Keyring).Open call site in this package: it
// decrypts sealedSecret transiently, in-process, uses it only to confirm the
// SP private key and certificate parse as valid PEM/X.509 material, and
// discards it immediately — never logged, returned, or serialized. This
// satisfies the epic #4962 boundary that Open is confined to login/authn
// packages.
//
// Explicit scope note (see #4966 executor report): this does NOT simulate an
// actual signed assertion exchange with a live IdP (SAML has no equivalent of
// OIDC's discovery document — there is no automatable "does the IdP accept
// this SP" check without a real browser-mediated SSO round trip). What it
// proves: (1) the IdP metadata (fetched from metadataURL, or the stored
// metadataXML) parses via the same ValidateIdentityProviderMetadata this
// package's login path uses, and (2) the stored SP private key and
// certificate decrypt to valid PEM/X.509 material.
func TestConnection(
	ctx context.Context,
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, entityID, metadataURL, metadataXML, sealedSecret string,
) (ConnectionTestResult, error) {
	return testConnection(ctx, keyring, providerConfigID, revisionID, entityID, metadataURL, metadataXML, sealedSecret, nil)
}

func testConnection(
	ctx context.Context,
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, entityID, metadataURL, metadataXML, sealedSecret string,
	fetch metadataFetcher,
) (ConnectionTestResult, error) {
	if keyring == nil {
		return ConnectionTestResult{}, fmt.Errorf("samlauth: connection test requires a configured keyring")
	}

	xmlBytes := []byte(strings.TrimSpace(metadataXML))
	if len(xmlBytes) == 0 {
		if strings.TrimSpace(metadataURL) == "" {
			return ConnectionTestResult{OK: false, Detail: "metadata_url or metadata_xml is required"}, nil
		}
		if fetch == nil {
			fetch = httpFetchMetadata
		}
		fetched, err := fetch(ctx, metadataURL)
		if err != nil {
			return ConnectionTestResult{OK: false, Detail: "metadata_url fetch failed"}, nil
		}
		xmlBytes = fetched
	}
	if _, err := ValidateIdentityProviderMetadata(MetadataValidationInput{
		MetadataXML:      xmlBytes,
		ExpectedEntityID: entityID,
		Now:              time.Now().UTC(),
	}); err != nil {
		return ConnectionTestResult{OK: false, Detail: "idp metadata validation failed"}, nil
	}

	plaintext, err := keyring.Open(sealedSecret, []byte(ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		return ConnectionTestResult{OK: false, Detail: "stored secret failed to decrypt"}, nil
	}
	var secret samlConnectionTestSecret
	if err := json.Unmarshal(plaintext, &secret); err != nil {
		return ConnectionTestResult{OK: false, Detail: "stored secret is not a valid sp signing material payload"}, nil
	}
	if ok, detail := parseableSPSigningMaterial(secret); !ok {
		return ConnectionTestResult{OK: false, Detail: detail}, nil
	}

	return ConnectionTestResult{OK: true, Detail: "idp metadata valid; sp signing material parses"}, nil
}

// parseableSPSigningMaterial reports whether the SP private key and
// certificate are well-formed PEM/X.509 material, without ever returning the
// key or certificate bytes themselves.
func parseableSPSigningMaterial(secret samlConnectionTestSecret) (bool, string) {
	keyBlock, _ := pem.Decode([]byte(secret.SPPrivateKey))
	if keyBlock == nil {
		return false, "sp_private_key is not valid PEM"
	}
	certBlock, _ := pem.Decode([]byte(secret.SPCertificate))
	if certBlock == nil {
		return false, "sp_certificate is not valid PEM"
	}
	if _, err := x509.ParseCertificate(certBlock.Bytes); err != nil {
		return false, "sp_certificate is not a valid X.509 certificate"
	}
	return true, ""
}

func httpFetchMetadata(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("samlauth: metadata fetch status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5 MiB cap
}
