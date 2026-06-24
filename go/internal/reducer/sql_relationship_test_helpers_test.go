// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
)

// recordingSQLRelationshipIntentWriter captures the durable shared-projection
// intents the promoted SQLRelationshipMaterializationHandler emits, so handler
// tests assert on emitted intents instead of direct edge writes (#2868).
type recordingSQLRelationshipIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (w *recordingSQLRelationshipIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *recordingSQLRelationshipIntentWriter) refreshRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingSQLRelationshipIntentWriter) edgeRows() []SharedProjectionIntentRow {
	var out []SharedProjectionIntentRow
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}
