// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// captureCommitter records the scope, generation, and facts of each committed
// generation and cancels the run context once the configured number of
// generations have been committed, so collector.Service.Run terminates.
type captureCommitter struct {
	cancel       context.CancelFunc
	stopAfter    int
	committed    int
	scopes       []scope.IngestionScope
	generations  []scope.ScopeGeneration
	factsByScope map[string][]facts.Envelope
}

func newCaptureCommitter(cancel context.CancelFunc, stopAfter int) *captureCommitter {
	return &captureCommitter{
		cancel:       cancel,
		stopAfter:    stopAfter,
		factsByScope: make(map[string][]facts.Envelope),
	}
}

func (c *captureCommitter) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if err := generationValue.ValidateForScope(scopeValue); err != nil {
		return err
	}
	var collected []facts.Envelope
	for env := range factStream {
		collected = append(collected, env)
	}
	c.scopes = append(c.scopes, scopeValue)
	c.generations = append(c.generations, generationValue)
	c.factsByScope[scopeValue.ScopeID] = collected
	c.committed++
	if c.committed >= c.stopAfter {
		c.cancel()
	}
	return nil
}

func TestServiceCommitsFactsAndGenerationState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	committer := newCaptureCommitter(cancel, 1)
	provider := NewFixturePageProvider(twoPageFixture(), azurecloud.ScopeAccess{})
	service := collector.Service{
		Source: &Source{
			Config: Config{
				CollectorInstanceID: "azure-collector-1",
				PollInterval:        time.Millisecond,
				Targets:             []TargetConfig{testTarget()},
			},
			ProviderFactory: StaticFixtureFactory(provider),
			Clock:           fixedClock(),
		},
		Committer:    committer,
		PollInterval: time.Millisecond,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("service run: %v", err)
	}
	if committer.committed != 1 {
		t.Fatalf("committed %d generations, want 1", committer.committed)
	}
	scopeValue := committer.scopes[0]
	if scopeValue.CollectorKind != scope.CollectorAzure {
		t.Fatalf("committed collector kind = %q, want azure", scopeValue.CollectorKind)
	}
	generationValue := committer.generations[0]
	if generationValue.ScopeID != scopeValue.ScopeID {
		t.Fatalf("generation scope id %q != scope id %q", generationValue.ScopeID, scopeValue.ScopeID)
	}
	committedFacts := committer.factsByScope[scopeValue.ScopeID]
	if len(factsOfKind(committedFacts, facts.AzureCloudResourceFactKind)) != 2 {
		t.Fatalf("committed %d resource facts, want 2", len(committedFacts))
	}
	for _, env := range committedFacts {
		if env.ScopeID != scopeValue.ScopeID || env.GenerationID != generationValue.GenerationID {
			t.Fatalf("fact scope/generation mismatch: %s/%s", env.ScopeID, env.GenerationID)
		}
	}
}
