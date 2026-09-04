// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/rationale"
)

const (
	rationaleDeltaFixtureRepoID   = "repository:r_f781caa5"
	rationaleDeltaFixtureRepoPath = "/repo-rationale"
)

func TestRationaleDeltaCassetteEmitsRefreshAndConvergesExactThreeToOne(t *testing.T) {
	t.Parallel()
	repoRoot := reducerRepoRoot(t)
	full := loadRationaleReducerCassette(t, filepath.Join(repoRoot, "testdata/cassettes/rationale/ifa-rationale-family.json"))
	delta := loadRationaleReducerCassette(t, filepath.Join(repoRoot, "testdata/cassettes/rationale/ifa-rationale-family-delta.json"))
	if len(delta) != 4 {
		t.Fatalf("rationale delta facts = %d, want exact 4", len(delta))
	}

	_, fullRows := rationale.ExtractRows(full)
	if len(fullRows) != 3 {
		t.Fatalf("rationale gen-1 edge rows = %d, want exact 3", len(fullRows))
	}
	edges := seedPriorRationaleEdges(fullRows, map[string]string{
		rationaleDeltaFixtureRepoID: rationaleDeltaFixtureRepoPath,
	})

	writer := &recordingRationaleIntentWriter{}
	handler := rationale.MaterializationHandler{
		FactLoader:   &stubFactLoader{envelopes: delta},
		IntentWriter: writer,
	}
	now := time.Date(2026, time.August, 15, 1, 0, 0, 0, time.UTC)
	result, err := handler.Handle(context.Background(), Intent{
		IntentID: "intent-rationale-delta", ScopeID: "scope-ifa-rationale-family",
		GenerationID: "gen-2", SourceSystem: "git", Domain: DomainRationaleMaterialization,
		EnqueuedAt: now, AvailableAt: now, Status: IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := len(writer.edgeRows()); got != 0 {
		t.Fatalf("rationale delta edge intents = %d, want 0", got)
	}
	refreshes := writer.refreshRows()
	if len(refreshes) != 1 || result.CanonicalWrites != 1 {
		t.Fatalf("rationale delta refreshes/writes = %d/%d, want 1/1", len(refreshes), result.CanonicalWrites)
	}
	if got, want := refreshes[0].Payload["delta_file_paths"], []string{"/repo-rationale/services/payments/charge.py"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rationale delta refresh paths = %#v, want %#v", got, want)
	}
	if err := edges.RetractEdges(context.Background(), DomainRationaleEdges, refreshes, rationale.EvidenceSource); err != nil {
		t.Fatalf("apply rationale delta refresh: %v", err)
	}
	wantSurvivor := "rationale:content-entity:e_2dc98238d686:HACK:06749b8de60ce629->content-entity:e_2dc98238d686"
	if got, want := edges.edgeKeys(rationaleDeltaFixtureRepoID), []string{wantSurvivor}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rationale edge state after delta = %#v, want exact invoice survivor %#v", got, want)
	}
}

func loadRationaleReducerCassette(t *testing.T, path string) []facts.Envelope {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed checked-in test fixture.
	if err != nil {
		t.Fatalf("read rationale cassette %s: %v", path, err)
	}
	var document struct {
		Scopes []struct {
			ScopeID      string    `json:"scope_id"`
			GenerationID string    `json:"generation_id"`
			ObservedAt   time.Time `json:"observed_at"`
			Facts        []struct {
				FactKind         string         `json:"fact_kind"`
				StableFactKey    string         `json:"stable_fact_key"`
				SchemaVersion    string         `json:"schema_version"`
				CollectorKind    string         `json:"collector_kind"`
				SourceConfidence string         `json:"source_confidence"`
				Payload          map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode rationale cassette %s: %v", path, err)
	}
	if len(document.Scopes) != 1 {
		t.Fatalf("rationale cassette %s scopes = %d, want 1", path, len(document.Scopes))
	}
	scope := document.Scopes[0]
	var envelopes []facts.Envelope
	for _, fact := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID: scope.ScopeID, GenerationID: scope.GenerationID,
			ObservedAt: scope.ObservedAt, FactKind: fact.FactKind,
			StableFactKey: fact.StableFactKey, SchemaVersion: fact.SchemaVersion,
			CollectorKind: fact.CollectorKind, SourceConfidence: fact.SourceConfidence,
			Payload: fact.Payload,
		})
	}
	return envelopes
}

func reducerRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}
