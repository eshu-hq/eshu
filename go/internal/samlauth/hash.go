// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package samlauth

import (
	"crypto/sha256"
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
