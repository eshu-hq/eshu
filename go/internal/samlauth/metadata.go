// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// MetadataValidationInput describes the IdP metadata a provider config expects.
type MetadataValidationInput struct {
	MetadataXML      []byte
	ExpectedEntityID string
	Now              time.Time
}

// IdentityProviderMetadata is the validation-safe view of trusted IdP metadata.
type IdentityProviderMetadata struct {
	EntityID                string
	SingleSignOnURL         string
	SigningCertificateCount int
	ValidUntil              time.Time
}

// ServiceProviderConfig describes the public SP endpoints Eshu advertises.
type ServiceProviderConfig struct {
	EntityID string
	ACSURL   string

	// Key and Certificate are the SP's own signing/decryption key pair,
	// decrypted from a DB-backed provider config's sealed sp_private_key /
	// sp_certificate (#4966, epic #4962; completes #4978) by
	// ResolveSealedProviderConfig — the only (*secretcrypto.Keyring).Open
	// call site for this material. Always nil for an env/file-registered
	// provider (samlProviderEnvConfig carries no key material at all).
	//
	// crewjam's ServiceProvider only consults these for decrypting an
	// EncryptedAssertion element (service_provider.go's
	// parseEncryptedAssertion) — Eshu's SP-initiated AuthnRequest uses
	// HTTP-Redirect binding, which crewjam never signs regardless of these
	// fields (see its MakeAuthenticationRequest doc comment: "We don't need
	// to sign the XML document if the IDP uses HTTP-Redirect binding").
	// Wiring them is still the correct login-runtime completion: an IdP
	// configured to encrypt assertions to this SP cannot be decrypted
	// without them, and every #4966 DB-backed SAML provider retains its own
	// sp_private_key/sp_certificate specifically so this is possible.
	Key         crypto.Signer
	Certificate *x509.Certificate
}

// ValidateIdentityProviderMetadata parses IdP metadata and enforces the Eshu
// provider config expectations needed before a SAML login may proceed.
func ValidateIdentityProviderMetadata(input MetadataValidationInput) (IdentityProviderMetadata, error) {
	metadata, err := samlsp.ParseMetadata(input.MetadataXML)
	if err != nil {
		return IdentityProviderMetadata{}, fmt.Errorf("%w: %v", ErrMetadataInvalid, err)
	}
	expected := strings.TrimSpace(input.ExpectedEntityID)
	if expected == "" || strings.TrimSpace(metadata.EntityID) != expected {
		return IdentityProviderMetadata{}, ErrMetadataIssuerMismatch
	}
	if !metadata.ValidUntil.IsZero() {
		now := input.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if !now.Before(metadata.ValidUntil) {
			return IdentityProviderMetadata{}, ErrMetadataExpired
		}
	}

	ssoURL := firstUsableSSOURL(metadata)
	if ssoURL == "" {
		return IdentityProviderMetadata{}, ErrMetadataMissingSSO
	}
	certCount, err := signingCertificateCount(metadata)
	if err != nil {
		return IdentityProviderMetadata{}, err
	}
	if certCount == 0 {
		return IdentityProviderMetadata{}, ErrMetadataMissingSigningCertificate
	}

	return IdentityProviderMetadata{
		EntityID:                metadata.EntityID,
		SingleSignOnURL:         ssoURL,
		SigningCertificateCount: certCount,
		ValidUntil:              metadata.ValidUntil,
	}, nil
}

// RenderServiceProviderMetadata returns SP metadata for Eshu's SAML endpoints.
func RenderServiceProviderMetadata(config ServiceProviderConfig) (string, error) {
	entityID := strings.TrimSpace(config.EntityID)
	acsURL := strings.TrimSpace(config.ACSURL)
	if entityID == "" || acsURL == "" {
		return "", fmt.Errorf("%w: entityID and ACS URL are required", ErrMetadataInvalid)
	}
	metadataURL, err := url.Parse(entityID)
	if err != nil {
		return "", fmt.Errorf("%w: invalid entityID: %v", ErrMetadataInvalid, err)
	}
	parsedACSURL, err := url.Parse(acsURL)
	if err != nil {
		return "", fmt.Errorf("%w: invalid ACS URL: %v", ErrMetadataInvalid, err)
	}
	sp := saml.ServiceProvider{
		EntityID:          entityID,
		MetadataURL:       *metadataURL,
		AcsURL:            *parsedACSURL,
		AuthnNameIDFormat: saml.PersistentNameIDFormat,
	}
	out, err := xml.Marshal(sp.Metadata())
	if err != nil {
		return "", fmt.Errorf("%w: marshal SP metadata: %v", ErrMetadataInvalid, err)
	}
	return string(out), nil
}

func firstUsableSSOURL(metadata *saml.EntityDescriptor) string {
	for _, descriptor := range metadata.IDPSSODescriptors {
		for _, endpoint := range descriptor.SingleSignOnServices {
			if endpoint.Location == "" {
				continue
			}
			switch endpoint.Binding {
			case saml.HTTPRedirectBinding, saml.HTTPPostBinding:
				return endpoint.Location
			}
		}
	}
	return ""
}

func signingCertificateCount(metadata *saml.EntityDescriptor) (int, error) {
	count := 0
	for _, descriptor := range metadata.IDPSSODescriptors {
		for _, keyDescriptor := range descriptor.KeyDescriptors {
			if keyDescriptor.Use != "" && keyDescriptor.Use != "signing" {
				continue
			}
			for _, cert := range keyDescriptor.KeyInfo.X509Data.X509Certificates {
				raw := strings.TrimSpace(cert.Data)
				if raw == "" {
					continue
				}
				der, err := base64.StdEncoding.DecodeString(raw)
				if err != nil {
					return 0, fmt.Errorf("%w: decode signing certificate", ErrMetadataInvalid)
				}
				if _, err := x509.ParseCertificate(der); err != nil {
					return 0, fmt.Errorf("%w: parse signing certificate", ErrMetadataInvalid)
				}
				count++
			}
		}
	}
	return count, nil
}
