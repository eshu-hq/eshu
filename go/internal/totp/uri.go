// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package totp

import (
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ProvisioningURIParams names the fields ProvisioningURI encodes into an
// otpauth:// URI. Digits and Period default to DefaultDigits and
// DefaultStep when zero, so callers enrolling with the production defaults
// can leave them unset.
type ProvisioningURIParams struct {
	// Issuer identifies the service in the authenticator app's entry list
	// (for example "Eshu"). Required; must not contain ':'.
	Issuer string
	// Account identifies the enrolling user (for example an email or
	// username). Required; must not contain ':'.
	Account string
	// Secret is the raw (unsealed) shared secret this URI encodes. Required.
	// Callers must only pass a freshly generated or freshly opened secret —
	// this function never touches storage.
	Secret []byte
	// Digits is the code length the authenticator app should generate.
	// Defaults to DefaultDigits when zero.
	Digits int
	// Period is the TOTP time step the authenticator app should use.
	// Defaults to DefaultStep when zero.
	Period time.Duration
}

// ProvisioningURI builds the otpauth://totp/... provisioning URI a
// standards-compliant authenticator app (Google Authenticator, Authy, 1Password,
// etc.) decodes from a QR code, per the de facto "Key Uri Format" convention
// (https://github.com/google/google-authenticator/wiki/Key-Uri-Format).
// This package renders no QR image; the console renders the QR client-side
// from this URI text so the plaintext secret only ever exists in the
// browser and the single server-side verification call, never in a
// server-generated image.
func ProvisioningURI(params ProvisioningURIParams) (string, error) {
	issuer := strings.TrimSpace(params.Issuer)
	account := strings.TrimSpace(params.Account)
	if issuer == "" {
		return "", errors.New("totp: provisioning uri requires issuer")
	}
	if account == "" {
		return "", errors.New("totp: provisioning uri requires account")
	}
	if len(params.Secret) == 0 {
		return "", errors.New("totp: provisioning uri requires secret")
	}
	if strings.Contains(issuer, ":") || strings.Contains(account, ":") {
		return "", errors.New("totp: issuer and account must not contain ':'")
	}
	digits := params.Digits
	if digits == 0 {
		digits = DefaultDigits
	}
	if _, ok := digitModulus[digits]; !ok {
		return "", fmt.Errorf("totp: digits must be %d-%d, got %d", minDigits, maxDigits, digits)
	}
	period := params.Period
	if period == 0 {
		period = DefaultStep
	}
	if period <= 0 {
		return "", errors.New("totp: period must be positive")
	}

	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(params.Secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", digits))
	q.Set("period", fmt.Sprintf("%d", int64(period.Seconds())))

	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode()), nil
}
