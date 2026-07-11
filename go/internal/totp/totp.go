// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package totp

import (
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- RFC 6238/4226 mandate HMAC-SHA1 for TOTP/HOTP; this is the algorithm, not a weak hash used for integrity of arbitrary data.
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultDigits is the production TOTP code length (approved design:
	// 6 digits, matching every mainstream authenticator app).
	DefaultDigits = 6
	// DefaultStep is the production TOTP time step (approved design: 30s,
	// the RFC 6238 recommended default).
	DefaultStep = 30 * time.Second
	// DefaultSkewSteps is the production clock-skew tolerance: one step
	// before and after the current step (approved design: +/-1 step).
	DefaultSkewSteps = 1

	minDigits = 6
	maxDigits = 8
)

// digitModulus maps a supported digit count to its truncation modulus
// (10^digits), avoiding a floating-point math.Pow10 round-trip for a value
// that only ever takes three possible inputs.
var digitModulus = map[int]uint32{
	6: 1_000_000,
	7: 10_000_000,
	8: 100_000_000,
}

// GenerateCode computes the RFC 6238 TOTP code for secret at time t, using
// the given step duration and digit count. It implements RFC 4226 Section
// 5.3 HOTP dynamic truncation over the RFC 6238 Section 4.2 time-step
// counter (floor(t.Unix() / step.Seconds())), using HMAC-SHA1 per the RFC
// 6238 default and only supported mode in this package.
//
// digits must be 6, 7, or 8 (RFC 4226 Section 5.3 requires at least 6;
// this package caps at 8, the widest value any RFC 6238 vector or common
// authenticator app uses). Verified against the RFC 6238 Appendix B SHA1
// test vectors in totp_test.go.
func GenerateCode(secret []byte, t time.Time, step time.Duration, digits int) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("totp: secret must not be empty")
	}
	if step <= 0 {
		return "", errors.New("totp: step must be positive")
	}
	if _, ok := digitModulus[digits]; !ok {
		return "", fmt.Errorf("totp: digits must be %d-%d, got %d", minDigits, maxDigits, digits)
	}
	// #nosec G115 -- t.Unix() is a positive wall-clock timestamp and step is
	// validated positive above, so the RFC 6238 time-step counter is
	// non-negative for any real authenticator time; a pre-1970 t is not a
	// valid TOTP input, and Verify separately skips any negative candidate step.
	counter := uint64(t.Unix() / int64(step.Seconds()))
	return hotp(secret, counter, digits)
}

// hotp computes the RFC 4226 HOTP value for one counter value. digits is
// assumed already validated by the caller (GenerateCode / Verify).
func hotp(secret []byte, counter uint64, digits int) (string, error) {
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)

	mac := hmac.New(sha1.New, secret)
	mac.Write(counterBytes[:])
	sum := mac.Sum(nil)

	// RFC 4226 Section 5.3 dynamic truncation (DT): the low nibble of the
	// last byte selects a 4-byte offset into the 20-byte HMAC-SHA1 digest;
	// masking the high bit of that 4-byte window avoids sign ambiguity
	// across platforms with differing int representations.
	offset := sum[len(sum)-1] & 0x0f
	binCode := (uint32(sum[offset])&0x7f)<<24 |
		(uint32(sum[offset+1])&0xff)<<16 |
		(uint32(sum[offset+2])&0xff)<<8 |
		(uint32(sum[offset+3]) & 0xff)

	code := binCode % digitModulus[digits]
	return fmt.Sprintf("%0*d", digits, code), nil
}

// Verify reports whether code matches secret at time t within +/- skewSteps
// time steps (RFC 6238 Section 5.2 recommends tolerating a small amount of
// clock drift between the authenticator app and the server). It checks the
// current step first, then increasingly distant steps, and compares every
// candidate with crypto/subtle.ConstantTimeCompare so no timing signal
// leaks which candidate (if any) matched.
//
// An empty code always returns false, nil (never an error) so callers can
// treat "no code submitted" identically to "wrong code" without a type
// switch. skewSteps must be non-negative.
func Verify(secret []byte, code string, t time.Time, step time.Duration, digits int, skewSteps int) (bool, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return false, nil
	}
	if skewSteps < 0 {
		return false, errors.New("totp: skewSteps must not be negative")
	}
	if step <= 0 {
		return false, errors.New("totp: step must be positive")
	}
	if _, ok := digitModulus[digits]; !ok {
		return false, fmt.Errorf("totp: digits must be %d-%d, got %d", minDigits, maxDigits, digits)
	}

	counter := t.Unix() / int64(step.Seconds())
	codeBytes := []byte(code)
	for delta := -skewSteps; delta <= skewSteps; delta++ {
		c := counter + int64(delta)
		if c < 0 {
			continue
		}
		want, err := hotp(secret, uint64(c), digits)
		if err != nil {
			return false, err
		}
		if subtle.ConstantTimeCompare([]byte(want), codeBytes) == 1 {
			return true, nil
		}
	}
	return false, nil
}
