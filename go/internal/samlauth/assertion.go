package samlauth

import (
	"strings"
	"time"
)

// AssertionWindow carries SAML assertion validity timestamps and clock skew.
type AssertionWindow struct {
	NotBefore    time.Time
	NotOnOrAfter time.Time
	ClockSkew    time.Duration
}

// ValidateAssertionWindow checks assertion timestamps with a bounded skew.
func ValidateAssertionWindow(now time.Time, window AssertionWindow) error {
	if !window.NotBefore.IsZero() && now.Add(window.ClockSkew).Before(window.NotBefore) {
		return ErrAssertionNotYetValid
	}
	if !window.NotOnOrAfter.IsZero() && !now.Add(-window.ClockSkew).Before(window.NotOnOrAfter) {
		return ErrAssertionExpired
	}
	return nil
}

// ReplayInput carries SAML identifiers used to build a durable replay key.
type ReplayInput struct {
	ProviderConfigID string
	RequestID        string
	ResponseID       string
	AssertionID      string
}

// ReplayFingerprint returns a hash-only replay key for storage reservation.
func ReplayFingerprint(input ReplayInput) (string, error) {
	providerID := strings.TrimSpace(input.ProviderConfigID)
	responseID := strings.TrimSpace(input.ResponseID)
	assertionID := strings.TrimSpace(input.AssertionID)
	if providerID == "" || (responseID == "" && assertionID == "") {
		return "", ErrReplayIdentifierMissing
	}
	return stableHash(
		"saml-replay",
		providerID,
		strings.TrimSpace(input.RequestID),
		responseID,
		assertionID,
	), nil
}
