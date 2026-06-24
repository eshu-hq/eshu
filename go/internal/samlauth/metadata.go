// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
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
