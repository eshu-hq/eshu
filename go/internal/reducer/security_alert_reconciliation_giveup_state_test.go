// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// #8956: the persisted reconciliation write must tell a give-up apart from a
// genuine no-match. A genuine no-match is a pass whose same-generation
// supply_chain_impact work all succeeded (the producer ran and wrote no
// finding for the advisory): the write carries no impact_wait_outcome, so its
// rows stay byte-identical to the pre-#8956 shape. Every other stop at the
// handler's attempt cap is a give-up and carries the closed outcome.

func TestSecurityAlertReconciliationWriteCarriesTheGiveUpOutcome(t *testing.T) {
	t.Parallel()

	stale := impactFenceNow.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)
	cases := []struct {
		name      string
		readiness securityalert.ImpactProducerReadiness
		want      string
	}{
		{
			name:      "producer settled is a genuine no-match",
			readiness: &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{Succeeded: 1, Total: 1}},
			want:      "",
		},
		{
			name:      "producer dead-lettered",
			readiness: &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{Failed: 1, Total: 1}},
			want:      securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name: "one producer row succeeded and one dead-lettered",
			readiness: &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{
				Succeeded: 1, Failed: 1, Total: 2,
			}},
			want: securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name: "producer row in a terminal status that is neither succeeded nor failed",
			readiness: &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{
				Succeeded: 1, Total: 2,
			}},
			want: securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name:      "no impact work was ever enqueued",
			readiness: &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{}},
			want:      securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name:      "fence not wired",
			readiness: nil,
			want:      securityalert.ImpactWaitGaveUpUnmatched,
		},
		{
			name: "in-flight producer past the bound",
			readiness: &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{
				Retrying: 1, Total: 1, NewestInFlightWorkableAt: stale,
			}},
			want: securityalert.ImpactWaitGaveUpBoundExceeded,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler, writer := pendingImpactFenceHandler(tc.readiness, nil)
			if _, err := handler.Handle(context.Background(), impactFenceIntent(3)); err != nil {
				t.Fatalf("attempt 3: Handle() error = %v, want the write", err)
			}
			if writer.calls != 1 {
				t.Fatalf("writer calls = %d, want 1", writer.calls)
			}
			if got := writer.write.ImpactWaitOutcome; got != tc.want {
				t.Fatalf("write ImpactWaitOutcome = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSecurityAlertReconciliationSettledProducerIsNotAGiveUp pins the settled
// pass: it writes the same unmatched decision, records producer_settled on the
// counter at INFO, and raises none of the give-up signals (no summary suffix,
// no sub signal, no WARN line).
func TestSecurityAlertReconciliationSettledProducerIsNotAGiveUp(t *testing.T) {
	t.Parallel()

	readiness := &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{Succeeded: 1, Total: 1}}
	var logs bytes.Buffer
	handler, writer := pendingImpactFenceHandler(readiness, &logs)

	_, err := handler.Handle(context.Background(), impactFenceIntent(1))
	if got, want := impactFenceFailureClass(err), "security_alert_reconciliation_waiting_for_impact"; got != want {
		t.Fatalf("attempt 1 with a settled producer: failure class = %q, want counting %q (retry skipping is out of scope)", got, want)
	}

	result, err := handler.Handle(context.Background(), impactFenceIntent(3))
	if err != nil {
		t.Fatalf("attempt 3: Handle() error = %v, want the write", err)
	}
	if writer.calls != 1 || len(writer.write.Decisions) != 1 {
		t.Fatalf("writer calls = %d decisions = %d, want one write of one decision", writer.calls, len(writer.write.Decisions))
	}
	if got := writer.write.Decisions[0].Status; got != securityalert.SecurityAlertReconciliationUnmatched {
		t.Fatalf("decision status = %q, want unmatched", got)
	}
	if strings.Contains(result.EvidenceSummary, "impact_wait=") {
		t.Fatalf("summary %q must not name a give-up for a settled producer", result.EvidenceSummary)
	}
	if _, ok := result.SubSignals["impact_wait_gave_up"]; ok {
		t.Fatalf("sub signals %v must not carry impact_wait_gave_up for a settled producer", result.SubSignals)
	}
	if strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("settled producer logged a WARN line:\n%s", logs.String())
	}
	for _, want := range []string{
		`"level":"INFO"`,
		`"impact_wait_outcome":"producer_settled"`,
		`"attempt_count":3`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("settled log missing %q:\n%s", want, logs.String())
		}
	}
}

// TestSecurityAlertReconciliationPersistedOutcomesAreClosed pins the closed
// set a persisted row can carry: the two give-up outcomes, never the defer or
// settled outcome.
func TestSecurityAlertReconciliationPersistedOutcomesAreClosed(t *testing.T) {
	t.Parallel()

	got := securityalert.PersistedImpactWaitOutcomes()
	want := []string{securityalert.ImpactWaitGaveUpBoundExceeded, securityalert.ImpactWaitGaveUpUnmatched}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("PersistedImpactWaitOutcomes() = %v, want %v", got, want)
	}
}
