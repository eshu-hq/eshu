// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sqlrelationship

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// This package moved out of the flat internal/reducer root under issue #6061,
// and Go test files cannot share unexported symbols across a package
// boundary. stubFactLoader and rowUsesRefreshFence below are therefore local
// copies of reducer-root test/production helpers, not new behavior (mirroring
// inheritance/test_helpers_test.go's identical copies).

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
func rowUsesRefreshFence(row SharedProjectionIntentRow) bool {
	return payloadcore.PayloadBool(row.Payload, sharedintent.RetractViaRefreshKey)
}

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
