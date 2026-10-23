// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// #8779: the bootstrap-index projector is the second production Project
// caller. It must carry the union base id out of the load onto the generation
// it projects, or every delta over its #7797 union fails the fact boundary
// exactly as the projector service did.

const (
	bootstrapUnionDeltaID = "gen-8779-delta-b"
	bootstrapUnionBaseID  = "gen-8779-base-a"
)

// unionFactStore returns one union-shaped load: the delta's own rows plus a
// base row for the untouched other.go, naming the base as the load would.
type unionFactStore struct {
	facts []facts.Envelope
}

func (s *unionFactStore) LoadProjectionFacts(context.Context, projector.ScopeGenerationWork) (projector.ProjectionFacts, error) {
	return projector.ProjectionFacts{
		Facts: s.facts, UnionBaseGenerationID: bootstrapUnionBaseID, UnionOutcome: projector.DeltaUnionBaseFound,
	}, nil
}

type bootstrapCanonicalRecorder struct {
	mu    sync.Mutex
	calls []canonical.CanonicalMaterialization
}

func (r *bootstrapCanonicalRecorder) Write(_ context.Context, mat canonical.CanonicalMaterialization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, mat)
	return nil
}

type bootstrapContentRecorder struct{}

func (bootstrapContentRecorder) Write(context.Context, content.Materialization) (content.Result, error) {
	return content.Result{}, nil
}

type bootstrapIntentRecorder struct{}

func (bootstrapIntentRecorder) Enqueue(_ context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error) {
	return runtime.IntentResult{Count: len(intents)}, nil
}

// recordingFailSink counts acks and keeps the first Fail cause.
type recordingFailSink struct {
	mu     sync.Mutex
	acked  int
	failed error
}

func (s *recordingFailSink) Ack(context.Context, projector.ScopeGenerationWork, runtime.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked++
	return nil
}

func (s *recordingFailSink) Fail(_ context.Context, _ projector.ScopeGenerationWork, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = err
	}
	return nil
}

func TestDrainProjectorProjectsDeltaOverUnionBase(t *testing.T) {
	t.Parallel()
	repo, err := repositoryidentity.MetadataFor("bootstrap-union-8779", "/repos/bootstrap-union-8779",
		"https://github.com/eshu-proof/bootstrap-union-8779.git")
	if err != nil {
		t.Fatalf("repository identity: %v", err)
	}
	scopeID := "git-repository-scope:" + repo.ID
	baseAt := time.Date(2026, time.October, 22, 11, 0, 0, 0, time.UTC)
	deltaAt := baseAt.Add(time.Hour)
	work := projector.ScopeGenerationWork{
		Scope: scope.IngestionScope{
			ScopeID: scopeID, SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: repo.ID,
			Metadata: map[string]string{"repo_id": repo.ID, "local_path": repo.LocalPath},
		},
		Generation: scope.ScopeGeneration{
			GenerationID: bootstrapUnionDeltaID, ScopeID: scopeID, ObservedAt: deltaAt, IngestedAt: deltaAt,
			Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		},
	}
	fileFact := func(generationID string, observed time.Time, rel string) facts.Envelope {
		return gitcontent.FileFactEnvelope(repo.LocalPath, repo.ID, scopeID, generationID, observed,
			map[string]any{"path": repo.LocalPath + "/" + rel, "language": "go"}, false)
	}
	store := &unionFactStore{facts: []facts.Envelope{
		fileFact(bootstrapUnionBaseID, baseAt, "other.go"),
		gitcontent.RepositoryFactEnvelope(repo.LocalPath, repo, "run-delta", scopeID, bootstrapUnionDeltaID, deltaAt, 1,
			nil, false, "main", nil, true, []string{"keep.go"}, nil, false, false), // delta, reconcile, emptyScope
		fileFact(bootstrapUnionDeltaID, deltaAt, "keep.go"),
	}}
	canonicalWriter := &bootstrapCanonicalRecorder{}
	runner := runtime.Runtime{
		CanonicalWriter: canonicalWriter, ContentWriter: bootstrapContentRecorder{}, IntentWriter: bootstrapIntentRecorder{},
	}
	sink := &recordingFailSink{}

	if err := drainProjector(
		context.Background(),
		&concurrentWorkSource{items: []projector.ScopeGenerationWork{work}}, store, runner, sink,
		passBootstrapBaselineFence{}, passBootstrapBaselineFence{}, nil, 0,
		1, nil, nil, nil,
	); err != nil {
		t.Fatalf("drainProjector() error = %v (fail cause: %v), want nil", err, sink.failed)
	}
	if sink.acked != 1 || sink.failed != nil {
		t.Fatalf("acked = %d, fail cause = %v; want the delta projected over its union base and acked", sink.acked, sink.failed)
	}
	if len(canonicalWriter.calls) != 1 {
		t.Fatalf("canonical writes = %d, want 1", len(canonicalWriter.calls))
	}
	var files []string
	for _, file := range canonicalWriter.calls[0].Files {
		files = append(files, file.RelativePath)
	}
	sort.Strings(files)
	if got, want := strings.Join(files, ","), "keep.go,other.go"; got != want {
		t.Fatalf("canonical files = %s, want %s", got, want)
	}
}
