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
// Unlike sqlFamilyOdu, which hand-builds its envelopes in Go while a separate
// committed cassette drives the live gate, this family derives its Odù FROM the
// committed cassette. The two artifacts then cannot drift: the offline vacuity
// guard and the live `ifa drive` assert the same bytes. A hand-built twin is a
// second source of truth that agrees on the day it is written and silently stops
// agreeing the first time only one side is edited.
const (
	codeCallFamilyOduName      = "odu:ifa-code-call-family"
	codeCallFamilyCassettePath = "testdata/cassettes/codecalls/ifa-code-call-family.json"
	codeCallExpectedEdgesPath  = "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"
)

// codeCallFamilyCassetteFile is the subset of the cassette envelope this loader
// reads. Fields the reducer does not consult are deliberately absent so a
// cassette-format addition does not silently change what the Odù carries.
type codeCallFamilyCassetteFile struct {
	Scopes []struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		Facts        []struct {
			FactKind    string         `json:"fact_kind"`
			IsTombstone bool           `json:"is_tombstone"`
			Payload     map[string]any `json:"payload"`
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
// Unexported, mirroring sqlFamilyOdu: the live gate reaches a family through the
// in-package catalog, never by importing its loader, so exporting this would
// widen the package surface for no consumer.
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
			ScopeID:      scope.ScopeID,
			GenerationID: scope.GenerationID,
			FactKind:     fact.FactKind,
			IsTombstone:  fact.IsTombstone,
			Payload:      fact.Payload,
		})
	}
	return Odu{Name: codeCallFamilyOduName, Facts: envelopes}, nil
}
