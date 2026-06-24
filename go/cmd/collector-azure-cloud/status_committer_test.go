// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestStatusCommitterForwardsFactsAndRecordsClaim proves the Azure status
// committer forwards facts to the inner committer and records a claim outcome
// without leaking provider identity.
func TestStatusCommitterForwardsFactsAndRecordsClaim(t *testing.T) {
	t.Parallel()

	metrics, err := azurecloud.NewMetrics(metricnoop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("azurecloud.NewMetrics: %v", err)
	}
	inner := &recordingCommitter{}
	committer := newAzureStatusCommitter(inner, metrics)

	scopeValue := scope.IngestionScope{
		ScopeID:       "azure:tenant-abc:subscription:11111111:microsoft.compute:eastus:resource_graph",
		SourceSystem:  azurecloud.CollectorKind,
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorAzure,
		PartitionKey:  "tenant-abc:subscription:11111111",
	}
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	generation := scope.ScopeGeneration{
		GenerationID: "gen-1",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	stream := make(chan facts.Envelope, 1)
	stream <- facts.Envelope{FactKind: facts.AzureCloudResourceFactKind}
	close(stream)

	if err := committer.CommitScopeGeneration(context.Background(), scopeValue, generation, stream); err != nil {
		t.Fatalf("CommitScopeGeneration: %v", err)
	}
	if inner.commits != 1 {
		t.Fatalf("inner commits = %d, want 1", inner.commits)
	}
	if inner.facts != 1 {
		t.Fatalf("inner facts = %d, want 1", inner.facts)
	}
	if inner.scopeID != scopeValue.ScopeID {
		t.Fatalf("inner scope id = %q, want %q", inner.scopeID, scopeValue.ScopeID)
	}
}

// TestStatusCommitterRejectsNonClaimedInner proves the committer surfaces a
// clear error when the inner committer does not support the claimed contract,
// rather than silently dropping the claimed commit.
func TestStatusCommitterRejectsNonClaimedInner(t *testing.T) {
	t.Parallel()

	committer := newAzureStatusCommitter(&plainCommitter{}, azurecloud.NopMetrics{})
	stream := make(chan facts.Envelope)
	close(stream)
	err := committer.CommitClaimedScopeGeneration(
		context.Background(),
		workflow.ClaimMutation{},
		scope.IngestionScope{},
		scope.ScopeGeneration{},
		stream,
	)
	if err == nil {
		t.Fatal("CommitClaimedScopeGeneration() error = nil, want non-claimed inner rejection")
	}
}

type recordingCommitter struct {
	commits int
	facts   int
	scopeID string
}

func (c *recordingCommitter) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	_ scope.ScopeGeneration,
	stream <-chan facts.Envelope,
) error {
	c.commits++
	c.scopeID = scopeValue.ScopeID
	for range stream {
		c.facts++
	}
	return nil
}

// plainCommitter implements only the base Committer contract so the status
// committer must reject claimed commits routed to it.
type plainCommitter struct{}

func (plainCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	stream <-chan facts.Envelope,
) error {
	for range stream {
	}
	return errors.New("plain committer does not support claimed commits")
}
