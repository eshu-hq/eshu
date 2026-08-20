// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func loadCassetteEnvelopes(t *testing.T, path string) []facts.Envelope {
	t.Helper()
	out, err := LoadCassetteEnvelopes(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return out
}

// TestIFALiveMatrixGenerationIDsAreUniqueAcrossScopes pins the database
// contract behind ingestion_scopes_active_generation_idx: every non-null
// active generation ID is globally unique, even when different fixture scopes
// are driven into the same live stack.
func TestIFALiveMatrixGenerationIDsAreUniqueAcrossScopes(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	fixturePaths := []string{
		"testdata/cassettes/gcpcloud/supply-chain-demo.json",
		"testdata/cassettes/sqlrelationships/ifa-sql-family.json",
		"testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json",
		codeCallFamilyCassettePath,
		documentationFamilyCassettePath,
		RationaleFamilyCassetteRelPath,
		RationaleFamilyDeltaCassetteRelPath,
	}

	// A duplicate entry is the fail-open mode this list is prone to: two entries
	// naming the same cassette re-register one generation/scope pair, so the
	// family that was displaced drops out of the proof below while the test
	// still reports green. Caught here rather than trusted, because reading the
	// paths from consts does not prevent one const's VALUE from being pointed
	// at another's (#6053).
	seenPath := map[string]int{}
	for i, fixturePath := range fixturePaths {
		if prior, exists := seenPath[fixturePath]; exists {
			t.Fatalf("fixture list entries %d and %d both name %q; a duplicate silently drops the displaced family from this proof",
				prior, i, fixturePath)
		}
		seenPath[fixturePath] = i
	}

	generationScopes := map[string]string{}
	for _, fixturePath := range fixturePaths {
		envelopes := loadCassetteEnvelopes(t, filepath.Join(repoRoot, fixturePath))
		if len(envelopes) == 0 {
			t.Fatalf("live cassette %q contains no facts", fixturePath)
		}
		for _, envelope := range envelopes {
			if envelope.ScopeID == "" || envelope.GenerationID == "" {
				t.Fatalf("live cassette %q contains a blank scope/generation coordinate", fixturePath)
			}
			if priorScope, exists := generationScopes[envelope.GenerationID]; exists && priorScope != envelope.ScopeID {
				t.Fatalf("live generation ID %q belongs to both %q and %q; ingestion_scopes_active_generation_idx permits only one active scope",
					envelope.GenerationID, priorScope, envelope.ScopeID)
			}
			generationScopes[envelope.GenerationID] = envelope.ScopeID
		}
	}
}
