// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"testing"
)

// TestHolderReadRefusedForSupportsReachesTheQueueWithItsOwnClass pins the #11197
// rule from arbiter-b2c decision 3: a holder read that refuses because the
// supports table is not ready must reach the queue with the non-counting class,
// not wrapped in the counting fan-out error. fanOutError reports
// supply_chain_impact_fanout_failed, which counts toward the attempt budget, so
// wrapping the refusal would dead-letter the intent after about three minutes
// against a backfill that takes hours. An ordinary read failure still wraps.
func TestHolderReadRefusedForSupportsReachesTheQueueWithItsOwnClass(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		findErr   error
		wantClass string
	}{
		"supports not ready": {&SupplyChainImpactSupportsNotReadyError{Reader: "fanout_holders"}, SupplyChainImpactSupportsNotReadyFailureClass},
		"ordinary failure":   {errors.New("boom"), SupplyChainImpactFanOutFailureClass},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			port := &stubFanOut{holders: []string{"a"}, findErr: tc.findErr}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
			_, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, nil))
			var classed interface{ FailureClass() string }
			if !errors.As(err, &classed) {
				t.Fatalf("error %v carries no failure class", err)
			}
			if got := classed.FailureClass(); got != tc.wantClass {
				t.Fatalf("failure class = %q, want %q (%v)", got, tc.wantClass, err)
			}
			var retryable interface{ Retryable() bool }
			if !errors.As(err, &retryable) || !retryable.Retryable() {
				t.Fatalf("error %v is not retryable", err)
			}
		})
	}
}
