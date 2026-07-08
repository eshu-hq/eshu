// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func runProofProjectorCycle(t *testing.T, db *proofDomainDB, now time.Time) {
	t.Helper()
	runProofProjectorCycleWithInjector(t, db, now, nil)
}

func runProofProjectorCycleWithInjector(
	t *testing.T,
	db *proofDomainDB,
	now time.Time,
	retryInjector projector.RetryInjector,
) {
	t.Helper()
	runProofProjectorCycleWithWriters(
		t,
		db,
		now,
		retryInjector,
		&recordingCanonicalWriter{},
		&recordingContentWriter{},
	)
}

func runProofProjectorCycleWithWriters(
	t *testing.T,
	db *proofDomainDB,
	now time.Time,
	retryInjector projector.RetryInjector,
	canonicalWriter *recordingCanonicalWriter,
	contentWriter *recordingContentWriter,
) {
	t.Helper()

	if canonicalWriter == nil {
		canonicalWriter = &recordingCanonicalWriter{}
	}
	if contentWriter == nil {
		contentWriter = &recordingContentWriter{}
	}

	projectorQueue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Second,
		Now:           func() time.Time { return now },
	}
	projectorService := projector.Service{
		PollInterval: time.Millisecond,
		WorkSource:   projectorQueue,
		FactStore:    NewFactStore(db),
		Runner: projector.Runtime{
			CanonicalWriter: canonicalWriter,
			ContentWriter:   contentWriter,
			IntentWriter:    ReducerQueue{db: db, LeaseOwner: "reducer-1", LeaseDuration: time.Minute, Now: func() time.Time { return now }},
			RetryInjector:   retryInjector,
		},
		WorkSink: projectorQueue,
		Wait:     func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := projectorService.Run(context.Background()); err != nil {
		t.Fatalf("projector service Run() error = %v, want nil", err)
	}
}

func proofRepositoryFacts(
	scopeID string,
	generationID string,
	factID string,
	digest string,
	observedAt time.Time,
) []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:        factID,
			ScopeID:       scopeID,
			GenerationID:  generationID,
			FactKind:      "repository",
			StableFactKey: "repository:" + factID,
			ObservedAt:    observedAt,
			Payload: map[string]any{
				"graph_id":   "repo-123",
				"graph_kind": "repository",
				"name":       "eshu",
				"digest":     digest,
				"repo_id":    "repo-123",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      factID,
			},
		},
	}
}
