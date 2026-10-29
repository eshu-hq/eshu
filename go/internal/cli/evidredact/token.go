// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidredact

import "strings"

// RedactToken masks a Bearer [REDACTED] for display. It never returns the raw token.
// A short token is fully masked; a longer token keeps a 4-character prefix so an
// operator can recognize which key is configured without exposing it.
//
// This is the single owner of token masking on the internal/cli side. It moved
// here from internal/cli/mcpsetup (which keeps a deprecated alias) so the
// endpoint-redaction fallback in Endpoint no longer drags the mcp tool surface
// into every consumer of this package.
func RedactToken(token string) string {
	t := strings.TrimSpace(token)
	if t == "" {
		return ""
	}
	if len(t) <= 8 {
		return strings.Repeat("*", len(t))
	}
	return t[:4] + strings.Repeat("*", len(t)-4)
}
