// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"slices"
	"testing"
)

type fixedLookup map[Anchor]struct{}

func (f fixedLookup) ExistingAnchors(context.Context, []Anchor) (map[Anchor]struct{}, error) {
	return f, nil
}

// TestCheckReportsMissingAnchors covers all present, one missing, and the
// sorted distinct anchor set Check hands the lookup.
func TestCheckReportsMissingAnchors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		existing fixedLookup
		wantMiss []Anchor
	}{
		{name: "all present", existing: fixedLookup{prod: {}, stage: {}}},
		{name: "one missing", existing: fixedLookup{prod: {}}, wantMiss: []Anchor{stage}},
		{name: "none present", existing: fixedLookup{}, wantMiss: []Anchor{prod, stage}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := Check(context.Background(), tc.existing, []Anchor{stage, prod, stage})
			if err != nil {
				t.Fatalf("Check() error = %v", err)
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
