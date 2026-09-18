// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"slices"
	"testing"
	"time"
)

type fixedLookup map[Anchor]struct{}

func (f fixedLookup) ExistingAnchors(context.Context, []Anchor) (map[Anchor]struct{}, error) {
	return f, nil
}

// TestEvaluateDefersUntilTheBoundThenCommits covers the three answers: all
// anchors present (no defer), one missing inside the bound (defer), one
// missing past the bound (commit), plus the zero-anchor case that must keep
// deferring rather than read as infinitely elapsed.
func TestEvaluateDefersUntilTheBoundThenCommits(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cases := []struct {
		name      string
		existing  fixedLookup
		cycle     time.Time
		wantDefer bool
		wantMiss  []Anchor
	}{
		{name: "all present", existing: fixedLookup{prod: {}, stage: {}}, cycle: now, wantDefer: false},
		{name: "missing inside bound", existing: fixedLookup{prod: {}}, cycle: now.Add(-time.Minute), wantDefer: true, wantMiss: []Anchor{stage}},
		{name: "missing past bound", existing: fixedLookup{prod: {}}, cycle: now.Add(-MaxWait - time.Minute), wantDefer: false, wantMiss: []Anchor{stage}},
		{name: "unknown cycle anchor", existing: fixedLookup{}, cycle: time.Time{}, wantDefer: true, wantMiss: []Anchor{prod, stage}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := Evaluate(context.Background(), tc.existing, []Anchor{stage, prod, stage}, tc.cycle, now)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if decision.Defer != tc.wantDefer {
				t.Fatalf("Defer = %v, want %v", decision.Defer, tc.wantDefer)
			}
			if !slices.Equal(decision.Missing, tc.wantMiss) {
				t.Fatalf("Missing = %v, want %v", decision.Missing, tc.wantMiss)
			}
			if !slices.Equal(decision.Anchors, []Anchor{prod, stage}) {
				t.Fatalf("Anchors = %v, want sorted distinct [prod stage]", decision.Anchors)
			}
		})
	}
}

// TestNotReadyErrorIsARetryableReadinessClass pins the class the queue
// enrolls as non-counting.
func TestNotReadyErrorIsARetryableReadinessClass(t *testing.T) {
	t.Parallel()
	err := NotReadyError{ScopeID: "s", GenerationID: "g", Missing: 1}
	if !err.Retryable() || err.FailureClass() != "workload_cloud_relationship_instances_not_ready" {
		t.Fatalf("Retryable=%v FailureClass=%q", err.Retryable(), err.FailureClass())
	}
}
