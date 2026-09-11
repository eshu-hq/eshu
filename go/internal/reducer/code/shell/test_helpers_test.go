// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shell

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// This package moved out of the flat internal/reducer root under issue #6061,
// and Go test files cannot share unexported symbols across a package
// boundary. stubFactLoader, stubIntentWriter, rowUsesRefreshFence, and
// shellRepositoryEnvelope below are therefore local copies of reducer-root
// test/production helpers, not new behavior (mirroring
// sqlrelationship/sql_relationship_test_helpers_test.go's identical copies).

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

// isRepoRefreshRow reports whether a row is a per-repo refresh intent.
func isRepoRefreshRow(row sharedintent.Row) bool {
	return payloadcore.PayloadStr(row.Payload, "intent_type") == sharedintent.RepoRefreshIntentType
}

// stubIntentWriter captures the durable shared-projection intents
// ExecMaterializationHandler emits, so handler tests assert on emitted
// intents instead of direct edge writes (#2868).
type stubIntentWriter struct {
	rows []sharedintent.Row
}

func (w *stubIntentWriter) UpsertIntents(_ context.Context, rows []sharedintent.Row) error {
	w.rows = append(w.rows, rows...)
	return nil
}

// refreshRows returns the per-repo refresh intents (the rows that own the
// retract) the writer captured.
func (w *stubIntentWriter) refreshRows() []sharedintent.Row {
	var out []sharedintent.Row
	for _, row := range w.rows {
		if isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// edgeRows returns the write-only per-edge intents the writer captured.
func (w *stubIntentWriter) edgeRows() []sharedintent.Row {
	var out []sharedintent.Row
	for _, row := range w.rows {
		if !isRepoRefreshRow(row) {
			out = append(out, row)
		}
	}
	return out
}

// shellRepositoryEnvelope returns the shared repo-123 repository fixture on a
// full (non-delta) generation, mirroring
// sql_relationship_root_test_helpers_test.go's sqlRelationshipRepositoryEnvelope(false, nil).
func shellRepositoryEnvelope() facts.Envelope {
	return facts.Envelope{
		FactKind: factload.FactKindRepository,
		ScopeID:  "scope-db",
		Payload: map[string]any{
			"repo_id":       "repo-123",
			"path":          "/repo",
			"source_run_id": "run-1",
		},
	}
}
