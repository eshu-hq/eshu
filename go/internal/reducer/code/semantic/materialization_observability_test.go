// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestSemanticEntityMaterializationHandlerLogsStageTiming(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &fakeSemanticEntityFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "content_entity",
				SourceRef: facts.Ref{
					SourceURI: "/repo/src/app.js",
				},
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"entity_id":     "function-1",
					"relative_path": "src/app.js",
					"entity_type":   "Function",
					"entity_name":   "getTab",
					"language":      "javascript",
					"method_kind":   "getter",
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{CanonicalWrites: 1},
	}

	handler := Handler{
		FactLoader: loader,
		Writer:     writer,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       reducercontract.DomainSemanticEntityMaterialization,
		Status:       reducercontract.IntentStatusClaimed,
		EnqueuedAt:   time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"semantic entity materialization completed"`,
		`"fact_count":1`,
		`"row_count":1`,
		`"repo_count":1`,
		`"skip_retract":false`,
		`"delta_projection":false`,
		`"delta_file_count":0`,
		`"load_facts_duration_seconds":`,
		`"extract_duration_seconds":`,
		`"retract_decision_duration_seconds":`,
		`"graph_write_duration_seconds":`,
		`"phase_publish_duration_seconds":`,
		`"total_duration_seconds":`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

// TestSemanticEntityMaterializationHandlerWarnsOnUnmatchedUIDs is the seeded
// RED for the #8359 MATCH-miss telemetry: when the writer reports admitted
// rows that matched no canonical node, Handle must WARN naming the uids
// (bounded) with per-label attempted-vs-matched counts on the completion
// record, and must stop calling admitted rows "materialized" in its status.
func TestSemanticEntityMaterializationHandlerWarnsOnUnmatchedUIDs(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &fakeSemanticEntityFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "content_entity",
				SourceRef: facts.Ref{
					SourceURI: "/repo/src/app.js",
				},
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"entity_id":     "function-1",
					"relative_path": "src/app.js",
					"entity_type":   "Function",
					"entity_name":   "getTab",
					"language":      "javascript",
					"method_kind":   "getter",
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{
			CanonicalWrites:      1,
			RowsAttemptedByLabel: map[string]int{"Function": 1},
			RowsMatchedByLabel:   map[string]int{"Function": 0},
			UnmatchedEntityIDs:   []string{"content-entity:sec-py-func"},
		},
	}

	handler := Handler{
		FactLoader: loader,
		Writer:     writer,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       reducercontract.DomainSemanticEntityMaterialization,
		Status:       reducercontract.IntentStatusClaimed,
		EnqueuedAt:   time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if strings.Contains(result.EvidenceSummary, "materialized 1 semantic entities") {
		t.Errorf("EvidenceSummary = %q, must not call admitted rows materialized", result.EvidenceSummary)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"semantic entity rows matched no canonical node"`,
		`content-entity:sec-py-func`,
		`"unmatched_uid_count":1`,
		`"rows_attempted_by_label":{"Function":1}`,
		`"rows_matched_by_label":{"Function":0}`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

// TestSemanticEntityMaterializationHandlerSilentWhenAllRowsMatched pins the
// other half of the telemetry contract: a fully-matched write logs its
// per-label counts on the completion record but emits no unmatched-uid WARN.
func TestSemanticEntityMaterializationHandlerSilentWhenAllRowsMatched(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &fakeSemanticEntityFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "content_entity",
				SourceRef: facts.Ref{
					SourceURI: "/repo/src/app.js",
				},
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"entity_id":     "function-1",
					"relative_path": "src/app.js",
					"entity_type":   "Function",
					"entity_name":   "getTab",
					"language":      "javascript",
					"method_kind":   "getter",
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{
			CanonicalWrites:      1,
			RowsAttemptedByLabel: map[string]int{"Function": 1},
			RowsMatchedByLabel:   map[string]int{"Function": 1},
		},
	}

	handler := Handler{
		FactLoader: loader,
		Writer:     writer,
	}

	if _, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       reducercontract.DomainSemanticEntityMaterialization,
		Status:       reducercontract.IntentStatusClaimed,
		EnqueuedAt:   time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	if strings.Contains(logText, "matched no canonical node") {
		t.Errorf("matched write emitted an unmatched-uid WARN:\n%s", logText)
	}
	for _, want := range []string{
		`"rows_attempted_by_label":{"Function":1}`,
		`"rows_matched_by_label":{"Function":1}`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}
