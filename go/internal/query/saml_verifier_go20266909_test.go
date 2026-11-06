// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	"github.com/beevik/etree"
	"github.com/crewjam/saml/xmlenc"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

// The tests in this file are the regression suite for GO-2026-6909 in
// crewjam/saml. The test names carry the advisory ID without dashes
// (GO20266909) because scripts/ci/govulncheck-replace-recheck.sh selects them
// by that name: it runs them once with the replace directive dropped (they must
// fail on upstream crewjam/saml) and once with the real go.mod (they must
// pass). Each row is an IdP-signed assertion driven through the real
// CrewjamSAMLVerifier, in plaintext and inside an EncryptedAssertion.

type go20266909Fixture struct {
	provider SAMLProviderConfig
	idpKey   *rsa.PrivateKey
	idpCert  []byte
	spCert   *x509.Certificate
}

func newGO20266909Fixture(t *testing.T, now time.Time) go20266909Fixture {
	t.Helper()
	idpKey, idpDER := go20266909KeyAndCert(t, "idp.example.test", now)
	spKey, spDER := go20266909KeyAndCert(t, "sp.example.test", now)
	spCert, err := x509.ParseCertificate(spDER)
	if err != nil {
		t.Fatalf("parse sp cert: %v", err)
	}
	metadata := `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" xmlns:ds="http://www.w3.org/2000/09/xmldsig#" entityID="` +
		samlFixtureIDP + `" validUntil="` + now.Add(time.Hour).Format(time.RFC3339) +
		`"><md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">` +
		`<md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>` +
		base64.StdEncoding.EncodeToString(idpDER) +
		`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>` +
		`<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.test/sso"/>` +
		`</md:IDPSSODescriptor></md:EntityDescriptor>`
	return go20266909Fixture{
		provider: SAMLProviderConfig{
			ProviderConfigID: "provider_a",
			ServiceProvider: samlauth.ServiceProviderConfig{
				EntityID: samlFixtureSP, ACSURL: samlFixtureACS, Key: spKey, Certificate: spCert,
			},
			IdentityProviderMetadataXML:      []byte(metadata),
			ExpectedIdentityProviderEntityID: samlFixtureIDP,
		},
		idpKey:  idpKey,
		idpCert: idpDER,
		spCert:  spCert,
	}
}

func go20266909KeyAndCert(t *testing.T, cn string, now time.Time) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return key, der
}

type go20266909Case struct {
	name        string
	omitIssuer  bool
	omitSubject bool
	omitSCData  bool
	omitCond    bool
	nameIDs     string
	// wantRejected means VerifySAMLResponse must return an ordinary error. A
	// recovered panic (ErrSAMLVerifierPanic) does not count: the library must
	// reject these assertions itself.
	wantRejected bool
	// wantNameID is the NameID the verifier must report when accepted.
	wantNameID string
}

const (
	go20266909GoodNameID = `<saml:NameID Format="urn:oasis:names:tc:SAML:2.0:nameid-format:persistent">alice</saml:NameID>`
	go20266909EvilNameID = `<evil:NameID xmlns:evil="urn:evil:ns">admin-victim</evil:NameID>`
)

func go20266909Cases() []go20266909Case {
	return []go20266909Case{
		{name: "control_valid", nameIDs: go20266909GoodNameID, wantNameID: "alice"},
		{name: "no_subject", omitSubject: true, wantRejected: true},
		{name: "no_subject_confirmation_data", nameIDs: go20266909GoodNameID, omitSCData: true, wantRejected: true},
		{name: "no_conditions", nameIDs: go20266909GoodNameID, omitCond: true, wantRejected: true},
		{name: "foreign_namespace_nameid_only", nameIDs: go20266909EvilNameID, wantNameID: ""},
		{name: "saml_nameid_then_foreign_nameid", nameIDs: go20266909GoodNameID + go20266909EvilNameID, wantNameID: "alice"},
		{name: "foreign_nameid_then_saml_nameid", nameIDs: go20266909EvilNameID + go20266909GoodNameID, wantNameID: "alice"},
		{name: "no_nameid", nameIDs: "", wantNameID: ""},
		// An assertion with no Issuer must be rejected as an ordinary error. It
		// passes on upstream and on the fork alike, like control_valid, and pins
		// the advisory's Issuer row so a future crewjam bump cannot regress it.
		{name: "no_issuer", omitIssuer: true, nameIDs: go20266909GoodNameID, wantRejected: true},
	}
}

func (c go20266909Case) assertionXML(now time.Time) string {
	const ns = `xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"`
	ts := now.UTC().Format(time.RFC3339)
	later := now.Add(5 * time.Minute).UTC().Format(time.RFC3339)
	var b strings.Builder
	b.WriteString(`<saml:Assertion ` + ns + ` ID="assertion-1" IssueInstant="` + ts + `" Version="2.0">`)
	if !c.omitIssuer {
		b.WriteString(`<saml:Issuer>` + samlFixtureIDP + `</saml:Issuer>`)
	}
	if !c.omitSubject {
		b.WriteString(`<saml:Subject>` + c.nameIDs)
		b.WriteString(`<saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">`)
		if !c.omitSCData {
			b.WriteString(`<saml:SubjectConfirmationData InResponseTo="` + samlFixtureRequestID + `" NotOnOrAfter="` + later + `" Recipient="` + samlFixtureACS + `"/>`)
		}
		b.WriteString(`</saml:SubjectConfirmation></saml:Subject>`)
	}
	if !c.omitCond {
		b.WriteString(`<saml:Conditions NotBefore="` + ts + `" NotOnOrAfter="` + later + `"><saml:AudienceRestriction><saml:Audience>` + samlFixtureSP + `</saml:Audience></saml:AudienceRestriction></saml:Conditions>`)
	}
	b.WriteString(`<saml:AuthnStatement AuthnInstant="` + ts + `" SessionIndex="s1"><saml:AuthnContext><saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef></saml:AuthnContext></saml:AuthnStatement>`)
	b.WriteString(`</saml:Assertion>`)
	return b.String()
}

// signedAssertion signs the assertion with the IdP key, as a real IdP would.
func (f go20266909Fixture) signedAssertion(t *testing.T, c go20266909Case, now time.Time) string {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(c.assertionXML(now)); err != nil {
		t.Fatalf("parse assertion: %v", err)
	}
	ctx, err := dsig.NewSigningContext(f.idpKey, [][]byte{f.idpCert})
	if err != nil {
		t.Fatalf("signing context: %v", err)
	}
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	signed, err := ctx.SignEnveloped(doc.Root())
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	out := etree.NewDocument()
	out.SetRoot(signed)
	xmlText, err := out.WriteToString()
	if err != nil {
		t.Fatalf("serialize signed assertion: %v", err)
	}
	return xmlText
}

// encryptedAssertion encrypts the signed assertion to the SP certificate.
func (f go20266909Fixture) encryptedAssertion(t *testing.T, signed string) string {
	t.Helper()
	cipher := xmlenc.OAEP()
	cipher.BlockCipher = xmlenc.AES128CBC
	cipher.DigestMethod = &xmlenc.SHA1
	el, err := cipher.Encrypt(f.spCert, []byte(signed), nil)
	if err != nil {
		t.Fatalf("encrypt assertion: %v", err)
	}
	wrap := etree.NewElement("saml:EncryptedAssertion")
	wrap.AddChild(el)
	doc := etree.NewDocument()
	doc.SetRoot(wrap)
	text, err := doc.WriteToString()
	if err != nil {
		t.Fatalf("serialize encrypted assertion: %v", err)
	}
	return text
}

func go20266909Response(now time.Time, assertionPart string) string {
	return `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ` +
		`ID="response-1" InResponseTo="` + samlFixtureRequestID + `" Version="2.0" IssueInstant="` + now.UTC().Format(time.RFC3339) +
		`" Destination="` + samlFixtureACS + `"><saml:Issuer>` + samlFixtureIDP + `</saml:Issuer>` +
		`<samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>` +
		assertionPart + `</samlp:Response>`
}

func TestVerifySAMLResponseGO20266909(t *testing.T) {
	now := time.Now().UTC()
	fixture := newGO20266909Fixture(t, now)
	modes := []struct {
		name string
		wrap func(t *testing.T, signed string) string
	}{
		{"plaintext", func(_ *testing.T, signed string) string { return signed }},
		{"encrypted_assertion", fixture.encryptedAssertion},
	}
	for _, mode := range modes {
		for _, tc := range go20266909Cases() {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				signed := fixture.signedAssertion(t, tc, now)
				response := go20266909Response(now, mode.wrap(t, signed))
				encoded := base64.StdEncoding.EncodeToString([]byte(response))
				verifier := CrewjamSAMLVerifier{Now: func() time.Time { return now }}

				got, err := verifier.VerifySAMLResponse(context.Background(), fixture.provider, encoded,
					[]string{BrowserSessionSecretHash(samlFixtureRequestID)})
				if errors.Is(err, ErrSAMLVerifierPanic) {
					t.Fatalf("crewjam panicked and the verifier recovered it; the library must reject or handle this assertion itself")
				}
				if tc.wantRejected {
					if err == nil {
						t.Fatalf("accepted NameID %q, want rejection", got.Claims.NameID)
					}
					return
				}
				if err != nil {
					t.Fatalf("VerifySAMLResponse() error = %v, want accepted", err)
				}
				if got.Claims.NameID != tc.wantNameID {
					t.Fatalf("NameID = %q, want %q", got.Claims.NameID, tc.wantNameID)
				}
			})
		}
	}
}
