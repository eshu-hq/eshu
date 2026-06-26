// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

func stableHash(parts ...string) string {
	sum := sha256.New()
	for _, part := range parts {
		_, _ = sum.Write([]byte(part))
		_, _ = sum.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

// constantTimeHashEqual compares two hash strings in constant time. It is safe
// for comparing replay fingerprints and request ID hashes where a timing side
// channel could leak which bytes matched.
func constantTimeHashEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
