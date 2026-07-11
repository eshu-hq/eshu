// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package totp

import (
	"strings"
	"testing"
	"time"
)

// rfc6238SHA1Secret is the exact 20-byte ASCII secret RFC 6238 Appendix B
// uses for every SHA1 test vector: "12345678901234567890".
const rfc6238SHA1Secret = "12345678901234567890"

// TestGenerateCode_RFC6238AppendixB_SHA1_8Digit proves the algorithm against
// the literal RFC 6238 Appendix B SHA1 test vectors (8-digit truncation, the
// vectors as published — the RFC's worked table only publishes 8-digit
// codes). This is the ground-truth conformance test: HMAC-SHA1 over the
// RFC 4226 dynamic truncation, keyed by the RFC 6238 30-second time-step
// counter.
func TestGenerateCode_RFC6238AppendixB_SHA1_8Digit(t *testing.T) {
	tests := []struct {
		name    string
		unixSec int64
		want    string
	}{
		{"T=59", 59, "94287082"},
		{"T=1111111109", 1111111109, "07081804"},
		{"T=1111111111", 1111111111, "14050471"},
		{"T=1234567890", 1234567890, "89005924"},
		{"T=2000000000", 2000000000, "69279037"},
		{"T=20000000000", 20000000000, "65353130"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateCode([]byte(rfc6238SHA1Secret), time.Unix(tt.unixSec, 0).UTC(), 30*time.Second, 8)
			if err != nil {
				t.Fatalf("GenerateCode returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("GenerateCode(%d) = %q, want %q (RFC 6238 Appendix B)", tt.unixSec, got, tt.want)
			}
		})
	}
}

// TestGenerateCode_SixDigitIsSuffixOfEightDigit proves the production 6-digit
// truncation used by this package (DefaultDigits) is mathematically the last
// six digits of the RFC's published 8-digit vector: HOTP truncation is
// `binary_code mod 10^Digits`, and because 10^6 divides 10^8,
// (x mod 10^8) mod 10^6 == x mod 10^6. This is not an independent vector; it
// is a derivation proof that ties the 6-digit production path back to the
// same RFC Appendix B ground truth verified above.
func TestGenerateCode_SixDigitIsSuffixOfEightDigit(t *testing.T) {
	tests := []struct {
		unixSec int64
		want8   string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, tt := range tests {
		want6 := tt.want8[len(tt.want8)-6:]
		got, err := GenerateCode([]byte(rfc6238SHA1Secret), time.Unix(tt.unixSec, 0).UTC(), 30*time.Second, 6)
		if err != nil {
			t.Fatalf("GenerateCode returned error: %v", err)
		}
		if got != want6 {
			t.Fatalf("GenerateCode(%d, digits=6) = %q, want %q (suffix of RFC 8-digit vector %q)", tt.unixSec, got, want6, tt.want8)
		}
	}
}

func TestGenerateCode_RejectsInvalidInput(t *testing.T) {
	now := time.Unix(59, 0).UTC()
	tests := []struct {
		name   string
		secret []byte
		step   time.Duration
		digits int
	}{
		{"empty secret", nil, 30 * time.Second, 6},
		{"zero step", []byte(rfc6238SHA1Secret), 0, 6},
		{"negative step", []byte(rfc6238SHA1Secret), -30 * time.Second, 6},
		{"digits too low", []byte(rfc6238SHA1Secret), 30 * time.Second, 5},
		{"digits too high", []byte(rfc6238SHA1Secret), 30 * time.Second, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := GenerateCode(tt.secret, now, tt.step, tt.digits); err == nil {
				t.Fatalf("GenerateCode(%+v) = nil error, want error", tt)
			}
		})
	}
}

func TestVerify_ExactStepMatches(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	code, err := GenerateCode([]byte(rfc6238SHA1Secret), at, DefaultStep, DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	ok, err := Verify([]byte(rfc6238SHA1Secret), code, at, DefaultStep, DefaultDigits, 1)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatalf("Verify(%q) = false, want true for the code just generated at the same instant", code)
	}
}

// TestVerify_ClockSkewWindow proves the +/-1 step skew window (RFC 6238
// Section 5.2 recommends tolerating clock drift) accepts a code generated
// one step early or late, but rejects one two steps away.
func TestVerify_ClockSkewWindow(t *testing.T) {
	base := time.Unix(1111111109, 0).UTC()
	oneStepEarly := base.Add(-DefaultStep)
	oneStepLate := base.Add(DefaultStep)
	twoStepsLate := base.Add(2 * DefaultStep)

	codeEarly, err := GenerateCode([]byte(rfc6238SHA1Secret), oneStepEarly, DefaultStep, DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode(early): %v", err)
	}
	codeLate, err := GenerateCode([]byte(rfc6238SHA1Secret), oneStepLate, DefaultStep, DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode(late): %v", err)
	}
	codeTwoLate, err := GenerateCode([]byte(rfc6238SHA1Secret), twoStepsLate, DefaultStep, DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode(twoLate): %v", err)
	}

	if ok, err := Verify([]byte(rfc6238SHA1Secret), codeEarly, base, DefaultStep, DefaultDigits, 1); err != nil || !ok {
		t.Fatalf("Verify(one step early) = %v, %v, want true, nil", ok, err)
	}
	if ok, err := Verify([]byte(rfc6238SHA1Secret), codeLate, base, DefaultStep, DefaultDigits, 1); err != nil || !ok {
		t.Fatalf("Verify(one step late) = %v, %v, want true, nil", ok, err)
	}
	if ok, err := Verify([]byte(rfc6238SHA1Secret), codeTwoLate, base, DefaultStep, DefaultDigits, 1); err != nil || ok {
		t.Fatalf("Verify(two steps late, skew=1) = %v, %v, want false, nil", ok, err)
	}
}

func TestVerify_RejectsWrongCode(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	ok, err := Verify([]byte(rfc6238SHA1Secret), "000000", at, DefaultStep, DefaultDigits, 1)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatalf("Verify(wrong code) = true, want false")
	}
}

func TestVerify_RejectsEmptyCode(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	ok, err := Verify([]byte(rfc6238SHA1Secret), "", at, DefaultStep, DefaultDigits, 1)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Fatalf("Verify(empty code) = true, want false")
	}
}

func TestVerify_TrimsWhitespace(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	code, err := GenerateCode([]byte(rfc6238SHA1Secret), at, DefaultStep, DefaultDigits)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	ok, err := Verify([]byte(rfc6238SHA1Secret), "  "+code+"\n", at, DefaultStep, DefaultDigits, 1)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatalf("Verify(padded code) = false, want true")
	}
}

func TestVerify_RejectsNegativeSkew(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	if _, err := Verify([]byte(rfc6238SHA1Secret), "000000", at, DefaultStep, DefaultDigits, -1); err == nil {
		t.Fatalf("Verify(skewSteps=-1) = nil error, want error")
	}
}

func TestGenerateSecret_ProducesRandomFixedLengthKeys(t *testing.T) {
	a, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	b, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	if len(a) != secretLengthBytes {
		t.Fatalf("len(secret) = %d, want %d", len(a), secretLengthBytes)
	}
	if string(a) == string(b) {
		t.Fatalf("two GenerateSecret calls returned the same secret, want independent random secrets")
	}
}

func TestProvisioningURI_EncodesExpectedFields(t *testing.T) {
	secret := []byte(rfc6238SHA1Secret)
	uri, err := ProvisioningURI(ProvisioningURIParams{
		Issuer:  "Eshu",
		Account: "alice@example.com",
		Secret:  secret,
		Digits:  DefaultDigits,
		Period:  DefaultStep,
	})
	if err != nil {
		t.Fatalf("ProvisioningURI: %v", err)
	}
	if !strings.HasPrefix(uri, "otpauth://totp/Eshu:alice@example.com?") {
		t.Fatalf("ProvisioningURI = %q, want otpauth://totp/Eshu:alice@example.com?...", uri)
	}
	for _, want := range []string{"secret=", "issuer=Eshu", "digits=6", "period=30", "algorithm=SHA1"} {
		if !strings.Contains(uri, want) {
			t.Fatalf("ProvisioningURI = %q, want it to contain %q", uri, want)
		}
	}
}

func TestProvisioningURI_RejectsMissingFields(t *testing.T) {
	tests := []struct {
		name   string
		params ProvisioningURIParams
	}{
		{"missing issuer", ProvisioningURIParams{Account: "alice@example.com", Secret: []byte(rfc6238SHA1Secret)}},
		{"missing account", ProvisioningURIParams{Issuer: "Eshu", Secret: []byte(rfc6238SHA1Secret)}},
		{"missing secret", ProvisioningURIParams{Issuer: "Eshu", Account: "alice@example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ProvisioningURI(tt.params); err == nil {
				t.Fatalf("ProvisioningURI(%+v) = nil error, want error", tt.params)
			}
		})
	}
}
