// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type advisoryAuthorityProbe struct {
	tx               *fakeTx
	staged           bool
	completed        bool
	published        bool
	completeErr      error
	allowPublish     bool
	acceptedFacts    int
	publishErrors    int
	publishCalls     int
	publishMutations []workflow.ClaimMutation
}

func (p *advisoryAuthorityProbe) RecordAcceptedPublicAdvisoryFacts(_ context.Context, tx db.ExecQueryer,
	receiptID string, batch []facts.Envelope,
) error {
	if !p.staged || receiptID != "receipt-1" || tx != p.tx || p.tx.committed {
		return errors.New("accepted advisory facts not bound to staged transaction")
	}
	p.acceptedFacts += len(batch)
	return nil
}

func (p *advisoryAuthorityProbe) StagePublicAdvisoryGeneration(_ context.Context, tx db.ExecQueryer,
	_ workflow.ClaimMutation, _, _ string,
) (string, error) {
	if tx != p.tx || p.tx.committed {
		return "", errors.New("stage outside fact transaction")
	}
	p.staged = true
	return "receipt-1", nil
}

func (p *advisoryAuthorityProbe) RecordCompletePublicAdvisoryGeneration(_ context.Context, tx db.ExecQueryer,
	receiptID, _, _ string,
) error {
	if !p.staged || p.acceptedFacts != 1 || receiptID != "receipt-1" || tx != p.tx || p.tx.committed {
		return errors.New("complete outside staged fact transaction")
	}
	p.completed = true
	return p.completeErr
}

func (p *advisoryAuthorityProbe) PublishCompletePublicAdvisoryGeneration(_ context.Context,
	mutation workflow.ClaimMutation, _, _ string,
) (bool, error) {
	if !p.completed || !p.tx.committed {
		return false, errors.New("published before complete fact commit")
	}
	p.published = true
	p.publishCalls++
	p.publishMutations = append(p.publishMutations, mutation)
	if p.publishCalls <= p.publishErrors {
		return false, errors.New("transient publication failure")
	}
	return p.allowPublish, nil
}

func TestPublicAdvisoryPublicationRetriesSameClaimThenTerminalizes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		failures     int
		wantTerminal bool
	}{
		{name: "recovers in claim", failures: 2},
		{name: "exhausts claim", failures: 3, wantTerminal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe := &advisoryAuthorityProbe{
				tx: &fakeTx{committed: true}, completed: true,
				allowPublish: true, publishErrors: tc.failures,
			}
			published, err := publishPublicAdvisoryWithRetry(context.Background(), probe,
				workflow.ClaimMutation{WorkItemID: "work", ClaimID: "claim", FencingToken: 7, OwnerID: "owner"},
				"scope", "generation")
			if probe.publishCalls != 3 || published == tc.wantTerminal {
				t.Fatalf("publish calls=%d published=%t err=%v", probe.publishCalls, published, err)
			}
			for _, seen := range probe.publishMutations {
				if seen.WorkItemID != "work" || seen.ClaimID != "claim" || seen.FencingToken != 7 || seen.OwnerID != "owner" {
					t.Fatalf("publication changed claim identity: %+v", seen)
				}
			}
			var terminal interface {
				TerminalFailure() bool
				FailureClass() string
			}
			if tc.wantTerminal {
				if !errors.As(err, &terminal) || !terminal.TerminalFailure() ||
					terminal.FailureClass() != "public_advisory_publish_exhausted" {
					t.Fatalf("publication error=%v, want terminal exhaustion", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicAdvisoryGenerationStagesAndCompletesBeforeSeparatePublication(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	scopeValue := scope.IngestionScope{
		ScopeID: "vuln-intel://kev", ScopeKind: scope.KindVulnerabilityIntelligence,
		CollectorKind: scope.CollectorVulnerabilityIntelligence,
		SourceSystem:  string(scope.CollectorVulnerabilityIntelligence), PartitionKey: "kev",
	}
	generation := scope.ScopeGeneration{
		GenerationID: "advisory-generation-1", ScopeID: scopeValue.ScopeID,
		ObservedAt: now, IngestedAt: now, Status: scope.GenerationStatusPending,
		TriggerKind: scope.TriggerKindSnapshot,
	}
	mutation := workflow.ClaimMutation{
		WorkItemID: "advisory-work-1", ClaimID: "advisory-claim-1", FencingToken: 1,
		OwnerID: "producer-1", ObservedAt: now, LeaseDuration: time.Minute,
	}
	stream := testfixtures.FactChannel([]facts.Envelope{{
		FactID: "advisory-fact-1", ScopeID: scopeValue.ScopeID,
		GenerationID: generation.GenerationID, FactKind: "vulnerability.cve",
		StableFactKey: "cve-1", ObservedAt: now,
		Payload: map[string]any{"id": "CVE-2026-12345"},
	}})
	probe := &advisoryAuthorityProbe{tx: database.tx, allowPublish: true}
	if err := store.CommitClaimedPublicAdvisoryGeneration(context.Background(), mutation,
		scopeValue, generation, stream, nil, probe); err != nil {
		t.Fatal(err)
	}
	if !probe.staged || !probe.completed || !probe.published || probe.acceptedFacts != 1 {
		t.Fatalf("advisory stages: staged=%t complete=%t published=%t accepted=%d", probe.staged, probe.completed, probe.published, probe.acceptedFacts)
	}
}

func TestPublicAdvisoryGenerationDoesNotPublishPartialOrUnapprovedReceipt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		completeErr  error
		allowPublish bool
	}{
		{name: "incomplete manifest rolls back", completeErr: errors.New("incomplete")},
		{name: "no approved receipt fails closed", allowPublish: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := &fakeTransactionalDB{tx: &fakeTx{}}
			store := NewIngestionStore(database)
			store.Now = func() time.Time { return now }
			scopeValue := scope.IngestionScope{
				ScopeID: "vuln-intel://kev", ScopeKind: scope.KindVulnerabilityIntelligence,
				CollectorKind: scope.CollectorVulnerabilityIntelligence,
				SourceSystem:  string(scope.CollectorVulnerabilityIntelligence), PartitionKey: "kev",
			}
			generation := scope.ScopeGeneration{
				GenerationID: "advisory-generation-1", ScopeID: scopeValue.ScopeID,
				ObservedAt: now, IngestedAt: now, Status: scope.GenerationStatusPending,
				TriggerKind: scope.TriggerKindSnapshot,
			}
			mutation := workflow.ClaimMutation{
				WorkItemID: "advisory-work-1", ClaimID: "advisory-claim-1", FencingToken: 1,
				OwnerID: "producer-1", ObservedAt: now, LeaseDuration: time.Minute,
			}
			stream := testfixtures.FactChannel([]facts.Envelope{{
				FactID: "advisory-fact-1", ScopeID: scopeValue.ScopeID,
				GenerationID: generation.GenerationID, FactKind: "vulnerability.cve",
				StableFactKey: "cve-1", ObservedAt: now,
				Payload: map[string]any{"id": "CVE-2026-12345"},
			}})
			probe := &advisoryAuthorityProbe{
				tx: database.tx, completeErr: tc.completeErr,
				allowPublish: tc.allowPublish,
			}
			if err := store.CommitClaimedPublicAdvisoryGeneration(context.Background(), mutation,
				scopeValue, generation, stream, nil, probe); err == nil {
				t.Fatal("public advisory commit accepted incomplete or unapproved receipt")
			}
			if tc.completeErr != nil && (database.tx.committed || probe.published) {
				t.Fatal("incomplete manifest committed or published")
			}
		})
	}
}
