// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rationale

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// This package moved out of the flat internal/reducer root under issue #6061,
// and Go test files cannot share unexported symbols across a package boundary.
// stubFactLoader, rowUsesRefreshFence and isRepoRefreshRow below are therefore
// local copies of reducer-root test/production helpers, not new behavior.

// stubFactLoader returns a fixed envelope set for every scope generation.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// rowUsesRefreshFence reports whether a per-edge row carries the marker that
// lets the worker fence it behind its paired repo refresh intent.
func rowUsesRefreshFence(row sharedintent.Row) bool {
	return payloadcore.PayloadBool(row.Payload, sharedintent.RetractViaRefreshKey)
}

// isRepoRefreshRow reports whether a row is a per-repo refresh intent -- the one
// row that owns the domain's single retract. It reads the same payload key and
// constant the reducer root's fence does.
func isRepoRefreshRow(row sharedintent.Row) bool {
	return payloadcore.PayloadStr(row.Payload, "intent_type") == sharedintent.RepoRefreshIntentType
}

// recordingRationaleIntentWriter captures the durable shared-projection intents
// the promoted MaterializationHandler emits, so handler tests assert
// on emitted intents instead of direct edge writes (#2869).
type recordingRationaleIntentWriter struct {
	rows []sharedintent.Row
}

func (w *recordingRationaleIntentWriter) UpsertIntents(_ context.Context, rows []sharedintent.Row) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *recordingRationaleIntentWriter) refreshRows() []sharedintent.Row {
	var out []sharedintent.Row
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *recordingRationaleIntentWriter) edgeRows() []sharedintent.Row {
	var out []sharedintent.Row
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].IntentID < out[j].IntentID })
	return out
}
