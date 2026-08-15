// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The codeowners_ownership_edges family Odù (#5992, under the #5543 umbrella).
//
// codeownersFamilyOdu in codeowners_family_catalog.go is the binary-portable
// compiled catalog representation. This file projects the committed cassette
// through the same strict envelope boundary for
// TestCodeownersFamilyIsCatalogedAndResolvable, which deeply compares the two
// representations so a one-sided edit fails the focused suite.
//
// Both live gate scripts drive this cassette. The offline extractor guard and
// the determinism/fault-injection matrices therefore assert the same committed
// bytes rather than maintaining parallel fixtures that can drift.
const (
	codeownersFamilyOduName      = "odu:ifa-codeowners-family"
	codeownersFamilyCassettePath = "testdata/cassettes/codeowners/ifa-codeowners-family.json"
	codeownersExpectedEdgesPath  = "go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json"
)

// codeownersFamilyCassetteFile mirrors the cassette envelope fields that
// decide whether a fact is accepted at all.
//
// schema_version is load-bearing and was originally dropped in the code_calls
// sibling's first draft (#5991). An empty version reads as "latest", so a
// cassette carrying an unsupported major would sail through this projection
// and satisfy the offline guard while live replay preserved the version and
// quarantined the fact — the extractor guard would then certify input the
// live gate rejects. stable_fact_key, collector_kind and source_confidence
// ride along for the same reason: this projection must never be more
// permissive than production.
type codeownersFamilyCassetteFile struct {
	Scopes []struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		Facts        []struct {
			FactKind         string         `json:"fact_kind"`
			SchemaVersion    string         `json:"schema_version"`
			StableFactKey    string         `json:"stable_fact_key"`
			CollectorKind    string         `json:"collector_kind"`
			SourceConfidence string         `json:"source_confidence"`
			IsTombstone      bool           `json:"is_tombstone"`
			Payload          map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// codeownersFamilyCassetteFullPath joins repoRoot onto the cassette path.
func codeownersFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeownersFamilyCassettePath)
}

// codeownersFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture.
//
// It lives under go/internal/ifa/testdata/ rather than testdata/cassettes/
// for the same reason the SQL, code_calls, documentation and rationale
// families' fixtures do: the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette, and this file is a gate
// ASSERTION, not a cassette.
func codeownersFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeownersExpectedEdgesPath)
}

// loadCodeownersFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes.
//
// Unexported because it is the test-side lockstep loader for the committed
// cassette. Production registers the compiled codeownersFamilyOdu in
// catalogSeed; TestCodeownersFamilyIsCatalogedAndResolvable compares that
// registered Odù with this strict cassette projection and exercises the
// codeowners_ownership_edges resolver guard.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous, which is the failure mode
// the whole #5543 exhaustiveness effort exists to remove.
func loadCodeownersFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read codeowners cassette %s: %w", cassettePath, err)
	}
	var parsed codeownersFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse codeowners cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: codeowners cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: codeowners cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
	}

	envelopes := make([]facts.Envelope, 0, len(scope.Facts))
	for _, fact := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          scope.ScopeID,
			GenerationID:     scope.GenerationID,
			FactKind:         fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableFactKey:    fact.StableFactKey,
			CollectorKind:    fact.CollectorKind,
			SourceConfidence: fact.SourceConfidence,
			IsTombstone:      fact.IsTombstone,
			Payload:          fact.Payload,
		})
	}
	return Odu{Name: codeownersFamilyOduName, Facts: envelopes}, nil
}
