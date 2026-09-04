// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/rationale"
)

const (
	rationaleFamilyDeltaExpectedRelPath = "go/internal/ifa/testdata/rationale/ifa-rationale-family-delta-live-expected-records.json"
)

type rationaleDeltaExpectedFixture struct {
	GenerationID  string                  `json:"generation_id"`
	Edges         []rationaleExpectedEdge `json:"edges"`
	RequiredNodes []rationaleDeltaNode    `json:"required_nodes"`
}

type rationaleDeltaNode struct {
	Labels []string       `json:"labels"`
	Props  map[string]any `json:"props"`
}

func TestRationaleDeltaExpectedFixtureIsLiveReadable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(repoRootDir(t), rationaleFamilyDeltaExpectedRelPath)
	repoID, records, err := LoadRationaleExpectedEdgeRecords(path)
	if err != nil {
		t.Fatalf("LoadRationaleExpectedEdgeRecords(delta): %v", err)
	}
	if repoID != ifa.RationaleFamilyRepoID || len(records) != 1 {
		t.Fatalf("live-readable rationale delta fixture = repo:%q edges:%d, want %q/1",
			repoID, len(records), ifa.RationaleFamilyRepoID)
	}
}

func TestRationaleDeltaCassettePinsFourFactRefreshAndSurvivors(t *testing.T) {
	t.Parallel()
	envelopes := loadCassetteEnvelopes(t, filepath.Join(repoRootDir(t), ifa.RationaleFamilyDeltaCassetteRelPath))
	if len(envelopes) != 4 {
		t.Fatalf("rationale delta cassette facts = %d, want exact 4", len(envelopes))
	}
	wantKinds := []string{"repository", "file", "content_entity", "shared_followup"}
	for index, want := range wantKinds {
		if got := envelopes[index].FactKind; got != want {
			t.Errorf("rationale delta fact %d kind = %q, want %q", index, got, want)
		}
		if envelopes[index].GenerationID != ifa.RationaleFamilyDeltaGenerationID {
			t.Errorf("rationale delta fact %d generation = %q, want %q", index, envelopes[index].GenerationID, ifa.RationaleFamilyDeltaGenerationID)
		}
	}

	stage := projector.ProjectWorkloadStage(envelopes)
	if got := stage.SourceRunPairs[ifa.RationaleFamilyRepoID]; got != ifa.RationaleFamilySourceRunID {
		t.Fatalf("rationale delta repository source run = %q, want %q", got, ifa.RationaleFamilySourceRunID)
	}
	if len(stage.Intents) != 1 || stage.Intents[0].Domain != reducer.DomainRationaleMaterialization {
		t.Fatalf("rationale delta stage intents = %#v, want one rationale materialization intent", stage.Intents)
	}
	if intent := stage.Intents[0]; intent.ScopeID != ifa.RationaleFamilyScopeID || intent.GenerationID != ifa.RationaleFamilyDeltaGenerationID {
		t.Fatalf("rationale delta intent scope/generation = %q/%q, want %q/%q",
			intent.ScopeID, intent.GenerationID, ifa.RationaleFamilyScopeID, ifa.RationaleFamilyDeltaGenerationID)
	}

	_, rows := rationale.ExtractRows(envelopes)
	if len(rows) != 0 {
		t.Fatalf("rationale delta extracted edge rows = %d, want 0", len(rows))
	}

	expected := loadRationaleDeltaExpectedFixture(t)
	if expected.GenerationID != ifa.RationaleFamilyDeltaGenerationID || len(expected.Edges) != 1 || len(expected.RequiredNodes) != 1 {
		t.Fatalf("rationale delta expected inventory = generation:%q edges:%d nodes:%d, want %s/1/1",
			expected.GenerationID, len(expected.Edges), len(expected.RequiredNodes), ifa.RationaleFamilyDeltaGenerationID)
	}
	if got := expected.Edges[0].TargetEntityID; got != "content-entity:e_2dc98238d686" {
		t.Fatalf("rationale delta survivor target = %q, want unchanged invoice", got)
	}
	gen1Edges, err := loadRationaleExpectedEdges(rationaleFamilyExpectedEdgesPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("load baseline rationale expected edges: %v", err)
	}
	var invoiceRecord rationaleExpectedEdge
	for _, edge := range gen1Edges {
		if edge.TargetEntityID == "content-entity:e_2dc98238d686" {
			invoiceRecord = edge
		}
	}
	if !reflect.DeepEqual(expected.Edges[0], invoiceRecord) {
		t.Fatalf("rationale delta survivor drifted from unchanged baseline invoice record\ndelta: %#v\nbaseline: %#v",
			expected.Edges[0], invoiceRecord)
	}

	entities := projector.ExtractEntityRows(envelopes, ifa.RationaleFamilyRepoID, ifa.RationaleFamilyLocalPath)
	if len(entities) != 1 {
		t.Fatalf("rationale delta canonical entities = %d, want changed charge only", len(entities))
	}
	gotNode := rationaleDeltaNodeFromEntity(entities[0])
	gotNodeJSON, err := json.Marshal(gotNode)
	if err != nil {
		t.Fatalf("encode projected rationale delta node: %v", err)
	}
	wantNodeJSON, err := json.Marshal(expected.RequiredNodes[0])
	if err != nil {
		t.Fatalf("encode expected rationale delta node: %v", err)
	}
	if string(gotNodeJSON) != string(wantNodeJSON) {
		t.Fatalf("rationale delta charge node = %s, want %s", gotNodeJSON, wantNodeJSON)
	}
}

func loadRationaleDeltaExpectedFixture(t *testing.T) rationaleDeltaExpectedFixture {
	t.Helper()
	path := filepath.Join(repoRootDir(t), rationaleFamilyDeltaExpectedRelPath)
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed checked-in test fixture.
	if err != nil {
		t.Fatalf("read rationale delta expected fixture: %v", err)
	}
	var fixture rationaleDeltaExpectedFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode rationale delta expected fixture: %v", err)
	}
	return fixture
}

func rationaleDeltaNodeFromEntity(entity projector.EntityRow) rationaleDeltaNode {
	return rationaleDeltaNode{
		Labels: []string{entity.Label},
		Props: map[string]any{
			"uid": entity.EntityID, "id": entity.EntityID,
			"name": entity.EntityName, "path": entity.FilePath,
			"relative_path": entity.RelativePath,
			"line_number":   entity.StartLine, "start_line": entity.StartLine,
			"end_line": entity.EndLine, "repo_id": entity.RepoID,
			"language": entity.Language, "lang": entity.Language,
			"scope_id": ifa.RationaleFamilyScopeID, "generation_id": ifa.RationaleFamilyDeltaGenerationID,
			"evidence_source":       "projector/canonical",
			"async":                 false,
			"cyclomatic_complexity": entity.CyclomaticComplexity,
		},
	}
}
