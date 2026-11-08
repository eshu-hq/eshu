// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// testGHSAWindowScope is the 2026-11-01 reviewed window used across the
// policy cells: [2026-11-01T00:00:00Z, 2026-11-02T00:00:00Z).
const testGHSAWindowScope = "vuln-intel://ghsa/reviewed/_window/1793491200000000-1793577600000000"

func testGHSAWindowSource(t *testing.T, policy ClaimCommitPolicy) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		ClaimCommitPolicy:   policy,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

func testGHSAWindowPolicy(t *testing.T) *GHSAWindowCommitPolicy {
	t.Helper()
	policy, err := NewGHSAWindowCommitPolicy("scorecard-instance", nil, nil)
	if err != nil {
		t.Fatalf("NewGHSAWindowCommitPolicy() error = %v", err)
	}
	return policy
}

func ghsaSnapshotEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind:      "vulnerability.source_snapshot",
		StableFactKey: "vulnerability.source_snapshot:ghsa",
		Payload:       payload,
	}
}

// TestGHSAWindowPolicyArmsCursorWriterAndWriteOnce is the S3c commit
// cell at the host seam: applying the policy to a collected window
// generation arms the cursor writer, marks the generation write-once,
// and clears any trusted-deletion attestation the satellite sent (a
// window claim never attests deletion). With no policy configured, a
// GHSA window claim fails unarmed instead of committing cursorless.
func TestGHSAWindowPolicyArmsCursorWriterAndWriteOnce(t *testing.T) {
	t.Parallel()

	source := testGHSAWindowSource(t, testGHSAWindowPolicy(t))
	item := testWorkItem()
	item.ScopeID = testGHSAWindowScope
	result := completeResult(item)
	result.Generation.TrustedDeletion = true
	envelopes := []facts.Envelope{ghsaSnapshotEnvelope(map[string]any{
		"source": "ghsa", "complete": true, "covered_through": "2026-11-02T00:00:00Z",
	})}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected); err != nil {
		t.Fatalf("applyCommitPolicy() error = %v, want armed", err)
	}
	if collected.StateWriter == nil {
		t.Fatal("StateWriter = nil, want the GHSA cursor writer armed")
	}
	if !collected.Generation.WriteOnce {
		t.Fatal("Generation.WriteOnce = false, want true for a window generation")
	}
	if collected.Generation.TrustedDeletion {
		t.Fatal("Generation.TrustedDeletion = true, want cleared: a window claim never attests deletion")
	}

	unarmed := testGHSAWindowSource(t, nil)
	collected = unarmed.collectedGeneration(item, result, envelopes, "")
	err := unarmed.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected)
	var failure extensionFailure
	if !errors.As(err, &failure) || failure.class != FailureClassGHSAWindowUnarmed || failure.terminal {
		t.Fatalf("applyCommitPolicy() without a policy = %v, want a retryable %s refusal", err, FailureClassGHSAWindowUnarmed)
	}
}

// TestGHSAWindowPolicyDecisions drives the policy directly over raw
// snapshot payloads: the host reads covered_through from the raw payload
// (NVD verdict-key precedent), so these cells pin its exact validation.
func TestGHSAWindowPolicyDecisions(t *testing.T) {
	t.Parallel()
	policy := testGHSAWindowPolicy(t)
	item := testWorkItem()
	item.ScopeID = testGHSAWindowScope
	observedAt := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)

	decide := func(payload map[string]any) (ClaimCommitDecision, string) {
		t.Helper()
		var envelopes []facts.Envelope
		if payload != nil {
			envelopes = []facts.Envelope{ghsaSnapshotEnvelope(payload)}
		}
		decision, err := policy.CommitPolicy(item, observedAt, envelopes)
		if err == nil {
			return decision, ""
		}
		var refusal *CommitPolicyRefusal
		if !errors.As(err, &refusal) {
			t.Fatalf("CommitPolicy() error = %v, want a CommitPolicyRefusal", err)
		}
		if refusal.Terminal {
			t.Fatalf("refusal %s is terminal, want retryable: nothing a retry cannot fix may burn the claim", refusal.Class)
		}
		return decision, refusal.Class
	}

	t.Run("full window arms writer and write-once", func(t *testing.T) {
		t.Parallel()
		decision, class := decide(map[string]any{"source": "ghsa", "complete": true, "covered_through": "2026-11-02T00:00:00Z"})
		if class != "" || !decision.Handled || !decision.WriteOnce || decision.StateWriter == nil {
			t.Fatalf("decision = %+v class %q, want handled write-once with a writer", decision, class)
		}
		recorder := &recordingCommitExecutor{}
		if err := decision.StateWriter(context.Background(), recorder); err != nil {
			t.Fatalf("StateWriter() error = %v", err)
		}
		if len(recorder.queries) != 1 || len(recorder.args) != 1 {
			t.Fatalf("StateWriter() ran %d statements, want one cursor advance", len(recorder.queries))
		}
		args := recorder.args[0]
		if args[0] != "scorecard-instance" || args[1] != "reviewed" || args[2] != int64(1793577600000000) {
			t.Fatalf("StateWriter() args = %v, want (instance, reviewed, window end micros)", args)
		}
	})

	t.Run("prefix arms to the cut", func(t *testing.T) {
		t.Parallel()
		decision, class := decide(map[string]any{"source": "ghsa", "complete": false, "covered_through": "2026-11-01T08:00:00Z"})
		if class != "" || decision.StateWriter == nil {
			t.Fatalf("decision = %+v class %q, want an armed prefix", decision, class)
		}
		recorder := &recordingCommitExecutor{}
		if err := decision.StateWriter(context.Background(), recorder); err != nil {
			t.Fatalf("StateWriter() error = %v", err)
		}
		if got := recorder.args[0][2]; got != int64(1793520000000000) {
			t.Fatalf("prefix cursor = %v, want the cut 1793520000000000", got)
		}
	})

	refusals := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "cannot arm without a snapshot", payload: nil, want: FailureClassGHSAWindowHeld},
		{name: "cannot arm without covered_through", payload: map[string]any{"source": "ghsa", "complete": true}, want: FailureClassGHSAWindowHeld},
		{name: "cannot arm on a non-RFC3339 covered_through", payload: map[string]any{"covered_through": "tomorrow"}, want: FailureClassGHSAWindowHeld},
		{name: "cannot arm on a non-string covered_through", payload: map[string]any{"covered_through": 1793577600}, want: FailureClassGHSAWindowHeld},
		{name: "cache replay never advances", payload: map[string]any{"covered_through": "2026-11-02T00:00:00Z", "cache_mode": "offline"}, want: FailureClassGHSACacheReplay},
		{name: "no progress at the window start", payload: map[string]any{"covered_through": "2026-11-01T00:00:00Z"}, want: FailureClassGHSAWindowNoProgress},
		{name: "no progress before the window start", payload: map[string]any{"covered_through": "2026-10-31T00:00:00Z"}, want: FailureClassGHSAWindowNoProgress},
		{name: "out of range past the window end", payload: map[string]any{"covered_through": "2026-11-03T00:00:00Z"}, want: FailureClassGHSACoveredThroughOutOfRange},
	}
	for _, cell := range refusals {
		t.Run(cell.name, func(t *testing.T) {
			t.Parallel()
			decision, class := decide(cell.payload)
			if class != cell.want {
				t.Fatalf("refusal class = %q, want %q (commit nothing)", class, cell.want)
			}
			if decision.StateWriter != nil {
				t.Fatal("refused decision still armed a state writer")
			}
		})
	}

	t.Run("refresh cache mode still advances", func(t *testing.T) {
		t.Parallel()
		decision, class := decide(map[string]any{"covered_through": "2026-11-02T00:00:00Z", "cache_mode": "refresh"})
		if class != "" || decision.StateWriter == nil {
			t.Fatalf("decision = %+v class %q, want an armed refresh", decision, class)
		}
	})

	t.Run("non-window scopes are not handled", func(t *testing.T) {
		t.Parallel()
		other := testWorkItem()
		decision, err := policy.CommitPolicy(other, observedAt, nil)
		if err != nil || decision.Handled {
			t.Fatalf("CommitPolicy(non-window) = (%+v, %v), want unhandled", decision, err)
		}
		anchor := testWorkItem()
		anchor.ScopeID = "vuln-intel://ghsa/reviewed"
		decision, err = policy.CommitPolicy(anchor, observedAt, nil)
		if err != nil || decision.Handled {
			t.Fatalf("CommitPolicy(anchor) = (%+v, %v), want unhandled", decision, err)
		}
	})
}
