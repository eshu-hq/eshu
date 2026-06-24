// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedServiceCompletesMetadataTooLargeWithoutRetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 28, 15, 0, 0, 0, time.UTC)
	target := TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "npmjs",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      "package-registry://npmjs/npm/oversized",
			Packages:     []string{"oversized"},
			PackageLimit: 1,
			VersionLimit: 1,
		},
		MetadataURL: "https://registry.npmjs.org/oversized",
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-package-registry",
		Targets:             []TargetConfig{target},
		Provider: failingMetadataProvider{
			err: newMetadataTooLargeError(maxMetadataDocumentBytes),
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	item := testPackageRegistryWorkItemForScope("package-registry://npmjs/npm/oversized")
	item.SourceSystem = string(scope.CollectorPackageRegistry)
	item.AcceptanceUnitID = item.ScopeID
	store := &oversizedClaimStore{
		item:  item,
		claim: oversizedClaim(now),
		found: true,
		onComplete: func() {
			cancel()
		},
	}
	committer := &oversizedClaimCommitter{}
	service := collector.ClaimedService{
		ControlStore:        store,
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "collector-package-registry",
		OwnerID:             "collector-package-registry-owner",
		ClaimIDFunc:         func() string { return "claim-package-registry-1" },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		MaxAttempts:         2,
		Clock:               func() time.Time { return now },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if store.retryableFailCalls != 0 {
		t.Fatalf("FailClaimRetryable calls = %d, want 0 for deterministic too-large metadata", store.retryableFailCalls)
	}
	if store.terminalFailCalls != 0 {
		t.Fatalf("FailClaimTerminal calls = %d, want 0 because too-large metadata is coverage-gap evidence", store.terminalFailCalls)
	}
	if store.completeCalls != 1 {
		t.Fatalf("CompleteClaim calls = %d, want 1", store.completeCalls)
	}
	if committer.claimedCalls != 1 {
		t.Fatalf("CommitClaimedScopeGeneration calls = %d, want 1 warning generation", committer.claimedCalls)
	}
	if got := committer.factKindCounts[facts.PackageRegistryWarningFactKind]; got != 1 {
		t.Fatalf("committed warning facts = %d, want 1", got)
	}
}

func oversizedClaim(now time.Time) workflow.Claim {
	return workflow.Claim{
		ClaimID:        "claim-package-registry-1",
		WorkItemID:     "package-registry-work-item-1",
		FencingToken:   42,
		OwnerID:        "collector-package-registry-owner",
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now,
		HeartbeatAt:    now,
		LeaseExpiresAt: now.Add(time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

type oversizedClaimStore struct {
	item               workflow.WorkItem
	claim              workflow.Claim
	found              bool
	completeCalls      int
	retryableFailCalls int
	terminalFailCalls  int
	onComplete         func()
}

func (s *oversizedClaimStore) ClaimNextEligible(
	context.Context,
	workflow.ClaimSelector,
	time.Time,
	time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	if !s.found {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	s.found = false
	return s.item, s.claim, true, nil
}

func (s *oversizedClaimStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *oversizedClaimStore) CompleteClaim(context.Context, workflow.ClaimMutation) error {
	s.completeCalls++
	if s.onComplete != nil {
		s.onComplete()
	}
	return nil
}

func (s *oversizedClaimStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *oversizedClaimStore) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	s.retryableFailCalls++
	return nil
}

func (s *oversizedClaimStore) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	s.terminalFailCalls++
	return nil
}

type oversizedClaimCommitter struct {
	claimedCalls   int
	factKindCounts map[string]int
}

func (c *oversizedClaimCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	return errors.New("generic commit should not be used")
}

func (c *oversizedClaimCommitter) CommitClaimedScopeGeneration(
	_ context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factsStream <-chan facts.Envelope,
) error {
	c.claimedCalls++
	if c.factKindCounts == nil {
		c.factKindCounts = map[string]int{}
	}
	for envelope := range factsStream {
		c.factKindCounts[envelope.FactKind]++
	}
	return nil
}
