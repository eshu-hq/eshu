// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "testing"

// TestCanonicalNodeWriterTerraformStateResolversConfigured proves the #5443
// wiring-visibility contract: TerraformStateResolversConfigured must report
// each resolver's presence independently and must not panic on a nil
// receiver, since cmd/* wiring-level tests type-assert the constructed
// projector.CanonicalWriter and call this accessor to prove their canonical
// writer construction actually attached both MATCHES_STATE resolvers rather
// than silently leaving them nil (see WithTerraformStateOwnershipResolver's
// "no MATCHES_STATE edges written" degradation).
func TestCanonicalNodeWriterTerraformStateResolversConfigured(t *testing.T) {
	t.Parallel()

	t.Run("neither wired", func(t *testing.T) {
		t.Parallel()
		writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
		ownership, configMatch := writer.TerraformStateResolversConfigured()
		if ownership || configMatch {
			t.Fatalf("TerraformStateResolversConfigured() = (%v, %v), want (false, false)", ownership, configMatch)
		}
	})

	t.Run("both wired", func(t *testing.T) {
		t.Parallel()
		writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil).
			WithTerraformStateOwnershipResolver(newFakeTerraformStateOwnershipResolver()).
			WithTerraformStateConfigMatchResolver(&fakeTerraformStateConfigMatchResolver{})
		ownership, configMatch := writer.TerraformStateResolversConfigured()
		if !ownership || !configMatch {
			t.Fatalf("TerraformStateResolversConfigured() = (%v, %v), want (true, true)", ownership, configMatch)
		}
	})

	t.Run("nil receiver", func(t *testing.T) {
		t.Parallel()
		var writer *CanonicalNodeWriter
		ownership, configMatch := writer.TerraformStateResolversConfigured()
		if ownership || configMatch {
			t.Fatalf("TerraformStateResolversConfigured() on nil = (%v, %v), want (false, false)", ownership, configMatch)
		}
	})
}
