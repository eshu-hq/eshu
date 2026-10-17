// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// THROWAWAY PHASE 0 MEASUREMENT SHIM for epic #5393 (branch-aware coverage).
//
// This file is NOT production wiring. It measures the CANDIDATE projection
// cost: what Runtime.Project pays to project one ref's worth of facts, so the
// owner cap decision can weigh fact volume (measured on the collector lane)
// against projection CPU, queue load (reducer intents enqueued), and write
// load (content records and canonical rows written).
//
// The ref_scope_gated refusal is OBSERVED, never tripped, here: every run
// below uses a default-kind scope (testScope), and the fail-closed gate for
// KindRepositoryRef scopes is pinned by TestRuntimeProjectRejectsRefScope in
// ref_scope_gate_test.go. No ref-scoped generation ever reaches Project on
// this lane; no overlay tables, RefSnapshot nodes, or ref-scoped reads exist
// anywhere in the shim.
//
// The input mix mirrors the real git-collector envelope shapes
// (go/internal/collector/git/content/envelopes.go): "repository", "file"
// metadata facts, "content" body facts, and "content_entity" Function facts.
// Entities split into undocumented (no callable metadata, like the collector
// lane's fixture funcs — these enqueue nothing) and documented (docstring
// metadata — one semantic intent each). Counts are asserted exactly; wall
// times are logged for the Phase 0 report, never asserted.
//
// Delete this file once the Phase 0 caps are decided and recorded.

// phase0CandidateFacts builds the 1× input volume: one repository fact,
// fileCount file-metadata facts, fileCount content facts, and entityCount
// Function entity facts (three quarters undocumented, one quarter
// documented). IDs and paths are unique per index so no dedup stage can
// collapse replicas and hide the true N× cost.
func phase0CandidateFacts(t *testing.T, scopeID, generationID string, fileCount, entityCount int) []facts.Envelope {
	t.Helper()
	observedAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	inputFacts := make([]facts.Envelope, 0, 1+2*fileCount+entityCount)
	inputFacts = append(inputFacts, facts.Envelope{
		FactID:       "phase0-repository",
		ScopeID:      scopeID,
		GenerationID: generationID,
		FactKind:     "repository",
		ObservedAt:   observedAt,
		Payload:      map[string]any{"repo_id": "repo-abc", "name": "phase0-shim"},
	})
	for i := 0; i < fileCount; i++ {
		rel := fmt.Sprintf("phase0/svc%05d.go", i)
		inputFacts = append(inputFacts, facts.Envelope{
			FactID:       fmt.Sprintf("phase0-file-%05d", i),
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKind:     "file",
			ObservedAt:   observedAt,
			Payload: map[string]any{
				"graph_id":      "repo-abc:" + rel,
				"graph_kind":    "file",
				"repo_id":       "repo-abc",
				"relative_path": rel,
				"language":      "go",
				"parsed_file_data": map[string]any{
					"path":     rel,
					"language": "go",
				},
			},
		})
		inputFacts = append(inputFacts, facts.Envelope{
			FactID:       fmt.Sprintf("phase0-content-%05d", i),
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKind:     "content",
			ObservedAt:   observedAt,
			Payload: map[string]any{
				"content_path":   rel,
				"content_body":   fmt.Sprintf("package svc%05d\n\nfunc Fn%05d() int {\n\treturn %d\n}\n", i, i, i),
				"content_digest": fmt.Sprintf("sha256:%064d", i),
				"repo_id":        "repo-abc",
				"language":       "go",
			},
		})
	}
	for i := 0; i < entityCount; i++ {
		rel := fmt.Sprintf("phase0/svc%05d.go", i%fileCount)
		metadata := map[string]any{"parameter_count": 1, "return_type": "int"}
		if i%4 == 3 {
			// Documented quarter: callable metadata, like a Go function
			// with a doc comment — one semantic intent each.
			metadata["docstring"] = fmt.Sprintf("Fn%05d does phase0 work.", i)
		}
		inputFacts = append(inputFacts, facts.Envelope{
			FactID:       fmt.Sprintf("phase0-entity-%05d", i),
			ScopeID:      scopeID,
			GenerationID: generationID,
			FactKind:     "content_entity",
			ObservedAt:   observedAt,
			Payload: map[string]any{
				"graph_id":        fmt.Sprintf("content-entity:phase0-entity-%05d", i),
				"graph_kind":      "content_entity",
				"entity_id":       fmt.Sprintf("phase0-entity-%05d", i),
				"repo_id":         "repo-abc",
				"relative_path":   rel,
				"entity_type":     "Function",
				"entity_name":     fmt.Sprintf("Fn%05d", i),
				"start_line":      3,
				"end_line":        5,
				"language":        "go",
				"entity_metadata": metadata,
			},
		})
	}
	return inputFacts
}

// TestPhase0CandidateProjectionCost projects the collector lane's dominant
// fact mix at 1×/2×/4× volume and records intent, content-record, canonical,
// and wall-time scaling. Content records must equal the content-fact count
// exactly (pure write load); total intents must grow with constant second
// differences (linear queue load plus any constant per-run term); the logged
// wall times are the Phase 0 projection-cost evidence.
func TestPhase0CandidateProjectionCost(t *testing.T) {
	scopeValue := testScope()
	generation := testGeneration()
	// ValidateForScope requires generation timestamps (mirrors the gate test).
	generation.ObservedAt = time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	generation.IngestedAt = time.Date(2026, time.September, 8, 12, 5, 0, 0, time.UTC)

	// Base unit: 1 repository + 40 file + 40 content + 80 entity facts, the
	// same order as one collector-lane ref snapshot (81 rows there).
	unitFiles, unitEntities := 40, 80
	intentTotals := make([]int, 0, 3)
	for _, multiple := range []int{1, 2, 4} {
		inputFacts := phase0CandidateFacts(t, scopeValue.ScopeID, generation.GenerationID, unitFiles*multiple, unitEntities*multiple)

		canonicalWriter := &recordingCanonicalWriter{}
		contentWriter := &recordingContentWriter{result: content.Result{}}
		intentWriter := &recordingIntentWriter{result: IntentResult{Count: len(inputFacts)}}
		runtime := Runtime{
			CanonicalWriter: canonicalWriter,
			ContentWriter:   contentWriter,
			IntentWriter:    intentWriter,
		}

		start := time.Now()
		result, err := runtime.Project(context.Background(), scopeValue, generation, inputFacts)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("Project(%d facts) error = %v, want nil", len(inputFacts), err)
		}
		if result.ScopeID != scopeValue.ScopeID || result.GenerationID != generation.GenerationID {
			t.Fatalf("result scope/generation = %q/%q, want %q/%q",
				result.ScopeID, result.GenerationID, scopeValue.ScopeID, generation.GenerationID)
		}
		if len(canonicalWriter.calls) != 1 {
			t.Fatalf("canonical writes at %dx = %d, want 1", multiple, len(canonicalWriter.calls))
		}
		if len(contentWriter.calls) != 1 {
			t.Fatalf("content writes at %dx = %d, want 1", multiple, len(contentWriter.calls))
		}
		intents := 0
		for _, batch := range intentWriter.calls {
			intents += len(batch)
		}
		records := 0
		for _, mat := range contentWriter.calls {
			records += len(mat.Records)
		}
		mat := canonicalWriter.calls[0]
		canonRows := len(mat.Files) + len(mat.Entities) + len(mat.Directories) + len(mat.Imports)
		if mat.Repository != nil {
			canonRows++
		}
		intentTotals = append(intentTotals, intents)

		// Write load: one content record per content fact, exactly.
		if want := unitFiles * multiple; records != want {
			t.Fatalf("content records at %dx = %d, want %d", multiple, records, want)
		}
		t.Logf("phase0 projection: facts=%d intents=%d content_records=%d canonical_rows=%d wall=%s (%.1f facts/ms)",
			len(inputFacts), intents, records, canonRows, elapsed, float64(len(inputFacts))/float64(elapsed.Microseconds())*1000)
	}

	// Queue load must be linear in volume: constant second differences hold
	// for intents(m) = variable*m + constant regardless of any constant
	// per-run term (e.g. a scope-generation intent).
	i1, i2, i4 := intentTotals[0], intentTotals[1], intentTotals[2]
	if i1 == 0 {
		t.Fatal("intents at 1x = 0: the documented-entity queue path is not exercised")
	}
	if i2 <= i1 || i4 <= i2 {
		t.Fatalf("intents not growing: 1x=%d 2x=%d 4x=%d", i1, i2, i4)
	}
	if (i4 - i2) != 2*(i2-i1) {
		t.Fatalf("intents not linear: 1x=%d 2x=%d 4x=%d (second differences differ)", i1, i2, i4)
	}
}
