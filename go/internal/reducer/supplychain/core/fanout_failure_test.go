// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"testing"
)

// TestFanOutFailuresAreRetryableAndSelfClassified is the contract with the
// queue: it dead-letters any error that does not say it is retryable, so a failed
// holder read, reopen, or state update must carry Retryable() and a failure
// class, or the pass would be dead-lettered on its first transient failure
// instead of retried up to the attempt budget.
func TestFanOutFailuresAreRetryableAndSelfClassified(t *testing.T) {
	t.Parallel()

	cases := map[string]*stubFanOut{
		"holder read":   {holders: []string{"holder-a"}, findErr: errors.New("holder read failed")},
		"holder reopen": {holders: []string{"holder-a"}, reopenErr: map[string]error{"holder-a": errors.New("reopen failed")}},
		"state update":  {holders: []string{"holder-a"}, stateFailures: []int{1}},
	}
	for name, port := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
			intent := fanOutIntent(1, nil)
			if name == "state update" {
				// A stamped intent makes the only fan-out work the stamp consume.
				intent = fanOutIntent(1, map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope": "src"}})
			}
			_, err := fanOutHandler(port, writer, nil).Handle(context.Background(), intent)
			if err == nil {
				t.Fatal("Handle() error = nil, want the fan-out failure")
			}
			var retryable interface{ Retryable() bool }
			if !errors.As(err, &retryable) || !retryable.Retryable() {
				t.Fatalf("error %v does not report Retryable() == true: the queue would dead-letter the pass", err)
			}
			var classified interface{ FailureClass() string }
			if !errors.As(err, &classified) || classified.FailureClass() != SupplyChainImpactFanOutFailureClass {
				t.Fatalf("error %v failure class is not %q", err, SupplyChainImpactFanOutFailureClass)
			}
		})
	}
}
