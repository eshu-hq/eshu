// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import "encoding/hex"

// isLegacyIDPGroupMappingRef recognizes the old 32-hex MD5 references so a
// delete cannot silently report a no-op after the FIPS-safe SHA-256 transition.
func isLegacyIDPGroupMappingRef(ref string) bool {
	if len(ref) != 32 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}
