package samlauth

import "errors"

var (
	// ErrMetadataInvalid means IdP metadata could not be parsed or used safely.
	ErrMetadataInvalid = errors.New("saml metadata invalid")
	// ErrMetadataIssuerMismatch means metadata entityID does not match config.
	ErrMetadataIssuerMismatch = errors.New("saml metadata issuer mismatch")
	// ErrMetadataMissingSSO means metadata has no usable SSO endpoint.
	ErrMetadataMissingSSO = errors.New("saml metadata missing sso endpoint")
	// ErrMetadataMissingSigningCertificate means metadata has no signing cert.
	ErrMetadataMissingSigningCertificate = errors.New("saml metadata missing signing certificate")
	// ErrMetadataExpired means metadata validUntil has already passed.
	ErrMetadataExpired = errors.New("saml metadata expired")
	// ErrNameIDMissing means the assertion did not include a usable NameID.
	ErrNameIDMissing = errors.New("saml nameid missing")
	// ErrMissingGroupClaims means required SAML group attributes were absent.
	ErrMissingGroupClaims = errors.New("saml group claims missing")
	// ErrAssertionNotYetValid means assertion NotBefore is outside clock skew.
	ErrAssertionNotYetValid = errors.New("saml assertion not yet valid")
	// ErrAssertionExpired means assertion NotOnOrAfter is outside clock skew.
	ErrAssertionExpired = errors.New("saml assertion expired")
	// ErrReplayIdentifierMissing means replay protection lacks stable SAML IDs.
	ErrReplayIdentifierMissing = errors.New("saml replay identifier missing")
)
