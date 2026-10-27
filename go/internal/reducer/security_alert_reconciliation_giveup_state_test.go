// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
// no sub signal, no WARN line). Since #9053 the settled write happens on the
// first run; the cap-attempt write below pins the same shape for a pass that
// reaches the cap directly.
func TestSecurityAlertReconciliationSettledProducerIsNotAGiveUp(t *testing.T) {
	t.Parallel()

	readiness := &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{Succeeded: 1, Total: 1}}
	var logs bytes.Buffer
	handler, writer := pendingImpactFenceHandler(readiness, &logs)

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

// impactCommitsDuringLoadLoader simulates the supply_chain_impact work writing
// its finding and acking succeeded right after the reconciliation loaded its
// evidence: the loaded evidence has no finding, but any producer probe taken
// after the load reads the generation as settled.
type impactCommitsDuringLoadLoader struct {
	*recordingSecurityAlertReconciliationFactLoader
	loaded bool
}

func (l *impactCommitsDuringLoadLoader) ListActiveSecurityAlertReconciliationFacts(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	envelopes, err := l.recordingSecurityAlertReconciliationFactLoader.ListActiveSecurityAlertReconciliationFacts(ctx, filter)
	l.loaded = true
	return envelopes, err
}

// phasedImpactReadiness answers running before the evidence load and settled
// after it.
type phasedImpactReadiness struct {
	loader *impactCommitsDuringLoadLoader
	calls  int
}

func (r *phasedImpactReadiness) ImpactProducerState(context.Context, string, string) (securityalert.ImpactProducerState, error) {
	r.calls++
	if r.loader.loaded {
		return securityalert.ImpactProducerState{Succeeded: 1, Total: 1}, nil
	}
	return securityalert.ImpactProducerState{
		Running: 1, Total: 1, NewestInFlightWorkableAt: impactFenceNow.Add(-time.Minute),
	}, nil
}

// TestSecurityAlertReconciliationReadsSettlementBeforeTheEvidenceLoad is the
// #8956 review TOCTOU: on the cap attempt the impact work can commit its
// finding and ack between the evidence load and a later probe. Read after the
// load, settlement would publish the stale unmatched row as a genuine
// no-match. Read before the load, the producer is still running, so the pass
// defers without spending an attempt and writes nothing.
func TestSecurityAlertReconciliationReadsSettlementBeforeTheEvidenceLoad(t *testing.T) {
	t.Parallel()

	handler, writer := pendingImpactFenceHandler(nil, nil)
	loader := &impactCommitsDuringLoadLoader{
		recordingSecurityAlertReconciliationFactLoader: handler.FactLoader.(*recordingSecurityAlertReconciliationFactLoader),
	}
	readiness := &phasedImpactReadiness{loader: loader}
	handler.FactLoader = loader
	handler.ImpactProducerReadiness = readiness

	_, err := handler.Handle(context.Background(), impactFenceIntent(3))
	if got, want := impactFenceFailureClass(err), securityalert.ImpactNotReadyFailureClass; got != want {
		t.Fatalf("cap attempt: failure class = %q (err %v), want %q: settlement read after the load claimed a stale no-match", got, err, want)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0: the stale unmatched row must not be published", writer.calls)
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness probes = %d, want 1 (one probe, taken before the load)", readiness.calls)
	}
}

// TestSecurityAlertReconciliationEarlyProbeErrorStaysCounting keeps the probe
// error rule on the cap attempt, where the probe now runs before the load: a
// store that cannot answer is a counting failure, never the non-counting wait,
// and nothing loads or writes.
func TestSecurityAlertReconciliationEarlyProbeErrorStaysCounting(t *testing.T) {
	t.Parallel()

	readiness := &stubImpactProducerReadiness{err: errors.New("connection refused")}
	handler, writer := pendingImpactFenceHandler(readiness, nil)

	_, err := handler.Handle(context.Background(), impactFenceIntent(3))
	if err == nil {
		t.Fatal("cap attempt: Handle() error = nil, want the probe failure")
	}
	if got := impactFenceFailureClass(err); got == securityalert.ImpactNotReadyFailureClass {
		t.Fatalf("cap attempt probe failure classified %q: it must never read as a non-counting wait", got)
	}
	if reducercontract.IsExpectedWait(err) {
		t.Fatalf("cap attempt probe failure %v must not be an expected wait", err)
	}
	if writer.calls != 0 || readiness.calls != 1 {
		t.Fatalf("writer calls = %d probes = %d, want 0 writes and 1 probe", writer.calls, readiness.calls)
	}
}
