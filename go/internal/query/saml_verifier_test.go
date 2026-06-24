package query

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

func TestRequestIDFromSAMLResponseExtractsInResponseTo(t *testing.T) {
	t.Parallel()

	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1" InResponseTo="request-1"></Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	got, err := requestIDFromSAMLResponse(encoded)
	if err != nil {
		t.Fatalf("requestIDFromSAMLResponse() error = %v", err)
	}
	if got != "request-1" {
		t.Fatalf("request id = %q, want request-1", got)
	}
}

func TestResponseEnvelopeIDsFromSAMLResponseExtractsRequestAndResponseIDs(t *testing.T) {
	t.Parallel()

	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1" InResponseTo="request-1"></Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	requestID, responseID, err := responseEnvelopeIDsFromSAMLResponse(encoded)
	if err != nil {
		t.Fatalf("responseEnvelopeIDsFromSAMLResponse() error = %v", err)
	}
	if requestID != "request-1" || responseID != "response-1" {
		t.Fatalf("ids = %q/%q, want request-1/response-1", requestID, responseID)
	}
}

func TestRequestIDFromSAMLResponseRejectsMissingInResponseTo(t *testing.T) {
	t.Parallel()

	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1"></Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	if got, err := requestIDFromSAMLResponse(encoded); err == nil {
		t.Fatalf("requestIDFromSAMLResponse() = %q, nil error; want error", got)
	}
}

// TestVerifySAMLResponsePopulatesPostFormSoCrewjamReadsResponse is the
// regression test for the production SAML SSO failure where every real Okta
// assertion was rejected. crewjam/saml v0.5.1 ServiceProvider.ParseResponse
// reads req.PostForm.Get("SAMLResponse") without first calling
// req.ParseForm(), so when the verifier put the form in the request body
// PostForm stayed nil, crewjam base64-decoded the empty string, and returned an
// opaque *saml.InvalidResponseError wrapping "invalid xml: no root". The prior
// handler tests used a fake verifier and never exercised crewjam, so this gap
// shipped. This test drives the real CrewjamSAMLVerifier and asserts the error
// is no longer the "no root" decode failure: with the form parsed, crewjam
// reads the real envelope and fails later on signature/validation instead.
func TestVerifySAMLResponsePopulatesPostFormSoCrewjamReadsResponse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	idpEntityID := "https://idp.example.test/saml"
	metadata := testQueryIDPMetadata(t, idpEntityID, "https://idp.example.test/app/eshu/sso/saml", now.Add(time.Hour))

	requestID := "request-real-1"
	responseXML := `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ` +
		`ID="response-real-1" InResponseTo="` + requestID + `" Version="2.0">` +
		`<saml:Issuer xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">` + idpEntityID + `</saml:Issuer>` +
		`</samlp:Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	provider := SAMLProviderConfig{
		ProviderConfigID: "provider_a",
		ServiceProvider: samlauth.ServiceProviderConfig{
			EntityID: "https://api.example.test/api/v0/auth/saml/providers/provider_a/metadata",
			ACSURL:   "https://api.example.test/api/v0/auth/saml/providers/provider_a/acs",
		},
		IdentityProviderMetadataXML:      []byte(metadata),
		ExpectedIdentityProviderEntityID: idpEntityID,
	}

	verifier := CrewjamSAMLVerifier{Now: func() time.Time { return now }}
	_, err := verifier.VerifySAMLResponse(
		context.Background(),
		provider,
		encoded,
		[]string{BrowserSessionSecretHash(requestID)},
	)
	if err == nil {
		t.Fatalf("VerifySAMLResponse() error = nil, want signature/validation failure on unsigned response")
	}

	var ire *saml.InvalidResponseError
	if errors.As(err, &ire) && ire.PrivateErr != nil {
		if strings.Contains(ire.PrivateErr.Error(), "no root") {
			t.Fatalf("VerifySAMLResponse() returned crewjam decode error %q; PostForm was not populated before ParseResponse", ire.PrivateErr.Error())
		}
	}
}

func testQueryIDPMetadata(t *testing.T, entityID, ssoURL string, until time.Time) string {
	t.Helper()

	cert := testQueryCertificateBase64(t)
	return `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" ` +
		`xmlns:ds="http://www.w3.org/2000/09/xmldsig#" entityID="` + entityID +
		`" validUntil="` + until.Format(time.RFC3339) +
		`"><md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">` +
		`<md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>` + cert +
		`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>` +
		`<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="` + ssoURL + `"/>` +
		`</md:IDPSSODescriptor></md:EntityDescriptor>`
}

func testQueryCertificateBase64(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "idp.example.test"},
		NotBefore:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}
