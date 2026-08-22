// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// symbolRuntimeFamilyCassetteFile mirrors the cassette envelope fields that
// decide whether a fact is accepted at all, matching
// deployableUnitFamilyCassetteFile's projection (#5991 precedent). Split into
// its own file (alongside LoadSymbolRuntimeFamilyOdu) so
// symbol_runtime_family_odu.go stays well clear of the 500-line Go file cap.
type symbolRuntimeFamilyCassetteFile struct {
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

// LoadSymbolRuntimeFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extraction seams consume. It is the
// test-side lockstep loader for the committed cassette: a lockstep test in
// materializededges compares this strict cassette projection with the
// compiled symbolRuntimeFamilyOdu() so a one-sided edit fails the focused
// suite.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous.
func LoadSymbolRuntimeFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read symbol-runtime cassette %s: %w", cassettePath, err)
	}
	var parsed symbolRuntimeFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse symbol-runtime cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: symbol-runtime cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: symbol-runtime cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: SymbolRuntimeFamilyOduName, Facts: envelopes}, nil
}
