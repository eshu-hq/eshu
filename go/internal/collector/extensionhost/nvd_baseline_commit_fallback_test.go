// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBaselineRefusalHoldsWithNoWiredCommitPolicy pins the no-wired-policy
// fallback in applyCommitPolicy (#10762): a baseline scope carrying a window
// advance verdict is still refused retryable under the same class, and arms
// no state writer, when the host has no commit policy at all. Only the
// outcome telemetry needs the wired policy; the refusal does not.
func TestBaselineRefusalHoldsWithNoWiredCommitPolicy(t *testing.T) {
	t.Parallel()

	source := testNVDBaselineSource(t, nil)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowVerdictPayload(true))}
	collected := source.collectedGeneration(item, result, envelopes, "")
	err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected)
	if err == nil {
		t.Fatal("applyCommitPolicy() error = nil with no wired policy for a baseline window verdict, want a fail-closed refusal")
	}
	failure, ok := err.(extensionFailure)
	if !ok {
		t.Fatalf("applyCommitPolicy() error = %v (%T), want extensionFailure", err, err)
	}
	if failure.class != FailureClassNVDBaselineWindowWriterArmed {
		t.Fatalf("refusal class = %q, want %q", failure.class, FailureClassNVDBaselineWindowWriterArmed)
	}
	if failure.terminal {
		t.Fatal("refusal terminal = true, want retryable: the next plan replays the same chunk")
	}
	if collected.StateWriter != nil {
		t.Fatal("StateWriter != nil on a refused baseline generation with no wired policy")
	}
}

// TestBaselineCheckpointArmsWithNoWiredCommitPolicy pins the other half of
// the fallback: an advancing baseline verdict arms the checkpoint writer
// through the built-in baseline decision even with no wired policy.
func TestBaselineCheckpointArmsWithNoWiredCommitPolicy(t *testing.T) {
	t.Parallel()

	source := testNVDBaselineSource(t, nil)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDBaselineVerdictPayload())}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected); err != nil {
		t.Fatalf("applyCommitPolicy() error = %v, want the checkpoint armed", err)
	}
	if collected.StateWriter == nil {
		t.Fatal("StateWriter = nil for an advancing baseline verdict with no wired policy, want the checkpoint writer armed")
	}
}
