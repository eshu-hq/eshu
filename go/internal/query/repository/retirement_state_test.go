// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	contractrepository "github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

var (
	errSelectionBoom  = errors.New("selection boom")
	errRetirementBoom = errors.New("retirement boom")
)

// TestDeriveRepositoryIndexStatePrecedence proves the #7766 bounded
// index_state derivation order: an open non-complete tombstone reads
// retiring, a complete tombstone reads retired, otherwise the graph
// evidence source decides indexed versus stub. A tombstone is never
// flattened to empty: retiring and retired rows stay in the list.
func TestDeriveRepositoryIndexStatePrecedence(t *testing.T) {
	t.Parallel()

	open := &contractrepository.PageRetirement{State: "pending", RetirementID: "rr_1"}
	complete := &contractrepository.PageRetirement{State: "complete", RetirementID: "rr_1"}

	tests := []struct {
		name           string
		tombstone      *contractrepository.PageRetirement
		evidenceSource string
		want           string
	}{
		{"open tombstone on indexed evidence is retiring", open, "projector/canonical", IndexStateRetiring},
		{"open tombstone on stub evidence is retiring", open, "other", IndexStateRetiring},
		{"complete tombstone is retired even with canonical evidence", complete, "projector/canonical", IndexStateRetired},
		{"complete tombstone without evidence is retired", complete, "", IndexStateRetired},
		{"canonical evidence without tombstone is indexed", nil, "projector/canonical", IndexStateIndexed},
		{"other evidence without tombstone is stub", nil, "other", IndexStateStub},
		{"missing evidence without tombstone is stub", nil, "", IndexStateStub},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DeriveRepositoryIndexState(tt.tombstone, tt.evidenceSource); got != tt.want {
				t.Fatalf("DeriveRepositoryIndexState() = %q, want %q", got, tt.want)
			}
		})
	}
}

// stubPageRetirementReader answers one batched tombstone page from a fixed
// map, recording the page ids so the annotation test proves the page
// actually reaches the store in one call.
type stubPageRetirementReader struct {
	labels map[string]contractrepository.PageRetirement
	calls  [][]string
	err    error
}

func (s *stubPageRetirementReader) ReadPageRetirements(_ context.Context, repoIDs []string) (map[string]contractrepository.PageRetirement, error) {
	s.calls = append(s.calls, repoIDs)
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]contractrepository.PageRetirement, len(repoIDs))
	for _, id := range repoIDs {
		if label, ok := s.labels[id]; ok {
			out[id] = label
		}
	}
	return out, nil
}

// stubPageSelectionReader answers the selection annotation empty or
// fails, so the staged page test proves which stage stopped.
type stubPageSelectionReader struct {
	err error
}

func (s stubPageSelectionReader) ReadPageSelectionsBySlug(_ context.Context, _ []string) (selection.PageSelections, error) {
	if s.err != nil {
		return selection.PageSelections{}, s.err
	}
	return selection.PageSelections{}, nil
}

// TestAnnotateListRepositoriesWithRetirement proves the list annotation:
// every row stays in the page carrying index_state plus the bounded
// retirement object, an open tombstone reads retiring with its state, a
// complete tombstone reads retired with its retirement id, and rows without
// a tombstone fall back to their evidence source. A nil store degrades to
// the evidence source alone, never to an error.
func TestAnnotateListRepositoriesWithRetirement(t *testing.T) {
	t.Parallel()

	retiredAt := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	store := &stubPageRetirementReader{labels: map[string]contractrepository.PageRetirement{
		"repo-fenced": {State: "pending", Phase: "fenced", RetirementID: "rr_f1"},
		"repo-gone":   {State: "complete", Phase: "done", RetirementID: "rr_g9", RetiredAt: retiredAt, HasRetiredAt: true},
	}}
	repos := []map[string]any{
		{"id": "repo-fenced", "evidence_source": "projector/canonical"},
		{"id": "repo-gone", "evidence_source": "projector/canonical"},
		{"id": "repo-live", "evidence_source": "projector/canonical"},
		{"id": "repo-stub", "evidence_source": "other"},
	}

	annotated, err := annotateListRepositoriesWithRetirement(context.Background(), store, repos)
	if err != nil {
		t.Fatalf("annotateListRepositoriesWithRetirement: %v", err)
	}
	if len(annotated) != len(repos) {
		t.Fatalf("rows = %d, want %d (tombstones are labelled, never dropped)", len(annotated), len(repos))
	}
	if got := annotated[0]["index_state"]; got != IndexStateRetiring {
		t.Fatalf("fenced index_state = %#v, want retiring", got)
	}
	retirement, ok := annotated[0]["retirement"].(map[string]any)
	if !ok {
		t.Fatalf("fenced retirement = %#v, want the bounded object", annotated[0]["retirement"])
	}
	if retirement["state"] != "pending" || retirement["retirement_id"] != "rr_f1" {
		t.Fatalf("fenced retirement = %#v, want state pending id rr_f1", retirement)
	}
	if got := annotated[1]["index_state"]; got != IndexStateRetired {
		t.Fatalf("complete index_state = %#v, want retired", got)
	}
	if got := annotated[2]["index_state"]; got != IndexStateIndexed {
		t.Fatalf("live index_state = %#v, want indexed", got)
	}
	if annotated[2]["retirement"] != nil {
		t.Fatalf("live retirement = %#v, want nil", annotated[2]["retirement"])
	}
	if got := annotated[3]["index_state"]; got != IndexStateStub {
		t.Fatalf("stub index_state = %#v, want stub", got)
	}

	if len(store.calls) != 1 {
		t.Fatalf("store calls = %d, want exactly one batched page read", len(store.calls))
	}
	if len(store.calls[0]) != len(repos) {
		t.Fatalf("page ids = %v, want all %d row ids", store.calls[0], len(repos))
	}

	// Nil store: evidence source alone, no error.
	degraded, err := annotateListRepositoriesWithRetirement(context.Background(), nil, repos)
	if err != nil {
		t.Fatalf("nil-store annotation: %v", err)
	}
	if got := degraded[0]["index_state"]; got != IndexStateIndexed {
		t.Fatalf("nil-store fenced index_state = %#v, want indexed (no tombstone evidence)", got)
	}
}

// TestAnnotateListPageStagesAttribution proves the staged page annotation
// runs selection before retirement, strips the derivation-only evidence
// source, and reports which stage stopped: a selection failure maps to the
// selection message, a retirement failure to the retirement message, and
// neither stage serves a partial page.
func TestAnnotateListPageStagesAttribution(t *testing.T) {
	t.Parallel()

	repos := []map[string]any{
		{"id": "repo-live", "repo_slug": "live", "evidence_source": "projector/canonical"},
	}

	annotated, stage, err := annotateListPageWithSelectionAndRetirement(
		context.Background(),
		stubPageSelectionReader{},
		&stubPageRetirementReader{labels: map[string]contractrepository.PageRetirement{}},
		repos,
	)
	if err != nil {
		t.Fatalf("staged annotation: %v", err)
	}
	if stage != 0 {
		t.Fatalf("stage = %v, want 0 on success", stage)
	}
	if got := annotated[0]["index_state"]; got != IndexStateIndexed {
		t.Fatalf("index_state = %#v, want indexed", got)
	}
	if _, ok := annotated[0]["evidence_source"]; ok {
		t.Fatalf("evidence_source survived: %#v, want it stripped from API rows", annotated[0])
	}

	if _, stage, err := annotateListPageWithSelectionAndRetirement(
		context.Background(),
		stubPageSelectionReader{err: errSelectionBoom},
		&stubPageRetirementReader{},
		repos,
	); err == nil || stage != listAnnotationSelection {
		t.Fatalf("selection failure = (%v, %v), want the selection stage", stage, err)
	} else if got := stage.failureMessage(); got != repositoryListSelectionFailedMessage {
		t.Fatalf("selection message = %q, want the selection failure message", got)
	}

	if _, stage, err := annotateListPageWithSelectionAndRetirement(
		context.Background(),
		stubPageSelectionReader{},
		&stubPageRetirementReader{err: errRetirementBoom},
		repos,
	); err == nil || stage != listAnnotationRetirement {
		t.Fatalf("retirement failure = (%v, %v), want the retirement stage", stage, err)
	} else if got := stage.failureMessage(); got != repositoryListRetirementFailedMessage {
		t.Fatalf("retirement message = %q, want the retirement failure message", got)
	}
}
