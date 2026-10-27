// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// #8827: security_alert_reconciliation and supply_chain_impact are enqueued
// from the same scope generation. The reconciliation can only match an alert
// once an impact finding exists, so a reconciliation that runs while the
// impact work is still in flight must wait WITHOUT spending an attempt, and
// the handler's own give-up must be visible instead of a silent unmatched
// write. These tests drive the real handler with the real manifest bridge;
// the queue-side half (the class freezing attempt_count on the real queue) is
// pinned in go/internal/storage/postgres.

type stubImpactProducerReadiness struct {
	state securityalert.ImpactProducerState
	err   error
	calls int
}

func (s *stubImpactProducerReadiness) ImpactProducerState(
	_ context.Context,
	_ string,
	_ string,
) (securityalert.ImpactProducerState, error) {
	s.calls++
	return s.state, s.err
}

var impactFenceNow = time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

// pendingImpactFenceHandler builds a handler whose alert has owned dependency
// evidence and no impact finding, the exact shape that defers.
func pendingImpactFenceHandler(
	readiness securityalert.ImpactProducerReadiness,
	logs *bytes.Buffer,
) (securityalert.SecurityAlertReconciliationHandler, *recordingSecurityAlertReconciliationWriter) {
	repoID := "repo://github/acme/api"
	packageID := "npm://registry.npmjs.org/pending-provider-lib"
	loader := &recordingSecurityAlertReconciliationFactLoader{
		scopeFacts: []facts.Envelope{
			securityAlertEnvelope("alert-impact-fence", repoID, map[string]any{
				"provider":              "github_dependabot",
				"provider_alert_number": int64(15),
				"provider_state":        "open",
				"package_id":            packageID,
				"package_name":          "pending-provider-lib",
				"ecosystem":             "npm",
				"manifest_path":         "package-lock.json",
				"cve_ids":               []string{"CVE-2026-9680"},
				"ghsa_ids":              []string{"GHSA-synthetic-9680"},
				"vulnerable_range":      "<8.9.1",
				"patched_version":       "8.9.1",
			}),
		},
		manifestFacts: []facts.Envelope{
			packageManifestDependencyFactWithMetadata(
				repoID, "api", "package-lock.json", "pending-provider-lib", "npm", "8.9.0",
				time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
				map[string]any{
					"section": "package-lock", "lockfile": true,
					"dependency_path": []any{"root", "pending-provider-lib"}, "dependency_depth": 2,
					"direct_dependency": false,
				},
			),
		},
	}
	writer := &recordingSecurityAlertReconciliationWriter{}
	handler := securityalert.SecurityAlertReconciliationHandler{
		FactLoader:                  loader,
		Writer:                      writer,
		ExtractManifestConsumptions: correlation.ExtractSecurityAlertManifestConsumptions,
		ImpactProducerReadiness:     readiness,
		Now:                         func() time.Time { return impactFenceNow },
	}
	if logs != nil {
		handler.Logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return handler, writer
}

func impactFenceIntent(attempt int) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-impact-fence",
		ScopeID:      "repo://github/acme/api",
		GenerationID: "security-alert-generation-1",
		SourceSystem: "security_alert",
		Domain:       DomainSecurityAlertReconciliation,
		Cause:        "provider alert observed",
		AttemptCount: attempt,
	}
}

func impactFenceFailureClass(err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}

// TestSecurityAlertReconciliationDefersWithoutSpendingAnAttemptWhileImpactInFlight
// is the #8827 regression. With the impact work item pending for the same
// scope generation, the reconciliation must return the non-counting
// impact-in-flight class at every attempt, including the attempt the handler
// used to give up on (3): giving up there writes unmatched rows although
// impact evidence is about to exist.
func TestSecurityAlertReconciliationDefersWithoutSpendingAnAttemptWhileImpactInFlight(t *testing.T) {
	t.Parallel()

	for _, attempt := range []int{1, 2, 3, 7} {
		readiness := &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{
			Pending: 1, Total: 1, NewestInFlightWorkableAt: impactFenceNow.Add(-2 * time.Minute),
		}}
		handler, writer := pendingImpactFenceHandler(readiness, nil)

		_, err := handler.Handle(context.Background(), impactFenceIntent(attempt))
		if err == nil {
			t.Fatalf("attempt %d: Handle() error = nil, want the non-counting impact-in-flight deferral", attempt)
		}
		if got, want := impactFenceFailureClass(err), securityalert.ImpactNotReadyFailureClass; got != want {
			t.Fatalf("attempt %d: failure class = %q, want %q (a counting class spends the attempt budget)", attempt, got, want)
		}
		if retryable, ok := err.(interface{ Retryable() bool }); !ok || !retryable.Retryable() {
			t.Fatalf("attempt %d: error %T %v must be retryable", attempt, err, err)
		}
		if !reducercontract.IsExpectedWait(err) {
			t.Fatalf("attempt %d: error %T %v must be an expected wait", attempt, err, err)
		}
		if writer.calls != 0 {
			t.Fatalf("attempt %d: writer calls = %d, want 0 while impact is in flight", attempt, writer.calls)
		}
		if readiness.calls != 1 {
			t.Fatalf("attempt %d: readiness probes = %d, want 1", attempt, readiness.calls)
		}
	}
}

// TestSecurityAlertReconciliationFallsBackToTheCountingWaitPastTheProducerBound
// keeps the wait bounded: a producer row with no sign of life for
// ProducerReadinessMaxWait stops holding the reconciliation, which then
// follows the pre-existing counting path (defer below the cap, write at it).
func TestSecurityAlertReconciliationFallsBackToTheCountingWaitPastTheProducerBound(t *testing.T) {
	t.Parallel()

	stale := impactFenceNow.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)
	readiness := &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{
		Retrying: 1, Total: 1, NewestInFlightWorkableAt: stale,
	}}
	handler, writer := pendingImpactFenceHandler(readiness, nil)

	_, err := handler.Handle(context.Background(), impactFenceIntent(1))
	if got, want := impactFenceFailureClass(err), "security_alert_reconciliation_waiting_for_impact"; got != want {
		t.Fatalf("attempt 1 past the bound: failure class = %q, want the counting %q", got, want)
	}
	if writer.calls != 0 {
		t.Fatalf("attempt 1 past the bound: writer calls = %d, want 0", writer.calls)
	}

	result, err := handler.Handle(context.Background(), impactFenceIntent(3))
	if err != nil {
		t.Fatalf("attempt 3 past the bound: Handle() error = %v, want the give-up write", err)
	}
	if writer.calls != 1 {
		t.Fatalf("attempt 3 past the bound: writer calls = %d, want 1", writer.calls)
	}
	if !strings.Contains(result.EvidenceSummary, "impact_wait=gave_up_bound_exceeded") {
		t.Fatalf("summary %q must name the gave_up_bound_exceeded give-up reason", result.EvidenceSummary)
	}
}

// TestSecurityAlertReconciliationGiveUpIsVisible covers the #8826-review
// finding: with no impact work in flight the handler still stops waiting at its
// cap and writes unmatched rows. That write must carry a bounded reason on the
// result and a WARN log naming the scope and attempt.
func TestSecurityAlertReconciliationGiveUpIsVisible(t *testing.T) {
	t.Parallel()

	readiness := &stubImpactProducerReadiness{state: securityalert.ImpactProducerState{Failed: 1, Total: 1}}
	var logs bytes.Buffer
	handler, writer := pendingImpactFenceHandler(readiness, &logs)

	_, err := handler.Handle(context.Background(), impactFenceIntent(1))
	if got, want := impactFenceFailureClass(err), "security_alert_reconciliation_waiting_for_impact"; got != want {
		t.Fatalf("attempt 1 with terminal impact: failure class = %q, want counting %q", got, want)
	}

	result, err := handler.Handle(context.Background(), impactFenceIntent(3))
	if err != nil {
		t.Fatalf("attempt 3: Handle() error = %v, want the give-up write", err)
	}
	if writer.calls != 1 {
		t.Fatalf("attempt 3: writer calls = %d, want 1", writer.calls)
	}
	if !strings.Contains(result.EvidenceSummary, "impact_wait=gave_up_unmatched") {
		t.Fatalf("summary %q must name the gave_up_unmatched reason", result.EvidenceSummary)
	}
	if got := result.SubSignals["impact_wait_gave_up"]; got != 1 {
		t.Fatalf("sub signal impact_wait_gave_up = %v, want 1: the reducer logs it as sub_signal_impact_wait_gave_up on the success line", got)
	}
	for _, want := range []string{
		`"level":"WARN"`,
		"security alert reconciliation gave up waiting for impact evidence",
		`"scope_id":"repo://github/acme/api"`,
		`"attempt_count":3`,
		`"impact_wait_outcome":"gave_up_unmatched"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("give-up log missing %q:\n%s", want, logs.String())
		}
	}
}

// TestSecurityAlertReconciliationProbeErrorIsCountingAndNeverAWait pins that a
// store that cannot answer is a real failure: it must not be converted into the
// non-counting class, which would retry forever without surfacing.
func TestSecurityAlertReconciliationProbeErrorIsCountingAndNeverAWait(t *testing.T) {
	t.Parallel()

	readiness := &stubImpactProducerReadiness{err: errors.New("connection refused")}
	handler, writer := pendingImpactFenceHandler(readiness, nil)

	_, err := handler.Handle(context.Background(), impactFenceIntent(1))
	if err == nil {
		t.Fatal("Handle() error = nil, want the probe failure")
	}
	if got := impactFenceFailureClass(err); got == securityalert.ImpactNotReadyFailureClass {
		t.Fatalf("probe failure classified %q: a failed probe must never read as a non-counting wait", got)
	}
	if reducercontract.IsExpectedWait(err) {
		t.Fatalf("probe failure %v must not be an expected wait", err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 on a probe failure", writer.calls)
	}
}

// TestSecurityAlertReconciliationProbesOncePerImpactProducingPass proves the
// fence costs exactly one early probe per impact-producing pass (#9053 reads
// settlement before the load on every attempt, so even a pass with nothing
// waiting on impact pays the probe), and no probe at all when the intent
// cannot produce impact evidence.
func TestSecurityAlertReconciliationProbesOncePerImpactProducingPass(t *testing.T) {
	t.Parallel()

	readiness := &stubImpactProducerReadiness{}
	handler, _ := pendingImpactFenceHandler(readiness, nil)
	handler.FactLoader = &recordingSecurityAlertReconciliationFactLoader{}

	if _, err := handler.Handle(context.Background(), impactFenceIntent(1)); err != nil {
		t.Fatalf("Handle() error = %v, want a clean write with no alerts", err)
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness probes = %d, want exactly 1 early probe per impact-producing pass", readiness.calls)
	}

	idle := &stubImpactProducerReadiness{}
	idleHandler, _ := pendingImpactFenceHandler(idle, nil)
	idleHandler.FactLoader = &recordingSecurityAlertReconciliationFactLoader{}
	idleIntent := impactFenceIntent(1)
	idleIntent.SourceSystem = "unrelated"
	idleIntent.Cause = "unrelated evidence observed"
	if _, err := idleHandler.Handle(context.Background(), idleIntent); err != nil {
		t.Fatalf("Handle() error = %v, want a clean write for a non-impact-producing intent", err)
	}
	if idle.calls != 0 {
		t.Fatalf("readiness probes = %d, want 0 when the intent cannot produce impact evidence", idle.calls)
	}
}

// TestSecurityAlertReconciliationImpactWaitCounterRecordsEveryOutcome pins the
// closed outcome set of eshu_dp_security_alert_reconciliation_impact_wait_total:
// one count per decision, labeled by what the handler did.
func TestSecurityAlertReconciliationImpactWaitCounterRecordsEveryOutcome(t *testing.T) {
	t.Parallel()

	stale := impactFenceNow.Add(-crossscope.ProducerReadinessMaxWait - time.Minute)
	cases := []struct {
		outcome string
		attempt int
		state   securityalert.ImpactProducerState
	}{
		{securityalert.ImpactWaitDeferredInFlight, 1, securityalert.ImpactProducerState{Pending: 1, Total: 1}},
		{securityalert.ImpactWaitGaveUpUnmatched, 3, securityalert.ImpactProducerState{Failed: 1, Total: 1}},
		{securityalert.ImpactWaitProducerSettled, 3, securityalert.ImpactProducerState{Succeeded: 1, Total: 1}},
		{securityalert.ImpactWaitGaveUpBoundExceeded, 3, securityalert.ImpactProducerState{Retrying: 1, Total: 1, NewestInFlightWorkableAt: stale}},
	}
	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			inst, err := telemetry.NewInstruments(provider.Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			handler, _ := pendingImpactFenceHandler(&stubImpactProducerReadiness{state: tc.state}, nil)
			handler.Instruments = inst
			_, _ = handler.Handle(context.Background(), impactFenceIntent(tc.attempt))

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			var got int64
			for _, scope := range rm.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name != "eshu_dp_security_alert_reconciliation_impact_wait_total" {
						continue
					}
					sum, ok := m.Data.(metricdata.Sum[int64])
					if !ok {
						t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
					}
					for _, dp := range sum.DataPoints {
						outcome, _ := dp.Attributes.Value("outcome")
						domain, _ := dp.Attributes.Value("domain")
						if outcome.AsString() == tc.outcome && domain.AsString() == "security_alert_reconciliation" {
							got += dp.Value
						}
					}
				}
			}
			if got != 1 {
				t.Fatalf("impact_wait_total{outcome=%q} = %d, want 1", tc.outcome, got)
			}
		})
	}
}
