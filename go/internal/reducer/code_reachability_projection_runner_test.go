// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestCodeReachabilityProjectionRunnerProcessesPendingInputs(t *testing.T) {
	t.Parallel()

	loader := &fakeCodeReachabilityInputLoader{
		inputs: []CodeReachabilityProjectionInput{{
			ScopeID:      "scope-1",
			GenerationID: "generation-1",
			RepositoryID: "repo-1",
			Roots:        []CodeReachabilityRoot{{EntityID: "entity:root"}},
			Edges: []CodeReachabilityEdge{{
				SourceEntityID:   "entity:root",
				TargetEntityID:   "entity:leaf",
				RelationshipType: "CALLS",
				ResolutionMethod: "scip",
			}},
			MaxDepth:  5,
			UpdatedAt: time.Date(2026, 6, 17, 3, 59, 0, 0, time.UTC),
		}},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
		Config: CodeReachabilityProjectionRunnerConfig{
			BatchLimit: 10,
		},
	}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if got, want := result.InputsProcessed, 1; got != want {
		t.Fatalf("InputsProcessed = %d, want %d", got, want)
	}
	if got, want := result.RowsWritten, 2; got != want {
		t.Fatalf("RowsWritten = %d, want %d rows=%#v", got, want, writer.rows)
	}
	if writer.scopeID != "scope-1" || writer.generationID != "generation-1" || writer.repositoryID != "repo-1" {
		t.Fatalf("replacement keys = %q/%q/%q, want scope-1/generation-1/repo-1", writer.scopeID, writer.generationID, writer.repositoryID)
	}
	if got, want := writer.watermark, loader.inputs[0].UpdatedAt; !got.Equal(want) {
		t.Fatalf("watermark = %v, want loaded completed_at %v", got, want)
	}
	if got, want := writer.rows[1].EntityID, "entity:leaf"; got != want {
		t.Fatalf("written leaf entity = %q, want %q", got, want)
	}
}

func TestCodeReachabilityProjectionRunnerRecordsEmptyInputWatermark(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 6, 17, 4, 5, 0, 0, time.UTC)
	loader := &fakeCodeReachabilityInputLoader{
		inputs: []CodeReachabilityProjectionInput{{
			ScopeID:      "scope-empty",
			GenerationID: "generation-empty",
			RepositoryID: "repo-empty",
			UpdatedAt:    completedAt,
		}},
	}
	writer := &fakeCodeReachabilityRowWriter{}
	runner := CodeReachabilityProjectionRunner{
		InputLoader: loader,
		RowWriter:   writer,
	}

	result, err := runner.ProcessOnce(context.Background(), time.Date(2026, 6, 17, 4, 10, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if got, want := result.InputsProcessed, 1; got != want {
		t.Fatalf("InputsProcessed = %d, want %d", got, want)
	}
	if got, want := result.RowsWritten, 0; got != want {
		t.Fatalf("RowsWritten = %d, want %d", got, want)
	}
	if got, want := writer.watermark, completedAt; !got.Equal(want) {
		t.Fatalf("watermark = %v, want loaded completed_at %v", got, want)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("rows = %#v, want empty replacement", writer.rows)
	}
}

type fakeCodeReachabilityInputLoader struct {
	inputs []CodeReachabilityProjectionInput
}

func (f *fakeCodeReachabilityInputLoader) LoadPendingCodeReachabilityInputs(
	_ context.Context,
	_ int,
) ([]CodeReachabilityProjectionInput, error) {
	return append([]CodeReachabilityProjectionInput(nil), f.inputs...), nil
}

type fakeCodeReachabilityRowWriter struct {
	scopeID      string
	generationID string
	repositoryID string
	watermark    time.Time
	truncated    bool
	rows         []CodeReachabilityRow
}

func (f *fakeCodeReachabilityRowWriter) ReplaceRepositoryRows(
	_ context.Context,
	scopeID string,
	generationID string,
	repositoryID string,
	rows []CodeReachabilityRow,
	watermark time.Time,
	truncated bool,
) error {
	f.scopeID = scopeID
	f.generationID = generationID
	f.repositoryID = repositoryID
	f.watermark = watermark
	f.truncated = truncated
	f.rows = append(f.rows, rows...)
	return nil
}
