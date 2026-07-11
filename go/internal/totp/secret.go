// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package totp

import (
	"crypto/rand"
	"fmt"
)

// secretLengthBytes is the generated shared-secret length: 20 bytes (160
// bits), matching the RFC 4226 Appendix C reference HOTP secret length and
// the block size of the HMAC-SHA1 primitive this package uses.
const secretLengthBytes = 20

// GenerateSecret returns a fresh, cryptographically random shared secret
// sized for HMAC-SHA1 TOTP (secretLengthBytes bytes). Callers seal the
// returned bytes at rest before persisting (see go/internal/secretcrypto)
// and open it again only inside the single verification call; this package
// never persists or transmits the secret itself.
func GenerateSecret() ([]byte, error) {
	secret := make([]byte, secretLengthBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("totp: generate secret: %w", err)
	}
	return secret, nil
}
