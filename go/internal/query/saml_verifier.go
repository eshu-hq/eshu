package query

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

const defaultSAMLClockSkew = 3 * time.Minute

// CrewjamSAMLVerifier validates POSTed SAML responses with crewjam/saml.
type CrewjamSAMLVerifier struct {
	Now func() time.Time
}

// CrewjamSAMLRequestBuilder builds AuthnRequest redirects with crewjam/saml.
type CrewjamSAMLRequestBuilder struct {
	Now func() time.Time
}

// BuildSAMLRedirect builds a SP-initiated redirect AuthnRequest.
func (b CrewjamSAMLRequestBuilder) BuildSAMLRedirect(
	_ context.Context,
	provider SAMLProviderConfig,
	relayState string,
) (SAMLAuthnRequest, error) {
	sp, err := newCrewjamServiceProvider(provider, b.now())
	if err != nil {
		return SAMLAuthnRequest{}, err
	}
	idpURL := sp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if idpURL == "" {
		return SAMLAuthnRequest{}, samlauth.ErrMetadataMissingSSO
	}
	request, err := sp.MakeAuthenticationRequest(idpURL, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		return SAMLAuthnRequest{}, err
	}
	redirectURL, err := request.Redirect(relayState, &sp)
	if err != nil {
		return SAMLAuthnRequest{}, err
	}
	return SAMLAuthnRequest{RequestID: request.ID, RedirectURL: redirectURL.String()}, nil
}

func (b CrewjamSAMLRequestBuilder) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// VerifySAMLResponse validates a base64 POST binding response and returns the
// assertion material Eshu needs for replay, claim mapping, and session creation.
func (v CrewjamSAMLVerifier) VerifySAMLResponse(
	ctx context.Context,
	provider SAMLProviderConfig,
	samlResponse string,
	possibleRequestIDHashes []string,
) (SAMLAssertion, error) {
	now := time.Now().UTC()
	if v.Now != nil {
		now = v.Now().UTC()
	}
	sp, err := newCrewjamServiceProvider(provider, now)
	if err != nil {
		return SAMLAssertion{}, err
	}
	requestID, err := requestIDFromSAMLResponse(samlResponse)
	if err != nil {
		return SAMLAssertion{}, err
	}
	if !matchesRequestIDHash(requestID, possibleRequestIDHashes) {
		return SAMLAssertion{}, fmt.Errorf("saml response request id did not match issued request")
	}
	form := url.Values{"SAMLResponse": []string{samlResponse}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.ServiceProvider.ACSURL, strings.NewReader(form.Encode()))
	if err != nil {
		return SAMLAssertion{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	assertion, err := sp.ParseResponse(req, []string{requestID})
	if err != nil {
		return SAMLAssertion{}, err
	}
	return samlAssertionFromCrewjam(assertion, provider.ClockSkew), nil
}

func requestIDFromSAMLResponse(samlResponse string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(samlResponse))
	if err != nil {
		return "", fmt.Errorf("decode saml response: %w", err)
	}
	var response struct {
		InResponseTo string `xml:"InResponseTo,attr"`
	}
	if err := xml.Unmarshal(raw, &response); err != nil {
		return "", fmt.Errorf("parse saml response envelope: %w", err)
	}
	if strings.TrimSpace(response.InResponseTo) == "" {
		return "", fmt.Errorf("saml response missing InResponseTo")
	}
	return strings.TrimSpace(response.InResponseTo), nil
}

func matchesRequestIDHash(requestID string, requestIDHashes []string) bool {
	requestIDHash := BrowserSessionSecretHash(requestID)
	for _, candidate := range requestIDHashes {
		if candidate == requestIDHash {
			return true
		}
	}
	return false
}

func newCrewjamServiceProvider(provider SAMLProviderConfig, now time.Time) (saml.ServiceProvider, error) {
	if _, err := samlauth.ValidateIdentityProviderMetadata(samlauth.MetadataValidationInput{
		MetadataXML:      provider.IdentityProviderMetadataXML,
		ExpectedEntityID: provider.ExpectedIdentityProviderEntityID,
		Now:              now,
	}); err != nil {
		return saml.ServiceProvider{}, err
	}
	idpMetadata, err := samlsp.ParseMetadata(provider.IdentityProviderMetadataXML)
	if err != nil {
		return saml.ServiceProvider{}, fmt.Errorf("parse saml metadata: %w", err)
	}
	metadataURL, err := url.Parse(provider.ServiceProvider.EntityID)
	if err != nil {
		return saml.ServiceProvider{}, fmt.Errorf("parse saml entity id: %w", err)
	}
	acsURL, err := url.Parse(provider.ServiceProvider.ACSURL)
	if err != nil {
		return saml.ServiceProvider{}, fmt.Errorf("parse saml acs url: %w", err)
	}
	return saml.ServiceProvider{
		EntityID:              provider.ServiceProvider.EntityID,
		MetadataURL:           *metadataURL,
		AcsURL:                *acsURL,
		IDPMetadata:           idpMetadata,
		AuthnNameIDFormat:     saml.PersistentNameIDFormat,
		AllowIDPInitiated:     false,
		DefaultRedirectURI:    "",
		LogoutBindings:        []string{saml.HTTPPostBinding},
		MetadataValidDuration: time.Hour,
	}, nil
}

func samlAssertionFromCrewjam(assertion *saml.Assertion, clockSkew time.Duration) SAMLAssertion {
	if clockSkew <= 0 {
		clockSkew = defaultSAMLClockSkew
	}
	claims := samlauth.AssertionClaims{Attributes: map[string][]string{}}
	if assertion != nil && assertion.Subject != nil && assertion.Subject.NameID != nil {
		claims.NameID = assertion.Subject.NameID.Value
	}
	if assertion != nil {
		for _, statement := range assertion.AttributeStatements {
			for _, attribute := range statement.Attributes {
				values := make([]string, 0, len(attribute.Values))
				for _, value := range attribute.Values {
					values = append(values, value.Value)
				}
				if attribute.Name != "" {
					claims.Attributes[attribute.Name] = append(claims.Attributes[attribute.Name], values...)
				}
				if attribute.FriendlyName != "" {
					claims.Attributes[attribute.FriendlyName] = append(claims.Attributes[attribute.FriendlyName], values...)
				}
			}
		}
	}
	window := samlauth.AssertionWindow{ClockSkew: clockSkew}
	if assertion != nil && assertion.Conditions != nil {
		window.NotBefore = assertion.Conditions.NotBefore
		window.NotOnOrAfter = assertion.Conditions.NotOnOrAfter
	}
	if assertion != nil {
		for _, statement := range assertion.AuthnStatements {
			if statement.SessionNotOnOrAfter == nil {
				continue
			}
			if window.NotOnOrAfter.IsZero() || statement.SessionNotOnOrAfter.Before(window.NotOnOrAfter) {
				window.NotOnOrAfter = *statement.SessionNotOnOrAfter
			}
		}
	}
	result := SAMLAssertion{
		Claims: claims,
		Window: window,
	}
	if assertion != nil {
		result.AssertionID = assertion.ID
	}
	return result
}
