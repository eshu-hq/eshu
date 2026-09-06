// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainmodel

import "testing"

// TestScopeGenerationKey pins ScopeGenerationKey to facts.Envelope's own
// "scope:generation" format. This forward must never drift from that format:
// OSPackage and ScannerAnalysis evidence is joined by this exact key.
func TestScopeGenerationKey(t *testing.T) {
	if got, want := ScopeGenerationKey("scope-123", "generation-456"), "scope-123:generation-456"; got != want {
		t.Fatalf("ScopeGenerationKey() = %q, want %q", got, want)
	}
}
