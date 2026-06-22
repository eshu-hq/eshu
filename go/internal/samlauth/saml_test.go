package samlauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateIdentityProviderMetadataRequiresIssuerSSOAndSigningCertificate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	cert := testCertificateBase64(t)
	valid := testIDPMetadata(testIDPMetadataInput{
		EntityID: "https://idp.example.test/saml",
		SSOURL:   "https://idp.example.test/app/eshu/sso/saml",
		Certs:    []string{cert},
		Until:    now.Add(time.Hour),
	})

	got, err := ValidateIdentityProviderMetadata(MetadataValidationInput{
		MetadataXML:      []byte(valid),
		ExpectedEntityID: "https://idp.example.test/saml",
		Now:              now,
	})
	if err != nil {
		t.Fatalf("ValidateIdentityProviderMetadata() error = %v, want nil", err)
	}
	if got.EntityID != "https://idp.example.test/saml" {
		t.Fatalf("entity id = %q, want IdP issuer", got.EntityID)
	}
	if got.SingleSignOnURL != "https://idp.example.test/app/eshu/sso/saml" {
		t.Fatalf("sso url = %q, want metadata SSO URL", got.SingleSignOnURL)
	}
	if got.SigningCertificateCount != 1 {
		t.Fatalf("signing cert count = %d, want 1", got.SigningCertificateCount)
	}
	if !got.ValidUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("validUntil = %v, want %v", got.ValidUntil, now.Add(time.Hour))
	}

	tests := []struct {
		name string
		xml  string
		want error
	}{
		{
			name: "wrong issuer",
			xml: testIDPMetadata(testIDPMetadataInput{
				EntityID: "https://wrong.example.test/saml",
				SSOURL:   "https://idp.example.test/app/eshu/sso/saml",
				Certs:    []string{cert},
				Until:    now.Add(time.Hour),
			}),
			want: ErrMetadataIssuerMismatch,
		},
		{
			name: "missing sso",
			xml: testIDPMetadata(testIDPMetadataInput{
				EntityID: "https://idp.example.test/saml",
				Certs:    []string{cert},
				Until:    now.Add(time.Hour),
			}),
			want: ErrMetadataMissingSSO,
		},
		{
			name: "missing signing certificate",
			xml: testIDPMetadata(testIDPMetadataInput{
				EntityID: "https://idp.example.test/saml",
				SSOURL:   "https://idp.example.test/app/eshu/sso/saml",
				Until:    now.Add(time.Hour),
			}),
			want: ErrMetadataMissingSigningCertificate,
		},
		{
			name: "expired metadata",
			xml: testIDPMetadata(testIDPMetadataInput{
				EntityID: "https://idp.example.test/saml",
				SSOURL:   "https://idp.example.test/app/eshu/sso/saml",
				Certs:    []string{cert},
				Until:    now.Add(-time.Second),
			}),
			want: ErrMetadataExpired,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ValidateIdentityProviderMetadata(MetadataValidationInput{
				MetadataXML:      []byte(tc.xml),
				ExpectedEntityID: "https://idp.example.test/saml",
				Now:              now,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateIdentityProviderMetadata() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRenderServiceProviderMetadataUsesPostACSAndNoThirdPartySessionState(t *testing.T) {
	t.Parallel()

	got, err := RenderServiceProviderMetadata(ServiceProviderConfig{
		EntityID: "https://api.example.test/api/v0/auth/saml/providers/provider_a/metadata",
		ACSURL:   "https://api.example.test/api/v0/auth/saml/providers/provider_a/acs",
	})
	if err != nil {
		t.Fatalf("RenderServiceProviderMetadata() error = %v, want nil", err)
	}
	if !strings.Contains(got, `entityID="https://api.example.test/api/v0/auth/saml/providers/provider_a/metadata"`) {
		t.Fatalf("metadata missing SP entityID: %s", got)
	}
	if !strings.Contains(got, `Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"`) {
		t.Fatalf("metadata missing POST ACS binding: %s", got)
	}
	if !strings.Contains(got, `Location="https://api.example.test/api/v0/auth/saml/providers/provider_a/acs"`) {
		t.Fatalf("metadata missing ACS URL: %s", got)
	}
	if strings.Contains(strings.ToLower(got), "cookie") || strings.Contains(strings.ToLower(got), "session") {
		t.Fatalf("SP metadata should not advertise third-party session behavior: %s", got)
	}

	var parsed struct {
		XMLName xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:metadata EntityDescriptor"`
	}
	if err := xml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("metadata XML is invalid: %v", err)
	}
}

func TestNormalizeClaimsHashesSubjectAndSortsRequiredGroups(t *testing.T) {
	t.Parallel()

	got, err := NormalizeClaims(AssertionClaims{
		NameID: " User@example.TEST ",
		Attributes: map[string][]string{
			"groups": {" Eshu-Admins ", "developers", "eshu-admins", ""},
			"email":  {"user@example.test"},
		},
	}, ClaimMapping{
		GroupAttributeNames: []string{"groups"},
		RequireGroups:       true,
		HashScope:           "tenant_a/provider_a",
	})
	if err != nil {
		t.Fatalf("NormalizeClaims() error = %v, want nil", err)
	}
	if got.ExternalSubjectHash == "" || !strings.HasPrefix(got.ExternalSubjectHash, "sha256:") {
		t.Fatalf("external subject hash = %q, want sha256 hash", got.ExternalSubjectHash)
	}
	if strings.Contains(got.ExternalSubjectHash, "User@example") {
		t.Fatalf("external subject hash leaked raw NameID: %q", got.ExternalSubjectHash)
	}
	if got.GroupClaimHash == "" || !strings.HasPrefix(got.GroupClaimHash, "sha256:") {
		t.Fatalf("group claim hash = %q, want sha256 hash", got.GroupClaimHash)
	}
	if strings.Contains(got.GroupClaimHash, "Eshu-Admins") {
		t.Fatalf("group claim hash leaked raw group: %q", got.GroupClaimHash)
	}
	wantGroups := []string{"developers", "eshu-admins"}
	if !reflect.DeepEqual(got.GroupKeys, wantGroups) {
		t.Fatalf("group keys = %#v, want %#v", got.GroupKeys, wantGroups)
	}

	_, err = NormalizeClaims(AssertionClaims{NameID: "user@example.test"}, ClaimMapping{
		GroupAttributeNames: []string{"groups"},
		RequireGroups:       true,
		HashScope:           "tenant_a/provider_a",
	})
	if !errors.Is(err, ErrMissingGroupClaims) {
		t.Fatalf("NormalizeClaims() error = %v, want %v", err, ErrMissingGroupClaims)
	}
}

func TestValidateAssertionWindowAppliesClockSkewAndExpiration(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 15, 0, 0, 0, time.UTC)
	err := ValidateAssertionWindow(now, AssertionWindow{
		NotBefore:    now.Add(2 * time.Minute),
		NotOnOrAfter: now.Add(10 * time.Minute),
		ClockSkew:    3 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ValidateAssertionWindow() error = %v, want nil inside skew", err)
	}

	_, tests := now, []struct {
		name   string
		window AssertionWindow
		want   error
	}{
		{
			name: "too early",
			window: AssertionWindow{
				NotBefore:    now.Add(4 * time.Minute),
				NotOnOrAfter: now.Add(10 * time.Minute),
				ClockSkew:    3 * time.Minute,
			},
			want: ErrAssertionNotYetValid,
		},
		{
			name: "expired",
			window: AssertionWindow{
				NotBefore:    now.Add(-10 * time.Minute),
				NotOnOrAfter: now.Add(-4 * time.Minute),
				ClockSkew:    3 * time.Minute,
			},
			want: ErrAssertionExpired,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateAssertionWindow(now, tc.window)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateAssertionWindow() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReplayFingerprintRequiresStableAssertionOrResponseIdentifiers(t *testing.T) {
	t.Parallel()

	got, err := ReplayFingerprint(ReplayInput{
		ProviderConfigID: "provider_a",
		RequestID:        "request-1",
		ResponseID:       "response-1",
		AssertionID:      "assertion-1",
	})
	if err != nil {
		t.Fatalf("ReplayFingerprint() error = %v, want nil", err)
	}
	again, err := ReplayFingerprint(ReplayInput{
		ProviderConfigID: "provider_a",
		RequestID:        "request-1",
		ResponseID:       "response-1",
		AssertionID:      "assertion-1",
	})
	if err != nil {
		t.Fatalf("ReplayFingerprint() second error = %v, want nil", err)
	}
	if got != again {
		t.Fatalf("replay fingerprint unstable: %q then %q", got, again)
	}
	if !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("replay fingerprint = %q, want sha256 hash", got)
	}
	if strings.Contains(got, "assertion-1") || strings.Contains(got, "response-1") {
		t.Fatalf("replay fingerprint leaked raw SAML identifiers: %q", got)
	}

	_, err = ReplayFingerprint(ReplayInput{ProviderConfigID: "provider_a"})
	if !errors.Is(err, ErrReplayIdentifierMissing) {
		t.Fatalf("ReplayFingerprint() error = %v, want %v", err, ErrReplayIdentifierMissing)
	}
}

type testIDPMetadataInput struct {
	EntityID string
	SSOURL   string
	Certs    []string
	Until    time.Time
}

func testIDPMetadata(in testIDPMetadataInput) string {
	var certs strings.Builder
	for _, cert := range in.Certs {
		certs.WriteString(`<md:KeyDescriptor use="signing"><ds:KeyInfo><ds:X509Data><ds:X509Certificate>`)
		certs.WriteString(cert)
		certs.WriteString(`</ds:X509Certificate></ds:X509Data></ds:KeyInfo></md:KeyDescriptor>`)
	}
	sso := ""
	if in.SSOURL != "" {
		sso = `<md:SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="` + in.SSOURL + `"/>`
	}
	return `<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" xmlns:ds="http://www.w3.org/2000/09/xmldsig#" entityID="` +
		in.EntityID + `" validUntil="` + in.Until.Format(time.RFC3339) + `"><md:IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">` +
		certs.String() + sso + `</md:IDPSSODescriptor></md:EntityDescriptor>`
}

func testCertificateBase64(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "idp.example.test",
		},
		NotBefore: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}
