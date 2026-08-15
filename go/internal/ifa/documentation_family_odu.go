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

// documentationFamilyCassetteFile mirrors the cassette envelope fields that
// decide whether a fact is accepted at all, the same fields
// codeCallFamilyCassetteFile mirrors and for the same reason: this projection
// must never be more permissive than production. schema_version is
// load-bearing (an empty version reads as "latest" and would silently sail an
// unsupported-major fact through this projection while live replay
// quarantines it).
type documentationFamilyCassetteFile struct {
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

// documentationFamilyCassetteFullPath joins repoRoot onto the cassette path.
func documentationFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, documentationFamilyCassettePath)
}

// loadDocumentationFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, mirroring
// loadCodeCallFamilyOdu exactly.
//
// Unexported because it is the test-side lockstep loader for the committed
// cassette. Production registers the compiled documentationFamilyOdu in
// catalogSeed; TestDocumentationFamilyIsCatalogedAndResolvable compares that
// registered Odù with this strict cassette projection and exercises the
// documentation_edges resolver guard.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous, which is the failure mode
// the whole #5543 exhaustiveness effort exists to remove.
func loadDocumentationFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read documentation cassette %s: %w", cassettePath, err)
	}
	var parsed documentationFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse documentation cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: documentation cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: documentation cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: documentationFamilyOduName, Facts: envelopes}, nil
}
