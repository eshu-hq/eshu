// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// deployableUnitCassetteRegressionFile mirrors the small slice of
// testdata/cassettes/deployableunit/ifa-deployable-unit-family.json this
// test needs -- fact_kind and payload only, one scope. Deliberately NOT the
// full go/internal/ifa cassette-loading struct: importing internal/ifa from
// here would be a cycle (internal/ifa imports internal/reducer), and this
// test needs none of that package's Odù/catalog machinery, only the raw
// committed facts.
type deployableUnitCassetteRegressionFile struct {
	Scopes []struct {
		ScopeID      string `json:"scope_id"`
		GenerationID string `json:"generation_id"`
		Facts        []struct {
			FactKind string         `json:"fact_kind"`
			Payload  map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// deployableUnitCassetteRegressionRepoRoot returns the repo root from this
// file's own location, same pattern as javaRouteFixtureRepoRoot
// (handles_route_java_test.go).
func deployableUnitCassetteRegressionRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file lives at <repoRoot>/go/internal/reducer/.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
}

// TestDeployableUnitCorrelationCommittedCassetteEntityKeySurvivesFilter is a
// targeted regression test for #5993's live defect, and the test the
// cassette/catalog lockstep test (TestDeployableUnitFamilyCassetteMatchesCompiledCatalog,
// go/internal/ifa) cannot be: reflect.DeepEqual only catches cassette-vs-catalog
// DRIFT, never an error baked into shared source both sides derive from the
// same (wrong) way. The #5993 defect was exactly that shape -- all four
// *LocalPath constants in deployable_unit_family_catalog.go were missing an
// "ifa-" segment, so both the compiled catalog AND the cassette regenerated
// from it carried the same corrupted entity_key, and the lockstep test
// passed throughout.
//
// This test reads the COMMITTED cassette JSON directly, off disk, exactly as
// production reads it, rather than deriving both sides from the same Go
// source the way the offline Ifá guard's own intent construction does
// (deployableUnitFamilyIntentFromOdu builds EntityKeys from the repository
// fact's name/graph_id -- correctly-spelled fields the #5993 bug never
// touched, so that path could never have caught this). It builds
// intent.EntityKeys the way production actually does in the live gate:
// go/internal/projector/runtime_reducer_intent.go reads a
// shared_followup fact's "entity_key" payload field verbatim
// (payloadString(fact.Payload, "entity_key")) -- reproduced here as a plain
// map read, not imported, since internal/projector is not a dependency this
// package needs elsewhere and importing it only for one field read would be
// a heavier coupling than the one-line read it replaces.
//
// If the cassette's shared_followup entity_key ever again names a repository
// that does not match the corresponding repository fact's repo_id/graph_id
// (the #5993 shape), filterDeployableUnitCandidates rejects every candidate
// and this test fails with a direct, actionable message -- instead of the
// failure only surfacing hours later as a live-gate zero-edges assertion
// with no attribution back to entity_key.
func TestDeployableUnitCorrelationCommittedCassetteEntityKeySurvivesFilter(t *testing.T) {
	t.Parallel()

	repoRoot := deployableUnitCassetteRegressionRepoRoot(t)
	cassettePath := filepath.Join(repoRoot, "testdata", "cassettes", "deployableunit", "ifa-deployable-unit-family.json")

	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		t.Fatalf("read committed cassette %s: %v", cassettePath, err)
	}
	var parsed deployableUnitCassetteRegressionFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse committed cassette %s: %v", cassettePath, err)
	}
	if len(parsed.Scopes) != 1 {
		t.Fatalf("cassette declares %d scopes, want exactly 1", len(parsed.Scopes))
	}
	scope := parsed.Scopes[0]

	var envelopes []facts.Envelope
	var appEntityKey string
	var sawAppFollowup bool
	for _, fact := range scope.Facts {
		switch fact.FactKind {
		case "repository", "file":
			envelopes = append(envelopes, facts.Envelope{
				ScopeID:      scope.ScopeID,
				GenerationID: scope.GenerationID,
				FactKind:     fact.FactKind,
				Payload:      fact.Payload,
			})
		case "shared_followup":
			if repoID, _ := fact.Payload["repo_id"].(string); repoID != "repo-ifa-deployable-unit-app" {
				continue
			}
			if domain, _ := fact.Payload["reducer_domain"].(string); domain != "deployable_unit_correlation" {
				continue
			}
			// Production's own read of this field: a plain payload map
			// lookup, no trimming or normalization at the source
			// (go/internal/projector/runtime_reducer_intent.go:24).
			key, ok := fact.Payload["entity_key"].(string)
			if !ok || key == "" {
				t.Fatalf("app repo's deployable_unit_correlation shared_followup fact has no entity_key payload field")
			}
			appEntityKey = key
			sawAppFollowup = true
		}
	}
	if !sawAppFollowup {
		t.Fatalf("cassette carries no deployable_unit_correlation shared_followup fact for repo-ifa-deployable-unit-app; fixture drifted")
	}

	candidates, _ := ExtractWorkloadCandidates(envelopes)
	if len(candidates) == 0 {
		t.Fatalf("ExtractWorkloadCandidates found zero candidates from the committed cassette's repository+file facts")
	}

	intent := Intent{
		IntentID:     "regression:deployable-unit-entity-key",
		ScopeID:      scope.ScopeID,
		GenerationID: scope.GenerationID,
		SourceSystem: "git",
		Domain:       DomainDeployableUnitCorrelation,
		EntityKeys:   []string{appEntityKey},
	}
	entityKeys, err := deployableUnitCorrelationEntityKeys(intent)
	if err != nil {
		t.Fatalf("deployableUnitCorrelationEntityKeys: %v", err)
	}

	filtered := filterDeployableUnitCandidates(candidates, entityKeys)
	if len(filtered) != 1 {
		t.Fatalf(
			"filterDeployableUnitCandidates(candidates, entityKeys-from-committed-cassette-entity_key=%q) = %d candidates, want exactly 1 (repo-ifa-deployable-unit-app) -- "+
				"the cassette's shared_followup entity_key does not match the repository fact's own repo_id/graph_id, the #5993 shape",
			appEntityKey, len(filtered),
		)
	}
	if got := filtered[0].RepoID; got != "repo-ifa-deployable-unit-app" {
		t.Fatalf("filterDeployableUnitCandidates survivor RepoID = %q, want repo-ifa-deployable-unit-app", got)
	}
}
