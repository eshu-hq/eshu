// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

const (
	samlFixtureIDP       = "https://idp.example.test/saml"
	samlFixtureSP        = "https://api.example.test/api/v0/auth/saml/providers/provider_a/metadata"
	samlFixtureACS       = "https://api.example.test/api/v0/auth/saml/providers/provider_a/acs"
	samlFixtureRequestID = "request-fixture-1"
)

// samlRecoverFixture returns a provider whose metadata passes
// newCrewjamServiceProvider and an unsigned response envelope that passes the
// request-ID check, plus the request-ID hash the handler would pass.
func samlRecoverFixture(t *testing.T, now time.Time) (SAMLProviderConfig, string, string) {
	t.Helper()
	metadata := testQueryIDPMetadataValidAt(t, samlFixtureIDP, now)
	provider := SAMLProviderConfig{
		ProviderConfigID:                 "provider_a",
		ServiceProvider:                  samlauth.ServiceProviderConfig{EntityID: samlFixtureSP, ACSURL: samlFixtureACS},
		IdentityProviderMetadataXML:      []byte(metadata),
		ExpectedIdentityProviderEntityID: samlFixtureIDP,
		GroupMapping:                     testSAMLProvider().GroupMapping,
	}
	responseXML := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-fixture-1" InResponseTo="` +
		samlFixtureRequestID + `" Version="2.0"></samlp:Response>`
	return provider, base64.StdEncoding.EncodeToString([]byte(responseXML)), BrowserSessionSecretHash(samlFixtureRequestID)
}

func testQueryIDPMetadataValidAt(t *testing.T, entityID string, now time.Time) string {
	t.Helper()
	return testQueryIDPMetadata(t, entityID, "https://idp.example.test/sso", now.Add(time.Hour))
}
