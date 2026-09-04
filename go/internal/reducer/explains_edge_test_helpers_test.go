// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The rationale family's production code moved to internal/reducer/rationale
// under issue #6061, and Go test files cannot share unexported symbols across
// a package boundary. These are therefore local copies of the fixture and the
// recording intent writer that the root's cross-domain suites -- the
// idempotency cases, the materialized-edge-family blocker shape gate, and the
// shared-projection domain-evidence gate -- drive the relocated handler
// through. Keep them in step with the family's own copies in
// internal/reducer/rationale/materialization_test.go and
// internal/reducer/rationale/test_helpers_test.go.

// rationaleDeltaEntityFacts returns the shared repo-123 delta fixture: one
// delta-generation repository envelope plus one content_entity envelope
// carrying a WHY rationale comment, yielding exactly one EXPLAINS edge.
func rationaleDeltaEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factKindRepository,
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":                      "repo-123",
				"local_path":                   "/repo",
				"source_run_id":                "run-1",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/handler.go", "../outside.go"},
				"delta_deleted_relative_paths": []string{},
			},
		},
		{
			FactKind: factKindContentEntity,
			ScopeID:  "scope-code",
			Payload: map[string]any{
				"repo_id":       "repo-123",
				"entity_id":     "content-entity:handler",
				"entity_type":   "Function",
				"entity_name":   "Handle",
				"relative_path": "src/handler.go",
				"entity_metadata": map[string]any{
					"rationale_comments": []any{
						map[string]any{"kind": "WHY", "text": "explain cached projector path"},
					},
				},
			},
		},
	}
}

// recordingRationaleIntentWriter captures the durable shared-projection intents
// the promoted rationale.MaterializationHandler emits, so handler tests assert
// on emitted intents instead of direct edge writes (#2869).
type recordingRationaleIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (w *recordingRationaleIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *recordingRationaleIntentWriter) refreshRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingRationaleIntentWriter) edgeRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IntentID < out[j].IntentID })
	return out
}
