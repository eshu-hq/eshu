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

// deployableUnitFamilyCassettePath is where the family's committed v1
// cassette lives. Both live gate scripts drive this cassette; the offline
// vacuity guard and the determinism/fault-injection matrices therefore
// assert the same committed bytes rather than maintaining parallel fixtures
// that can drift.
const deployableUnitFamilyCassettePath = "testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"

// deployableUnitFamilyCassetteFile mirrors the cassette envelope fields that
// decide whether a fact is accepted at all, the same subset
// codeCallFamilyCassetteFile projects (#5991 precedent): schema_version is
// load-bearing (an empty version reads as "latest" and would hide an
// unsupported major), and stable_fact_key/collector_kind/source_confidence
// ride along for the same reason -- this projection must never be more
// permissive than production.
type deployableUnitFamilyCassetteFile struct {
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

// DeployableUnitFamilyCassetteFullPath joins repoRoot onto the cassette path.
// Exported (#6163) so materializededges' moved deployable-unit-family tests
// can locate the same committed cassette LoadDeployableUnitFamilyOdu reads.
func DeployableUnitFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, deployableUnitFamilyCassettePath)
}

// LoadDeployableUnitFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the reducer's extraction seam consumes.
//
// It is the test-side lockstep loader for the committed cassette. Production
// registers the compiled deployableUnitFamilyOdu() in catalogSeed; a lockstep
// test in materializededges (#6163, moved with the rest of the
// deployable_unit_edges guard) compares that registered Odù with this strict
// cassette projection so a one-sided edit fails the focused suite. Exported so
// that moved test can reach it across the package boundary.
//
// It fails closed on an empty scope or fact list: an Odù carrying no facts
// would make every downstream assertion vacuous, which is the failure mode
// the whole #5543 exhaustiveness effort exists to remove.
func LoadDeployableUnitFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read deployable-unit cassette %s: %w", cassettePath, err)
	}
	var parsed deployableUnitFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse deployable-unit cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		return Odu{}, fmt.Errorf("ifa: deployable-unit cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge", cassettePath, len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return Odu{}, fmt.Errorf("ifa: deployable-unit cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
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
	return Odu{Name: deployableUnitFamilyOduName, Facts: envelopes}, nil
}
