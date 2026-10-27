// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// #9053: once the impact producer has settled (every supply_chain_impact work
// item of the same scope generation succeeded), the answer is already final:
// the reconciliation must write its genuine no-match on the first run instead
// of spending its two counting defers on a wait nothing can change. Every
// other producer state keeps today's behavior.

func TestSecurityAlertReconciliationSettledProducerWritesOnTheFirstAttempt(t *testing.T) {
	t.Parallel()

	for _, attempt := range []int{1, 2} {
		t.Run(fmt.Sprintf("attempt-%d", attempt), func(t *testing.T) {
			t.Parallel()
			readiness := &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{Succeeded: 2, Total: 2}}
			var logs bytes.Buffer
			handler, writer := pendingImpactFenceHandler(readiness, &logs)

			result, err := handler.Handle(context.Background(), impactFenceIntent(attempt))
			if err != nil {
				t.Fatalf("attempt %d with a settled producer: Handle() error = %v, want the genuine no-match write", attempt, err)
			}
			if writer.calls != 1 || len(writer.write.Decisions) != 1 {
				t.Fatalf("attempt %d: writer calls = %d decisions = %d, want one write of one decision", attempt, writer.calls, len(writer.write.Decisions))
			}
			if got := writer.write.Decisions[0].Status; got != securityalert.SecurityAlertReconciliationUnmatched {
				t.Fatalf("attempt %d: decision status = %q, want unmatched", attempt, got)
			}
			if got := writer.write.ImpactWaitOutcome; got != "" {
				t.Fatalf("attempt %d: write ImpactWaitOutcome = %q, want empty (genuine no-match carries none)", attempt, got)
			}
			if strings.Contains(result.EvidenceSummary, "impact_wait=") {
				t.Fatalf("attempt %d: summary %q must not name a give-up for a settled producer", attempt, result.EvidenceSummary)
			}
			if _, ok := result.SubSignals["impact_wait_gave_up"]; ok {
				t.Fatalf("attempt %d: sub signals %v must not carry impact_wait_gave_up for a settled producer", attempt, result.SubSignals)
			}
			for _, want := range []string{
				`"level":"INFO"`,
				`"impact_wait_outcome":"producer_settled"`,
			} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("attempt %d: settled log missing %q:\n%s", attempt, want, logs.String())
				}
			}
			if readiness.calls != 1 {
				t.Fatalf("attempt %d: readiness probes = %d, want 1 (the early probe before the load)", attempt, readiness.calls)
			}
		})
	}
}

// TestSecurityAlertReconciliationUnsettledProducerKeepsTodaysWait pins the
// #9053 negative space: every producer state that is not settled behaves
// exactly as before — in flight defers without spending an attempt at any
// attempt, and never-enqueued, dead-lettered, failed, or superseded work takes
// the counting path (defer below the cap, visible give-up at it).
func TestSecurityAlertReconciliationUnsettledProducerKeepsTodaysWait(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		state      securityalert.ImpactProducerState
		deferClass string
		giveUp     string
	}{
		{
			name:       "producer pending",
			state:      securityalert.ImpactProducerState{Pending: 1, Total: 1, NewestInFlightWorkableAt: impactFenceNow.Add(-2 * time.Minute)},
			deferClass: securityalert.ImpactNotReadyFailureClass,
		},
		{
			name:       "producer never enqueued",
			state:      securityalert.ImpactProducerState{},
			deferClass: "security_alert_reconciliation_waiting_for_impact",
			giveUp:     securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name:       "producer dead-lettered",
			state:      securityalert.ImpactProducerState{Failed: 1, Total: 1},
			deferClass: "security_alert_reconciliation_waiting_for_impact",
			giveUp:     securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name:       "producer failed",
			state:      securityalert.ImpactProducerState{Failed: 2, Total: 2},
			deferClass: "security_alert_reconciliation_waiting_for_impact",
			giveUp:     securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name:       "producer superseded",
			state:      securityalert.ImpactProducerState{Succeeded: 1, Total: 2},
			deferClass: "security_alert_reconciliation_waiting_for_impact",
			giveUp:     securityalert.ImpactWaitGaveUpUnmatched,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			readiness := &stubImpactProducerReadiness{state: tc.state}
			handler, writer := pendingImpactFenceHandler(readiness, nil)

			_, err := handler.Handle(context.Background(), impactFenceIntent(1))
			if got := impactFenceFailureClass(err); got != tc.deferClass {
				t.Fatalf("attempt 1: failure class = %q (err %v), want %q", got, err, tc.deferClass)
			}
			if writer.calls != 0 {
				t.Fatalf("attempt 1: writer calls = %d, want 0 before the cap", writer.calls)
			}

			if tc.giveUp == "" {
				_, err := handler.Handle(context.Background(), impactFenceIntent(3))
				if got := impactFenceFailureClass(err); got != tc.deferClass {
					t.Fatalf("attempt 3 in flight: failure class = %q (err %v), want the non-counting %q", got, err, tc.deferClass)
				}
				if writer.calls != 0 {
					t.Fatalf("attempt 3 in flight: writer calls = %d, want 0 while impact is in flight", writer.calls)
				}
				return
			}
			result, err := handler.Handle(context.Background(), impactFenceIntent(3))
			if err != nil {
				t.Fatalf("attempt 3: Handle() error = %v, want the give-up write", err)
			}
			if writer.calls != 1 {
				t.Fatalf("attempt 3: writer calls = %d, want 1", writer.calls)
			}
			if got := writer.write.ImpactWaitOutcome; got != tc.giveUp {
				t.Fatalf("attempt 3: write ImpactWaitOutcome = %q, want %q", got, tc.giveUp)
			}
			if !strings.Contains(result.EvidenceSummary, "impact_wait="+tc.giveUp) {
				t.Fatalf("attempt 3: summary %q must name the %s reason", result.EvidenceSummary, tc.giveUp)
			}
		})
	}
}
