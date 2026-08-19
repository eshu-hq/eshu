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

// repoDependencyFamilyCassettePath is where the family's committed cassette
// lives. Mirrors deployableUnitFamilyCassettePath's role: a future live gate
// replays this same committed cassette, and this file's loader plus a
// lockstep test prove it never drifts from the compiled catalog Odù
// (repo_dependency_family_catalog.go) rather than maintaining two fixtures
// that can silently diverge.
const repoDependencyFamilyCassettePath = "testdata/cassettes/repodependency/ifa-repo-dependency-family.json"

// repoDependencyFamilyCassetteFile mirrors deployableUnitFamilyCassetteFile's
// field set for the same reason: schema_version, stable_fact_key,
// collector_kind, and source_confidence all gate whether a fact is accepted
// at all, so this projection must never be more permissive than production.
type repoDependencyFamilyCassetteFile struct {
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

// repoDependencyFamilyCassetteFullPath joins repoRoot onto the cassette path.
func repoDependencyFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, repoDependencyFamilyCassettePath)
}

// loadRepoDependencyFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes relationships.DiscoverEvidence consumes.
//
// Unexported: it is the test-side lockstep loader for the committed
// cassette. Production registers the compiled repoDependencyFamilyOdu() in
// catalogSeed; a lockstep test compares that registered Odù with this strict
// cassette projection so a one-sided edit fails the focused suite.
//
// It fails closed on more than one scope or an empty fact list, mirroring
// loadDeployableUnitFamilyOdu: a multi-scope fixture would make the
// expected-edge set ambiguous about which scope produced an edge, and an
// empty Odù would make every downstream assertion vacuous.
func loadRepoDependencyFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read repo-dependency cassette %s: %w", cassettePath, err)
	}
	var parsed repoDependencyFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse repo-dependency cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: repo-dependency cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: repo-dependency cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: repoDependencyFamilyOduName, Facts: envelopes}, nil
}
