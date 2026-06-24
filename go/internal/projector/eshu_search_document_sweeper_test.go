// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type fakePendingLister struct {
	scopes []PendingSearchDocumentScope
	err    error
	calls  int
}

func (f *fakePendingLister) ListPendingSearchDocumentScopes(_ context.Context, _ int) ([]PendingSearchDocumentScope, error) {
	f.calls++
	return f.scopes, f.err
}

type fakeSweepIntentWriter struct {
	enqueued []ReducerIntent
}

func (f *fakeSweepIntentWriter) Enqueue(_ context.Context, intents []ReducerIntent) (IntentResult, error) {
	f.enqueued = append(f.enqueued, intents...)
	return IntentResult{Count: len(intents)}, nil
}

func TestSearchDocumentSweeperEnqueuesPendingScopes(t *testing.T) {
	t.Parallel()

	lister := &fakePendingLister{scopes: []PendingSearchDocumentScope{
		{ScopeID: "git-repository-scope:repository:r_a", GenerationID: "gen-a", SourceSystem: "github"},
		{ScopeID: "git-repository-scope:repository:r_b", GenerationID: "gen-b", SourceSystem: "github"},
	}}
	writer := &fakeSweepIntentWriter{}
	sweeper := SearchDocumentProjectionSweeper{Pending: lister, Intents: writer}

	count, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if count != 2 {
		t.Fatalf("enqueued count = %d, want 2", count)
	}
	if len(writer.enqueued) != 2 {
		t.Fatalf("writer received %d intents, want 2", len(writer.enqueued))
	}
	first := writer.enqueued[0]
	if first.Domain != reducer.DomainEshuSearchDocument {
		t.Errorf("domain = %q, want eshu_search_document", first.Domain)
	}
	if first.EntityKey != "eshu_search_document:git-repository-scope:repository:r_a" {
		t.Errorf("entity key = %q", first.EntityKey)
	}
	if first.GenerationID != "gen-a" {
		t.Errorf("generation = %q, want gen-a", first.GenerationID)
	}
	if first.SourceSystem != "github" {
		t.Errorf("source system = %q, want github", first.SourceSystem)
	}
}

func TestSearchDocumentSweeperEmptyDoesNotEnqueue(t *testing.T) {
	t.Parallel()

	writer := &fakeSweepIntentWriter{}
	sweeper := SearchDocumentProjectionSweeper{Pending: &fakePendingLister{}, Intents: writer}
	count, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if count != 0 || len(writer.enqueued) != 0 {
		t.Fatalf("expected no enqueue, got count=%d enqueued=%d", count, len(writer.enqueued))
	}
}

func TestSearchDocumentSweeperPropagatesListerError(t *testing.T) {
	t.Parallel()

	sweeper := SearchDocumentProjectionSweeper{
		Pending: &fakePendingLister{err: errors.New("boom")},
		Intents: &fakeSweepIntentWriter{},
	}
	if _, err := sweeper.RunOnce(context.Background()); err == nil {
		t.Fatal("expected lister error to propagate")
	}
}

func TestSearchDocumentSweeperNilDependencies(t *testing.T) {
	t.Parallel()

	if count, err := (SearchDocumentProjectionSweeper{}).RunOnce(context.Background()); err != nil || count != 0 {
		t.Fatalf("nil-dependency RunOnce = (%d, %v), want (0, nil)", count, err)
	}
}
