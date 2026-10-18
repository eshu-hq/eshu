// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// TestDecideFirstGenerationRetract covers the one prior-generation predicate
// every first-generation retract skip shares (#8167): only a first attempt
// with a wired check that reports no strictly older generation skips.
func TestDecideFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	checkErr := errors.New("connection refused")
	answer := func(prior bool, err error) PriorGenerationCheck {
		return func(context.Context, string, string) (bool, error) { return prior, err }
	}
	tests := []struct {
		name    string
		check   PriorGenerationCheck
		attempt int
		want    RetractDecision
		wantErr error
	}{
		{
			name: "first generation skips", check: answer(false, nil), attempt: 1,
			want: RetractDecision{Skip: true, Reason: RetractReasonFirstGeneration},
		},
		{
			name: "attempt zero is a first attempt", check: answer(false, nil), attempt: 0,
			want: RetractDecision{Skip: true, Reason: RetractReasonFirstGeneration},
		},
		{
			name: "older generation retracts", check: answer(true, nil), attempt: 1,
			want: RetractDecision{Reason: RetractReasonPriorGeneration},
		},
		{
			name: "retry retracts without asking", check: answer(false, checkErr), attempt: 2,
			want: RetractDecision{Reason: RetractReasonRetryAttempt},
		},
		{
			name: "unwired check retracts", check: nil, attempt: 1,
			want: RetractDecision{Reason: RetractReasonCheckUnwired},
		},
		{
			name: "check error propagates", check: answer(false, checkErr), attempt: 1,
			wantErr: checkErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			intent := Intent{ScopeID: "scope-8167", GenerationID: "gen-8167", AttemptCount: tt.attempt}
			got, err := DecideFirstGenerationRetract(context.Background(), tt.check, intent)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want wrapping %v", err, tt.wantErr)
				}
				if got.Skip {
					t.Fatalf("decision on error = %+v, want no skip", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("decision = %+v, want %+v", got, tt.want)
			}
			if !slices.Contains(RetractReasons(), got.Reason) {
				t.Fatalf("reason %q is not in the closed set %v", got.Reason, RetractReasons())
			}
		})
	}
}

// TestRetractReasonsClosedSet pins the bounded reason vocabulary the handlers
// log and attach to spans.
func TestRetractReasonsClosedSet(t *testing.T) {
	t.Parallel()
	want := []string{
		RetractReasonFirstGeneration,
		RetractReasonPriorGeneration,
		RetractReasonRetryAttempt,
		RetractReasonCheckUnwired,
		RetractReasonRecommit,
		RetractReasonAlreadyCommitted,
	}
	if got := RetractReasons(); !slices.Equal(got, want) {
		t.Fatalf("RetractReasons() = %v, want %v", got, want)
	}
}
