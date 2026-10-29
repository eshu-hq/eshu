// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import "strings"

// RuntimeVersionTokenCap bounds declarations retained from one version source.
// Readers count past the cap but must not emit a prefix as complete evidence.
const RuntimeVersionTokenCap = 1024

// SafeVersionSpec reports whether a declared version is short, printable
// ASCII from the version grammar and lacks a credential-like opaque run.
// This is the shared parser and fact-emitter guard for manifest values.
func SafeVersionSpec(raw string) bool {
	const maxBytes, maxOpaqueRun = 128, 16
	if raw == "" || len(raw) > maxBytes || strings.Contains(raw, "://") {
		return false
	}
	run := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
			run++
			if run >= maxOpaqueRun {
				return false
			}
		case strings.IndexByte(" .+*^~<>=|,-/:@${}", c) >= 0:
			run = 0
		default:
			return false
		}
	}
	return true
}
