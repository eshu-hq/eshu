// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"encoding/hex"
	"strings"
)

// isLegacyIDPGroupMappingRef recognizes the old 32-hex MD5 references so a
// delete cannot silently report a no-op after the FIPS-safe SHA-256 transition.
func isLegacyIDPGroupMappingRef(ref string) bool {
	if len(ref) != 32 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

// isCurrentIDPGroupMappingRef accepts a canonical SHA-256 mapping reference for
// keyset pagination. The same form is returned by the list and create routes.
func isCurrentIDPGroupMappingRef(ref string) bool {
	if len(ref) != 64 || strings.ToLower(ref) != ref {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}
