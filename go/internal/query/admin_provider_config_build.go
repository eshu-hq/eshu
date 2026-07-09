// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// builtProviderConfigWrite is the normalized, validated form of an admin
// provider-config write request: a non-secret Configuration JSON blob, a
// secret JSON blob (sealed by the store, never logged here), and the
// dedup/correlation hashes CreateProviderConfig needs. secretJSON and every
// field it was built from are held only in local variables for the duration
// of one request; nothing here persists them.
type builtProviderConfigWrite struct {
	kind              string // "external_oidc" | "external_saml"
	keyHash           string
	issuerHash        string
	clientIDHash      string
	metadataURLHash   string
	entityIDHash      string
	configurationJSON string
	secretJSON        string
	metadataForHash   string
}

// oidcConfigurationFields is the non-secret OIDC configuration JSON shape.
type oidcConfigurationFields struct {
	Issuer      string   `json:"issuer"`
	ClientID    string   `json:"client_id"`
	Scopes      []string `json:"scopes,omitempty"`
	GroupClaim  string   `json:"group_claim,omitempty"`
	RedirectURL string   `json:"redirect_url,omitempty"`
}

// oidcSecretFields is the OIDC secret JSON shape sealed by the store.
type oidcSecretFields struct {
	ClientSecret string `json:"client_secret"` // #nosec G101 -- JSON field name, not a credential
}

// samlConfigurationFields is the non-secret SAML configuration JSON shape.
// ServiceProviderEntityID/ServiceProviderACSURL are Eshu's own SP endpoints
// for this provider (#4966 follow-up #4978) — mirrored by samlauth's
// dbSAMLConfiguration, which decodes this same JSON shape at login-resolution
// time; both sides are documented to keep them in sync.
type samlConfigurationFields struct {
	MetadataURL             string `json:"metadata_url,omitempty"`
	MetadataXML             string `json:"metadata_xml,omitempty"`
	EntityID                string `json:"entity_id"`
	GroupAttribute          string `json:"group_attribute,omitempty"`
	ServiceProviderEntityID string `json:"service_provider_entity_id,omitempty"`
	ServiceProviderACSURL   string `json:"service_provider_acs_url,omitempty"`
}

// samlSecretFields is the SAML secret JSON shape sealed by the store.
type samlSecretFields struct {
	SPPrivateKey  string `json:"sp_private_key"` // #nosec G101 -- JSON field name, not a credential
	SPCertificate string `json:"sp_certificate"`
}

// buildProviderConfigWrite validates an admin write request and assembles the
// configuration/secret JSON and hashes CreateProviderConfig/UpdateProviderConfig
// need. It returns a plain error (not a sentinel) for every validation
// failure — the caller maps any error from this function to 400.
func buildProviderConfigWrite(body adminProviderConfigWriteRequest) (builtProviderConfigWrite, error) {
	switch strings.TrimSpace(strings.ToLower(body.ProviderKind)) {
	case "oidc":
		return buildOIDCProviderConfigWrite(body)
	case "saml":
		return buildSAMLProviderConfigWrite(body)
	default:
		return builtProviderConfigWrite{}, fmt.Errorf("provider_kind must be %q or %q", "oidc", "saml")
	}
}

func buildOIDCProviderConfigWrite(body adminProviderConfigWriteRequest) (builtProviderConfigWrite, error) {
	issuer := strings.TrimSpace(body.Issuer)
	clientID := strings.TrimSpace(body.ClientID)
	clientSecret := body.ClientSecret
	if issuer == "" || clientID == "" {
		return builtProviderConfigWrite{}, fmt.Errorf("issuer and client_id are required for an oidc provider config")
	}
	if strings.TrimSpace(clientSecret) == "" {
		return builtProviderConfigWrite{}, fmt.Errorf("client_secret is required: write-only secrets must be resupplied on every create or update")
	}

	scopes := append([]string(nil), body.Scopes...)
	sort.Strings(scopes) // deterministic configuration_hash regardless of request field order
	configJSON, err := json.Marshal(oidcConfigurationFields{
		Issuer:      issuer,
		ClientID:    clientID,
		Scopes:      scopes,
		GroupClaim:  strings.TrimSpace(body.GroupClaim),
		RedirectURL: strings.TrimSpace(body.RedirectURL),
	})
	if err != nil {
		return builtProviderConfigWrite{}, fmt.Errorf("encode oidc configuration: %w", err)
	}
	secretJSON, err := json.Marshal(oidcSecretFields{ClientSecret: clientSecret}) // #nosec G117 -- write-only secret payload marshaled solely to be sealed by secretcrypto.Seal; never emitted to any read surface.
	if err != nil {
		return builtProviderConfigWrite{}, fmt.Errorf("encode oidc secret: %w", err)
	}

	return builtProviderConfigWrite{
		kind:              "external_oidc",
		keyHash:           localIdentityHash(issuer + "|" + clientID),
		issuerHash:        localIdentityHash(issuer),
		clientIDHash:      localIdentityHash(clientID),
		configurationJSON: string(configJSON),
		secretJSON:        string(secretJSON),
	}, nil
}

func buildSAMLProviderConfigWrite(body adminProviderConfigWriteRequest) (builtProviderConfigWrite, error) {
	entityID := strings.TrimSpace(body.EntityID)
	metadataURL := strings.TrimSpace(body.MetadataURL)
	metadataXML := strings.TrimSpace(body.MetadataXML)
	privateKey := body.SPPrivateKey
	cert := body.SPCertificate
	if entityID == "" {
		return builtProviderConfigWrite{}, fmt.Errorf("entity_id is required for a saml provider config")
	}
	if metadataURL == "" && metadataXML == "" {
		return builtProviderConfigWrite{}, fmt.Errorf("metadata_url or metadata_xml is required for a saml provider config")
	}
	if strings.TrimSpace(privateKey) == "" || strings.TrimSpace(cert) == "" {
		return builtProviderConfigWrite{}, fmt.Errorf("sp_private_key and sp_certificate are required: write-only secrets must be resupplied on every create or update")
	}

	configJSON, err := json.Marshal(samlConfigurationFields{
		MetadataURL:             metadataURL,
		MetadataXML:             metadataXML,
		EntityID:                entityID,
		GroupAttribute:          strings.TrimSpace(body.GroupAttribute),
		ServiceProviderEntityID: strings.TrimSpace(body.ServiceProviderEntityID),
		ServiceProviderACSURL:   strings.TrimSpace(body.ServiceProviderACSURL),
	})
	if err != nil {
		return builtProviderConfigWrite{}, fmt.Errorf("encode saml configuration: %w", err)
	}
	secretJSON, err := json.Marshal(samlSecretFields{SPPrivateKey: privateKey, SPCertificate: cert})
	if err != nil {
		return builtProviderConfigWrite{}, fmt.Errorf("encode saml secret: %w", err)
	}

	metadataForHash := metadataXML
	if metadataForHash == "" {
		metadataForHash = metadataURL
	}

	return builtProviderConfigWrite{
		kind:              "external_saml",
		keyHash:           localIdentityHash(entityID),
		entityIDHash:      localIdentityHash(entityID),
		metadataURLHash:   localIdentityHash(metadataURL),
		configurationJSON: string(configJSON),
		secretJSON:        string(secretJSON),
		metadataForHash:   metadataForHash,
	}, nil
}
