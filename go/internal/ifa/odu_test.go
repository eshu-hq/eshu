// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestOduCanonicalizesFactsIdempotently(t *testing.T) {
	t.Parallel()

	odu := ifa.Odu{
		Name:  "contract-demo",
		Facts: unorderedFacts(),
	}
	first, err := ifa.CanonicalizeOdu(context.Background(), odu, nil)
	if err != nil {
		t.Fatalf("CanonicalizeOdu() error = %v", err)
	}
	second, err := replay.Canonicalize(first, replay.DefaultCanonicalOptions())
	if err != nil {
		t.Fatalf("re-canonicalize Ifa output: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Ifa canonical output is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if bytes.Contains(first, []byte("run-specific-generation")) {
		t.Fatalf("canonical output retained run-specific generation:\n%s", first)
	}
	if !bytes.Contains(first, []byte(replay.DerivedGenerationID("repo:alpha"))) {
		t.Fatalf("canonical output missing replay-derived generation id:\n%s", first)
	}
}

func TestOduLoadsFactsThroughFactStore(t *testing.T) {
	t.Parallel()

	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID:       "repo:alpha",
			SourceSystem:  "git",
			ScopeKind:     scope.KindRepository,
			CollectorKind: scope.CollectorGit,
			PartitionKey:  "repo:alpha",
		},
		Generation: scope.ScopeGeneration{
			GenerationID: "run-specific-generation",
			ScopeID:      "repo:alpha",
			ObservedAt:   time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC),
			Status:       scope.GenerationStatusCompleted,
		},
	}
	store := &recordingFactStore{facts: unorderedFacts()}
	odu := ifa.Odu{Name: "loaded-contract-demo", Work: &work}

	if _, err := ifa.CanonicalizeOdu(context.Background(), odu, store); err != nil {
		t.Fatalf("CanonicalizeOdu() error = %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("LoadFacts calls = %d, want 1", store.calls)
	}
	if store.work.Scope.ScopeID != work.Scope.ScopeID {
		t.Fatalf("LoadFacts scope = %q, want %q", store.work.Scope.ScopeID, work.Scope.ScopeID)
	}
}

type recordingFactStore struct {
	calls int
	work  projector.ScopeGenerationWork
	facts []facts.Envelope
}

func (s *recordingFactStore) LoadFacts(_ context.Context, work projector.ScopeGenerationWork) ([]facts.Envelope, error) {
	s.calls++
	s.work = work
	return s.facts, nil
}

func unorderedFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:           "fact-b",
			ScopeID:          "repo:alpha",
			GenerationID:     "run-specific-generation",
			FactKind:         "repository",
			StableFactKey:    "repo:alpha:b",
			SchemaVersion:    "1.0.0",
			CollectorKind:    "git",
			SourceConfidence: "observed",
			ObservedAt:       time.Date(2026, 7, 8, 12, 1, 0, 0, time.UTC),
			Payload:          map[string]any{"name": "b"},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				ScopeID:      "repo:alpha",
				GenerationID: "run-specific-generation",
				FactKey:      "repo:alpha:b",
			},
		},
		{
			FactID:           "fact-a",
			ScopeID:          "repo:alpha",
			GenerationID:     "run-specific-generation",
			FactKind:         "file",
			StableFactKey:    "repo:alpha:a",
			SchemaVersion:    "1.0.0",
			CollectorKind:    "git",
			SourceConfidence: "observed",
			ObservedAt:       time.Date(2026, 7, 8, 12, 0, 30, 0, time.UTC),
			Payload:          map[string]any{"path": "a.go"},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				ScopeID:      "repo:alpha",
				GenerationID: "run-specific-generation",
				FactKey:      "repo:alpha:a",
			},
		},
	}
}
