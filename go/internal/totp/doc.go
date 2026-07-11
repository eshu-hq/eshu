// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package totp implements RFC 6238 Time-Based One-Time Passwords (TOTP) for
// Eshu's authenticator-app MFA factor (issue #4986), using only the Go
// standard library: crypto/hmac and crypto/sha1 for the RFC 4226 HOTP
// primitive, crypto/rand for secret generation, and crypto/subtle for
// constant-time code comparison.
//
// GenerateCode computes one HOTP value (RFC 4226 Section 5.3 dynamic
// truncation) over the RFC 6238 Section 4.2 time-step counter
// (floor(unixSeconds / step)), keyed by HMAC-SHA1. It is verified against
// the literal RFC 6238 Appendix B SHA1 test vectors in totp_test.go.
//
// Verify checks a submitted code against a window of +/- skewSteps time
// steps around now (RFC 6238 Section 5.2 recommends tolerating a small
// amount of clock drift), using crypto/subtle.ConstantTimeCompare so a
// timing side channel cannot narrow down which digit of a guessed code was
// wrong.
//
// GenerateSecret returns a fresh crypto/rand secret sized for HMAC-SHA1
// (20 bytes, matching the RFC 4226 Appendix C reference secret length).
// ProvisioningURI builds the otpauth:// URI an authenticator app QR code
// encodes; this package never renders a QR image itself — callers (the
// console) render the QR client-side from the URI text.
//
// This package holds no database, HTTP, or storage concerns and never
// receives an already-sealed secret: callers own sealing the generated
// secret at rest (see go/internal/secretcrypto) and only ever pass this
// package the opened plaintext for the single verification call.
package totp
