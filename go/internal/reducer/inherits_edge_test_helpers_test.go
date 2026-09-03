// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// The inheritance family's production code moved to internal/reducer/inheritance
// under issue #6061, and Go test files cannot share unexported symbols across a
// package boundary. These are therefore local copies of the fixture and the
// recording intent writer that the root's cross-domain suites -- the fact-kind
// and fact-payload loader gates, the idempotency cases, and the
// sub-duration/sub-signal gate -- drive the relocated handler through. Keep them
// in step with the family's own copies in
// internal/reducer/inheritance/test_helpers_test.go and materialization_test.go.

// inheritanceEntityFacts returns the shared repo-1 parent/child fixture: one
// repository envelope plus two content_entity envelopes whose child declares the
// parent as a base, yielding exactly one INHERITS edge.
func inheritanceEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"path":          "/repo",
				"source_run_id": "run-1",
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":     "repo-1",
				"entity_id":   "content-entity:e_parent",
				"entity_type": "Class",
				"entity_name": "ParentClass",
				// "relative_path" is the key contentEntityFactEnvelope actually
				// emits (contentEntityFactEnvelope in git_content_fact_envelopes.go);
				// production carries no
				// top-level "path" key (#5996).
				"relative_path": "/repo/parent.py",
			},
		},
		{
			FactKind: factload.FactKindContentEntity,
			ScopeID:  "scope-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "content-entity:e_child",
				"entity_type":   "Class",
				"entity_name":   "ChildClass",
				"relative_path": "/repo/child.py",
				"entity_metadata": map[string]any{
					"bases": []any{"ParentClass"},
				},
			},
		},
	}
}

// inheritanceEntityContentFacts returns only the content_entity envelopes of that
// fixture (no repository envelope), for loaders that supply the repository fact
// through a separate kind-filtered channel (#2867).
func inheritanceEntityContentFacts() []facts.Envelope {
	out := make([]facts.Envelope, 0, 2)
	for _, env := range inheritanceEntityFacts() {
		if env.FactKind == factload.FactKindContentEntity {
			out = append(out, env)
		}
	}
	return out
}

// recordingInheritanceIntentWriter captures the durable shared-projection intents
// the promoted inheritance handler emits, so handler tests assert on emitted
// intents instead of direct edge writes (#2867).
type recordingInheritanceIntentWriter struct {
	rows []sharedintent.Row
}

func (w *recordingInheritanceIntentWriter) UpsertIntents(_ context.Context, rows []sharedintent.Row) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingInheritanceIntentWriter) edgeRows() []sharedintent.Row {
	var out []sharedintent.Row
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}
