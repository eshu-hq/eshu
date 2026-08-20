// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// rationaleFamilyDeltaCassetteRelPath duplicates the rationale-family delta
// cassette path from materializededges' rationale_family_delta_live_fixture_test.go
// (#6053): this test spans every family's live cassette (including the
// unrelated gcpcloud fixture above) to prove a cross-family database
// constraint, so it cannot move into materializededges with any single
// family's guard, and an in-package ifa test cannot import a package that
// imports ifa's production code plus additionally be imported the other way
// -- so the pure literal is copied here rather than exported for one test.
const rationaleFamilyDeltaCassetteRelPath = "testdata/cassettes/rationale/ifa-rationale-family-delta.json"

// loadCassetteEnvelopes duplicates materializededges'
// sql_relationship_odu_cassette_test.go helper of the same name (#6053): a
// generic cassette-to-envelope reader with no family-specific knowledge, for
// the same cross-family reason rationaleFamilyDeltaCassetteRelPath above is
// duplicated rather than exported.
func loadCassetteEnvelopes(t *testing.T, path string) []facts.Envelope {
	t.Helper()
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource(%s): %v", path, err)
	}
	var out []facts.Envelope
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatalf("cassette Next: %v", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			out = append(out, env)
		}
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
		"testdata/cassettes/codecalls/ifa-code-call-family.json",
		"testdata/cassettes/documentation/ifa-documentation-family.json",
		RationaleFamilyCassetteRelPath,
		rationaleFamilyDeltaCassetteRelPath,
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
