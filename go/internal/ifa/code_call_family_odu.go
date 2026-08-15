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

// The code_calls family Odù (#5991, under the #5543 umbrella).
//
// codeCallFamilyOdu in code_call_family_catalog.go is the binary-portable
// compiled catalog representation. This file projects the committed cassette
// through the same strict envelope boundary for
// TestCodeCallFamilyIsCatalogedAndResolvable, which deeply compares the two
// representations so a one-sided edit fails the focused suite.
//
// Both live gate scripts drive this cassette. The offline extractor guard and
// the determinism/fault-injection matrices therefore assert the same committed
// bytes rather than maintaining parallel fixtures that can drift.
const (
	codeCallFamilyOduName      = "odu:ifa-code-call-family"
	codeCallFamilyCassettePath = "testdata/cassettes/codecalls/ifa-code-call-family.json"
	codeCallExpectedEdgesPath  = "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"
)

// codeCallFamilyCassetteFile mirrors the cassette envelope fields that decide
// whether a fact is accepted at all.
//
// schema_version is load-bearing and was originally dropped here. An empty
// version reads as "latest", so a cassette carrying an unsupported major would
// sail through this projection and satisfy the offline guard while live replay
// preserved the version and quarantined the fact — the extractor guard would
// then certify input the live gate rejects. stable_fact_key, collector_kind and
// source_confidence ride along for the same reason: this projection must never
// be more permissive than production.
type codeCallFamilyCassetteFile struct {
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

// codeCallFamilyCassetteFullPath joins repoRoot onto the cassette path.
func codeCallFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeCallFamilyCassettePath)
}

// codeCallFamilyExpectedEdgesPath joins repoRoot onto the expected-edge fixture.
//
// It lives under go/internal/ifa/testdata/ rather than testdata/cassettes/ for
// the same reason the SQL, documentation and rationale families' fixtures do:
// the offline cassette validator globs every testdata/cassettes/*/*.json as a
// replay cassette, and this file is a gate ASSERTION, not a cassette.
func codeCallFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeCallExpectedEdgesPath)
}

// loadCodeCallFamilyOdu reads the committed cassette and projects it onto the
// fact envelopes the reducer's extractor consumes.
//
// Unexported because it is the test-side lockstep loader for the committed
// cassette. Production registers the compiled codeCallFamilyOdu in catalogSeed;
// TestCodeCallFamilyIsCatalogedAndResolvable compares that registered Odù with
// this strict cassette projection and exercises the code_calls resolver guard.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts would
// make every downstream assertion vacuous, which is the failure mode the whole
// #5543 exhaustiveness effort exists to remove.
func loadCodeCallFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read code-call cassette %s: %w", cassettePath, err)
	}
	var parsed codeCallFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse code-call cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: code-call cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: code-call cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: codeCallFamilyOduName, Facts: envelopes}, nil
}
