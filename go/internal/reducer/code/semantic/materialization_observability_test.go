// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// #8587 supersedes the WARN-only contract: unmatched uids are
	// deterministic identity divergences, so Handle fails non-retryably. The
	// WARN stays as the operator-facing signal alongside the failure.
	if err == nil {
		t.Fatalf("Handle() error = nil, want non-retryable identity-divergence failure")
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"semantic entity rows matched no canonical node"`,
		`content-entity:sec-py-func`,
		`"unmatched_uid_count":1`,
		`"rows_attempted_by_label":{"Function":1}`,
		`"rows_matched_by_label":{"Function":0}`,
		// #8587 owner P2: the completion record emits on the failure path
		// with failed=true so log queries cannot mistake dead-lettered
		// items for successes.
		`"msg":"semantic entity materialization completed"`,
		`"failed":true`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

// TestSemanticEntityMaterializationHandlerFailsOnIdentityDivergentUIDs is the
// seeded RED for #8587: unmatched uids are deterministic, never races (the
// projector provably never derives that uid, and the writer's verify is pure
// Go over row identities — no backend race can explain a miss). Handle must
// fail the work item with a non-retryable error naming the uids (bounded) so
// the queue dead-letters it once instead of retry-looping a poison pill.
func TestSemanticEntityMaterializationHandlerFailsOnIdentityDivergentUIDs(t *testing.T) {
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
	if err == nil {
		t.Fatalf("Handle() error = nil, want non-retryable failure naming the unmatched uid")
	}
	if !strings.Contains(err.Error(), "content-entity:sec-py-func") {
		t.Errorf("Handle() error = %q, must name the unmatched uid", err.Error())
	}
	if reducercontract.IsRetryable(err) {
		t.Errorf("Handle() error is retryable; an identity-divergent uid can never heal on retry (poison pill)")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("Handle() error %T carries no FailureClass(); telemetry would log reducer_failure instead of projection_bug", err)
	}
	if classified.FailureClass() != "projection_bug" {
		t.Errorf("Handle() error FailureClass() = %q, want projection_bug (manual-review poison bucket)", classified.FailureClass())
	}
}

// TestSemanticEntityIdentityDivergentErrorTruncatesUIDs pins the P3 bound:
// past maxSemanticUnmatchedUIDsLogged uids the message names the capped
// prefix and reports the overflow count instead of growing unbounded.
func TestSemanticEntityIdentityDivergentErrorTruncatesUIDs(t *testing.T) {
	uids := make([]string, 0, maxSemanticUnmatchedUIDsLogged+2)
	for i := 0; i < maxSemanticUnmatchedUIDsLogged+2; i++ {
		uids = append(uids, fmt.Sprintf("uid-%d", i))
	}
	err := newSemanticEntityIdentityDivergentError(EntityWriteResult{
		RowsAttemptedByLabel: map[string]int{"Function": len(uids)},
		RowsMatchedByLabel:   map[string]int{},
		UnmatchedEntityIDs:   uids,
	})
	if err == nil {
		t.Fatalf("newSemanticEntityIdentityDivergentError() = nil, want failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "uid-0") {
		t.Errorf("error message = %q, must name the capped uid prefix", msg)
	}
	for _, overflow := range []string{
		fmt.Sprintf("uid-%d", maxSemanticUnmatchedUIDsLogged),
		fmt.Sprintf("uid-%d", maxSemanticUnmatchedUIDsLogged+1),
	} {
		if strings.Contains(msg, overflow+" ") || strings.Contains(msg, overflow+"]") {
			t.Errorf("error message = %q, must not name overflow %s", msg, overflow)
		}
	}
	if !strings.Contains(msg, "truncated=2") {
		t.Errorf("error message = %q, must report truncated=2", msg)
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
