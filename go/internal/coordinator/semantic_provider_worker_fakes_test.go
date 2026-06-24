// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

const semanticWorkerScope = "repository:eshu"

// fakeSemanticClaimer is a lease-fenced in-memory claimer for worker tests. It
// records the order of operations so tests can prove the egress gate runs before
// any dispatch terminal.
type fakeSemanticClaimer struct {
	mu          sync.Mutex
	pending     []semanticqueue.Record
	skipped     []semanticqueue.Record
	deadLetter  []semanticqueue.Record
	succeeded   []semanticqueue.Record
	claimErr    error
	skipReasons []string
}

func (f *fakeSemanticClaimer) ClaimNext(
	_ context.Context,
	_ string,
	_ string,
	_ time.Time,
	_ time.Duration,
) (semanticqueue.Record, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return semanticqueue.Record{}, false, f.claimErr
	}
	if len(f.pending) == 0 {
		return semanticqueue.Record{}, false, nil
	}
	record := f.pending[0]
	f.pending = f.pending[1:]
	record.Status = semanticqueue.StatusClaimed
	return record, true, nil
}

func (f *fakeSemanticClaimer) SkipClaimByPolicy(
	_ context.Context,
	record semanticqueue.Record,
	_ string,
	_ time.Time,
	reasonCode string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skipped = append(f.skipped, record)
	f.skipReasons = append(f.skipReasons, reasonCode)
	return nil
}

func (f *fakeSemanticClaimer) SucceedClaim(
	_ context.Context,
	record semanticqueue.Record,
	_ string,
	_ time.Time,
	responseHash string,
	_ semanticqueue.BudgetDecision,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	record.ResponseHash = responseHash
	f.succeeded = append(f.succeeded, record)
	return nil
}

func (f *fakeSemanticClaimer) DeadLetterClaim(
	_ context.Context,
	record semanticqueue.Record,
	_ string,
	_ time.Time,
	failure semanticqueue.Failure,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	record.Failure = failure
	f.deadLetter = append(f.deadLetter, record)
	return nil
}

// fakeSemanticProviderClient records whether Dispatch was ever called so tests
// can prove the no-network and gate-first invariants.
type fakeSemanticProviderClient struct {
	enabled       bool
	dispatchCalls atomic.Int64
	responseHash  string
	dispatchErr   error
}

func (c *fakeSemanticProviderClient) Enabled() bool { return c.enabled }

func (c *fakeSemanticProviderClient) Dispatch(
	_ context.Context,
	_ SemanticDispatchRequest,
) (SemanticDispatchResult, error) {
	c.dispatchCalls.Add(1)
	if c.dispatchErr != nil {
		return SemanticDispatchResult{}, c.dispatchErr
	}
	return SemanticDispatchResult{ResponseHash: c.responseHash}, nil
}

func semanticWorkerRecord(profileID, sourceClass string) semanticqueue.Record {
	return semanticqueue.Record{
		JobID:                "semantic-job:" + profileID + ":" + sourceClass,
		WorkItemID:           "semantic-work:" + profileID + ":" + sourceClass,
		Fingerprint:          "fingerprint-" + profileID,
		ScopeID:              semanticWorkerScope,
		GenerationID:         "generation-1",
		SourceClass:          sourceClass,
		ProviderKind:         "semantic-docs",
		ProviderProfileID:    profileID,
		ProviderProfileClass: "managed",
		Status:               semanticqueue.StatusPending,
		ProviderJob:          true,
	}
}

func semanticWorkerEgressPolicy(decision string) semanticpolicy.Policy {
	return semanticpolicy.Policy{
		PolicyID: "policy-1",
		Enabled:  true,
		Egress: semanticpolicy.EgressPolicy{
			Mode: semanticpolicy.EgressModeRestricted,
			SemanticProviders: []semanticpolicy.EgressProviderRule{{
				ProviderProfileID: "semantic-docs-default",
				SourceClasses:     []string{semanticprofile.SourceDocumentation},
				Decision:          decision,
			}},
		},
		Rules: []semanticpolicy.Rule{{
			RuleID:            "rule-1",
			ProviderProfileID: "semantic-docs-default",
			SourceClasses:     []string{semanticprofile.SourceDocumentation},
			Scopes:            []semanticpolicy.Scope{{Kind: semanticpolicy.ScopeRepository, ID: "eshu"}},
			SourceAllowlist:   []semanticpolicy.SourceSelector{{Kind: semanticpolicy.SourceSelectorAll}},
			Settings: semanticpolicy.Settings{
				Limits:    semanticpolicy.Limits{MaxChunkBytes: 1024, MaxTokensPerChunk: 256, MaxDailyTokens: 1000},
				Redaction: semanticpolicy.Redaction{Mode: semanticpolicy.RedactionStrict},
				Retention: semanticpolicy.Retention{Posture: semanticpolicy.RetentionMetadataOnly, Prompt: semanticpolicy.RetentionNone, Response: semanticpolicy.RetentionHashOnly},
			},
		}},
	}
}

func newSemanticWorker(
	claimer *fakeSemanticClaimer,
	client SemanticProviderClient,
	audit GovernanceAuditAppender,
	cfg SemanticProviderWorkerConfig,
	logs *bytes.Buffer,
) SemanticProviderWorker {
	cfg.Enabled = true
	cfg.LeaseOwner = "semantic-worker-1"
	cfg.LeaseTTL = time.Minute
	cfg.MaxClaimsPerPass = 16
	cfg.ScopeIDs = []string{semanticWorkerScope}
	worker := SemanticProviderWorker{
		Config:          cfg,
		Claimer:         claimer,
		Client:          client,
		GovernanceAudit: audit,
		Clock:           func() time.Time { return time.Date(2026, time.June, 11, 12, 0, 0, 0, time.UTC) },
	}
	if logs != nil {
		worker.Logger = slog.New(slog.NewTextHandler(logs, nil))
	}
	return worker
}

// BenchmarkSemanticWorkerEgressGatedClaimLoop measures the per-claim egress-gate
// and terminal-disposition cost with the default no-network client. It is the
// no-regression baseline for the claim loop: no provider traffic occurs, so the
// measurement isolates the gate plus lifecycle write overhead.
func BenchmarkSemanticWorkerEgressGatedClaimLoop(b *testing.B) {
	policy := semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionAllow)
	template := semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation)
	for i := 0; i < b.N; i++ {
		claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{template}}
		worker := newSemanticWorker(claimer, DisabledSemanticProviderClient{}, nil, SemanticProviderWorkerConfig{
			ExecutionEnabled: true,
			Policy:           policy,
		}, nil)
		if err := worker.Run(context.Background()); err != nil {
			b.Fatalf("Run() error = %v, want nil", err)
		}
	}
}
