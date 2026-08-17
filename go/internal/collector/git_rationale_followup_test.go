// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildStreamingGenerationEmitsRationaleFollowupForFullAndDelta(t *testing.T) {
	t.Parallel()

	for _, delta := range []bool{false, true} {
		delta := delta
		t.Run(map[bool]string{false: "full", true: "delta"}[delta], func(t *testing.T) {
			t.Parallel()
			repoPath := t.TempDir()
			repo := testCollectorRepositoryMetadata(repoPath)
			snapshot := testCollectorSnapshot(repoPath, "# WHY: bounded retry\ndef handler():\n    return 1\n", "digest-rationale")
			snapshot.Delta = delta
			snapshot.ContentEntities[0].Metadata = map[string]any{
				"rationale_comments": []any{
					map[string]any{"kind": "WHY", "text": "bounded retry"},
				},
			}

			collected := buildStreamingGeneration(repoPath, repo, "run-rationale", time.Now().UTC(), snapshot, false, "")
			assertSingleRationaleFollowup(t, drainFactChannel(collected.Facts), repoPath, repo.ID, collected.Scope.ScopeID, collected.Generation.GenerationID)
		})
	}
}

func TestRationaleFollowupHelperLivesInFollowupModule(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("git_followup_facts.go")
	if err != nil {
		t.Fatalf("read git_followup_facts.go: %v", err)
	}
	if !strings.Contains(string(raw), "func rationaleMaterializationFactEnvelope(") {
		t.Fatal("git_followup_facts.go does not own rationaleMaterializationFactEnvelope")
	}
	if _, err := os.Stat("git_rationale_followup.go"); !os.IsNotExist(err) {
		t.Fatalf("standalone git_rationale_followup.go still exists: err=%v", err)
	}
}

func TestBuildStreamingGenerationReconcilesRationaleWhenNoPositiveRemains(t *testing.T) {
	t.Parallel()
	for _, delta := range []bool{false, true} {
		delta := delta
		t.Run(map[bool]string{false: "full", true: "delta"}[delta], func(t *testing.T) {
			t.Parallel()
			repoPath := t.TempDir()
			repo := testCollectorRepositoryMetadata(repoPath)
			snapshot := testCollectorSnapshot(repoPath, "def handler():\n    return 1\n", "digest-no-rationale")
			snapshot.Delta = delta
			snapshot.ContentEntities = nil
			if delta {
				snapshot.FileCount = 0
				snapshot.FileData = nil
				snapshot.ContentFiles = nil
				snapshot.DeletedRelativePaths = []string{"app.py"}
			}

			collected := buildStreamingGeneration(repoPath, repo, "run-no-rationale", time.Now().UTC(), snapshot, false, "")
			assertSingleRationaleFollowup(t, drainFactChannel(collected.Facts), repoPath, repo.ID, collected.Scope.ScopeID, collected.Generation.GenerationID)
		})
	}
}

func TestBuildStreamingGenerationDeltaEstimateAccountsForRationaleMarker(t *testing.T) {
	t.Parallel()
	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	collected := buildStreamingGeneration(repoPath, repo, "run-minimal-delta", time.Now().UTC(), RepositorySnapshot{Delta: true}, false, "")
	if got, want := collected.EstimatedFactCount, 3; got != want {
		t.Fatalf("minimal delta EstimatedFactCount = %d, want conservative floor %d (repository + shell baseline + rationale)", got, want)
	}
	envelopes := drainFactChannel(collected.Facts)
	if got, want := len(envelopes), 5; got != want {
		t.Fatalf("minimal delta emitted facts = %d, want exact %d", got, want)
	}
	if got, want := collected.FactCount(), len(envelopes); got != want {
		t.Fatalf("drained minimal delta FactCount = %d, want exact emitted count %d", got, want)
	}
}

func TestRationaleProductionMarkerMatchesReplayCatalog(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../testdata/cassettes/rationale/ifa-rationale-family.json")
	if err != nil {
		t.Fatalf("read rationale cassette: %v", err)
	}
	var document struct {
		Scopes []struct {
			ScopeID      string `json:"scope_id"`
			GenerationID string `json:"generation_id"`
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
		t.Fatalf("parse rationale cassette: %v", err)
	}
	var catalog facts.Envelope
	for _, scope := range document.Scopes {
		for _, fact := range scope.Facts {
			if fact.FactKind == "shared_followup" && fact.Payload["reducer_domain"] == "rationale_materialization" {
				catalog = facts.Envelope{
					ScopeID: scope.ScopeID, GenerationID: scope.GenerationID,
					FactKind: fact.FactKind, StableFactKey: fact.StableFactKey,
					SchemaVersion: fact.SchemaVersion, CollectorKind: fact.CollectorKind,
					SourceConfidence: fact.SourceConfidence, Payload: fact.Payload,
				}
			}
		}
	}
	if catalog.StableFactKey == "" {
		t.Fatal("rationale cassette lacks rationale_materialization followup")
	}
	production := rationaleMaterializationFactEnvelope(
		"/repo-rationale", "repository:r_f781caa5", "scope-ifa-rationale-family", "gen-ifa-rationale-family-1",
		time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
	)
	clearDerivedEnvelopeFields := func(envelope facts.Envelope) facts.Envelope {
		envelope.FactID = ""
		// Replay cassette facts require a schema_version wrapper even for the
		// internal shared_followup marker; production factEnvelope leaves it
		// blank, so normalize that transport-only difference.
		envelope.SchemaVersion = ""
		envelope.FencingToken = 0
		envelope.ObservedAt = time.Time{}
		envelope.SourceRef = facts.Ref{}
		return envelope
	}
	if got, want := clearDerivedEnvelopeFields(catalog), clearDerivedEnvelopeFields(production); !reflect.DeepEqual(got, want) {
		t.Fatalf("rationale catalog marker drifted from production helper\ncatalog: %#v\nproduction: %#v", got, want)
	}
}

func assertSingleRationaleFollowup(t *testing.T, envelopes []facts.Envelope, repoPath, repoID, scopeID, generationID string) {
	t.Helper()
	var matches int
	lastContentEntityIndex := -1
	markerIndex := -1
	for index, envelope := range envelopes {
		if envelope.FactKind == "content_entity" {
			lastContentEntityIndex = index
		}
		if envelope.FactKind != "shared_followup" || envelope.Payload["reducer_domain"] != "rationale_materialization" {
			continue
		}
		matches++
		markerIndex = index
		if got, want := envelope.StableFactKey, "shared_followup:"+repoID+":rationale_materialization"; got != want {
			t.Errorf("rationale followup stable key = %q, want %q", got, want)
		}
		if got, want := envelope.Payload["repo_id"], repoID; got != want {
			t.Errorf("rationale followup repo_id = %#v, want %#v", got, want)
		}
		if got, want := envelope.Payload["entity_key"], "rationale:"+filepath.Base(repoPath); got != want {
			t.Errorf("rationale followup entity_key = %#v, want %#v", got, want)
		}
		if got, want := envelope.Payload["reason"], "repository generation requested rationale materialization reconciliation"; got != want {
			t.Errorf("rationale followup reason = %#v, want %#v", got, want)
		}
		if envelope.ScopeID != scopeID || envelope.GenerationID != generationID {
			t.Errorf("rationale followup scope/generation = %q/%q, want %q/%q", envelope.ScopeID, envelope.GenerationID, scopeID, generationID)
		}
	}
	if matches != 1 {
		t.Fatalf("rationale_materialization followups = %d, want exactly 1", matches)
	}
	if markerIndex <= lastContentEntityIndex {
		t.Fatalf("rationale followup index = %d, want after final content_entity index %d", markerIndex, lastContentEntityIndex)
	}
}

func BenchmarkBuildStreamingGenerationRationaleFollowup(b *testing.B) {
	for _, delta := range []bool{false, true} {
		delta := delta
		b.Run(map[bool]string{false: "full", true: "delta"}[delta], func(b *testing.B) {
			repoPath := b.TempDir()
			repo := testCollectorRepositoryMetadata(repoPath)
			snapshot := RepositorySnapshot{Delta: delta}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				collected := buildStreamingGeneration(repoPath, repo, "run-benchmark", time.Unix(0, 0).UTC(), snapshot, false, "")
				for range collected.Facts {
				}
			}
		})
	}
}

var rationaleFollowupBenchmarkSink facts.Envelope

func BenchmarkRationaleMaterializationFactEnvelope(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		rationaleFollowupBenchmarkSink = rationaleMaterializationFactEnvelope(
			"/repo-rationale", "repo-ifa-rationale", "scope-ifa-rationale", "gen-1", time.Unix(0, 0).UTC(),
		)
	}
}
