// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"reflect"
	"testing"
)

// TestEC2InstanceIdentityLockedWriterNilGateWritesThrough proves a nil
// LockOnlyGate preserves prior (ungated) behavior, mirroring
// TestLockOnlyGateNilGateWritesThrough for the sibling posture writers.
func TestEC2InstanceIdentityLockedWriterNilGateWritesThrough(t *testing.T) {
	t.Parallel()

	var got []map[string]any
	underlying := func(_ context.Context, rows []map[string]any, _, _, _ string) error {
		got = rows
		return nil
	}
	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}}

	var gate *LockOnlyGate
	w := NewEC2InstanceIdentityLockedWriter(gate, underlying, nil)
	if err := w.WriteEC2InstanceIdentityNodes(context.Background(), rows, "scope-1", "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("WriteEC2InstanceIdentityNodes error = %v", err)
	}
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("pass-through nil gate altered rows: got %v", got)
	}
}

// TestEC2InstanceIdentityLockedWriterRetractPassesThroughUnwrapped proves
// Retract is forwarded unchanged, never lock-gated (see the LockOnlyGate doc
// comment on why retraction has no row-level uid set to lock ahead of time).
func TestEC2InstanceIdentityLockedWriterRetractPassesThroughUnwrapped(t *testing.T) {
	t.Parallel()

	var gotScopeIDs []string
	var gotGenerationID, gotEvidenceSource string
	retract := func(_ context.Context, scopeIDs []string, generationID string, evidenceSource string) error {
		gotScopeIDs = scopeIDs
		gotGenerationID = generationID
		gotEvidenceSource = evidenceSource
		return nil
	}

	w := NewEC2InstanceIdentityLockedWriter(nil, nil, retract)
	if err := w.RetractEC2InstanceIdentityNodes(context.Background(), []string{"scope-1"}, "gen-1", "reducer/ec2-instance-identity"); err != nil {
		t.Fatalf("RetractEC2InstanceIdentityNodes error = %v", err)
	}
	if !reflect.DeepEqual(gotScopeIDs, []string{"scope-1"}) {
		t.Fatalf("retract scopeIDs = %v, want [scope-1]", gotScopeIDs)
	}
	if gotGenerationID != "gen-1" || gotEvidenceSource != "reducer/ec2-instance-identity" {
		t.Fatalf("retract generationID/evidenceSource = %q/%q", gotGenerationID, gotEvidenceSource)
	}
}

// TestEC2InstanceIdentityLockedWriterSatisfiesReducerInterface is a
// compile-time guarantee that the gated wrapper satisfies the reducer-owned
// EC2InstanceIdentityNodeWriter consumer interface.
func TestEC2InstanceIdentityLockedWriterSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	var _ interface {
		WriteEC2InstanceIdentityNodes(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractEC2InstanceIdentityNodes(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
	} = NewEC2InstanceIdentityLockedWriter(nil, nil, nil)
}
