// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// inheritanceEntityContentFacts returns only the content_entity envelopes for the
// shared repo-1 parent/child fixture (no repository envelope), for loaders that
// supply the repository fact through a separate kind-filtered channel (#2867).
func inheritanceEntityContentFacts() []facts.Envelope {
	out := make([]facts.Envelope, 0, 2)
	for _, env := range inheritanceEntityFacts() {
		if env.FactKind == factKindContentEntity {
			out = append(out, env)
		}
	}
	return out
}

// recordingInheritanceIntentWriter captures the durable shared-projection intents
// the promoted InheritanceMaterializationHandler emits, so handler tests assert
// on emitted intents instead of direct edge writes (#2867).
type recordingInheritanceIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (w *recordingInheritanceIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *recordingInheritanceIntentWriter) refreshRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingInheritanceIntentWriter) edgeRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}
