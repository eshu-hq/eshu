// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimRetryDelayAndBudgetContract pins the exported retry arithmetic
// ClaimedService and the extension host's source-state recorder share
// (#9420): the applied delay is the larger of poll interval and request, and
// the budget trips once AttemptCount reaches a positive MaxAttempts.
func TestClaimRetryDelayAndBudgetContract(t *testing.T) {
	t.Parallel()

	delays := []struct {
		poll, requested, want time.Duration
	}{
		{poll: time.Minute, requested: 0, want: time.Minute},
		{poll: time.Minute, requested: 2 * time.Minute, want: 2 * time.Minute},
		{poll: 5 * time.Minute, requested: 2 * time.Minute, want: 5 * time.Minute},
	}
	for _, tc := range delays {
		if got := ClaimRetryDelay(tc.poll, tc.requested); got != tc.want {
			t.Fatalf("ClaimRetryDelay(%s, %s) = %s, want %s", tc.poll, tc.requested, got, tc.want)
		}
	}
	budgets := []struct {
		maxAttempts, attempt int
		want                 bool
	}{
		{maxAttempts: 0, attempt: 99, want: false},
		{maxAttempts: 3, attempt: 2, want: false},
		{maxAttempts: 3, attempt: 3, want: true},
		{maxAttempts: 3, attempt: 4, want: true},
	}
	for _, tc := range budgets {
		item := workflow.WorkItem{AttemptCount: tc.attempt}
		if got := ClaimAttemptBudgetExhausted(tc.maxAttempts, item); got != tc.want {
			t.Fatalf("ClaimAttemptBudgetExhausted(%d, attempt %d) = %t, want %t", tc.maxAttempts, tc.attempt, got, tc.want)
		}
	}
}
