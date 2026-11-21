// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// stubAttachmentReadiness answers the #11105 port with a fixed state or error
// and records how often it was asked.
type stubAttachmentReadiness struct {
	state securityalert.ImpactProducerState
	err   error
	calls int
}

func (s *stubAttachmentReadiness) SBOMAttestationAttachmentState(
	context.Context, string, string,
) (securityalert.ImpactProducerState, error) {
	s.calls++
	return s.state, s.err
}

var attachmentWaitNow = time.Date(2026, 11, 21, 5, 0, 0, 0, time.UTC)

func attachmentWaitIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID: "impact-11105", ScopeID: "scope:11105", GenerationID: "gen-11105",
		Domain: reducercontract.DomainSupplyChainImpact, AttemptCount: 1,
	}
}

// attachmentWaitHandler builds an impact handler whose loader, writer, and
// token issuer record calls, so a deferral can be proven to run before all of
// them.
func attachmentWaitHandler(
	port SBOMAttachmentReadiness,
	inst *telemetry.Instruments,
	logs *bytes.Buffer,
) (SupplyChainImpactHandler, *stubSupplyChainImpactFactLoader, *recordingSupplyChainImpactWriter, *countingIssuer) {
	loader := &stubSupplyChainImpactFactLoader{}
	writer := &recordingSupplyChainImpactWriter{}
	issuer := &countingIssuer{}
	handler := SupplyChainImpactHandler{
		FactLoader: loader, Writer: writer, FencingTokenIssuer: issuer, Instruments: inst,
		AttachmentReadiness: port, Now: func() time.Time { return attachmentWaitNow },
	}
	if logs != nil {
		handler.Logger = slog.New(slog.NewTextHandler(logs, nil))
	}
	return handler, loader, writer, issuer
}

func attachmentWaitOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_attachment_wait_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				domain, _ := dp.Attributes.Value("domain")
				if domain.AsString() != string(reducercontract.DomainSupplyChainImpact) {
					t.Fatalf("domain label = %q, want supply_chain_impact", domain.AsString())
				}
				out[outcome.AsString()] += dp.Value
			}
		}
	}
	return out
}

// TestSupplyChainImpactAttachmentWaitDecisionTable pins the #11105 decision
// table on Handle: which states defer before any token, load, or write, which
// proceed, and which outcome each records.
func TestSupplyChainImpactAttachmentWaitDecisionTable(t *testing.T) {
	t.Parallel()
	recent := attachmentWaitNow.Add(-time.Minute)
	stale := attachmentWaitNow.Add(-crossscope.ProducerReadinessMaxWait - time.Second)
	cases := []struct {
		name        string
		state       securityalert.ImpactProducerState
		wantDefer   bool
		wantOutcome string
		wantLog     string
	}{
		{name: "no attachment work proceeds silently", state: securityalert.ImpactProducerState{}},
		{name: "all succeeded proceeds silently", state: securityalert.ImpactProducerState{Succeeded: 2, Total: 2}},
		{name: "superseded only proceeds silently", state: securityalert.ImpactProducerState{Total: 1}},
		{
			name: "retrying inside the bound defers", wantDefer: true, wantOutcome: AttachmentWaitDeferredInFlight,
			state:   securityalert.ImpactProducerState{Retrying: 1, Total: 1, NewestInFlightWorkableAt: recent},
			wantLog: "attachment_retrying=1",
		},
		{
			name: "pending with a future anchor defers", wantDefer: true, wantOutcome: AttachmentWaitDeferredInFlight,
			state: securityalert.ImpactProducerState{Pending: 1, Total: 1, NewestInFlightWorkableAt: attachmentWaitNow.Add(time.Minute)},
		},
		{
			name: "claimed with a zero anchor keeps waiting", wantDefer: true, wantOutcome: AttachmentWaitDeferredInFlight,
			state: securityalert.ImpactProducerState{Claimed: 1, Total: 1},
		},
		{
			name: "in flight while a sibling dead-lettered still defers", wantDefer: true, wantOutcome: AttachmentWaitDeferredInFlight,
			state: securityalert.ImpactProducerState{Running: 1, Failed: 1, Total: 2, NewestInFlightWorkableAt: recent},
		},
		{
			name: "in flight past the bound proceeds", wantOutcome: AttachmentWaitBoundExceeded,
			state:   securityalert.ImpactProducerState{Retrying: 1, Total: 1, NewestInFlightWorkableAt: stale},
			wantLog: "level=WARN",
		},
		{
			name: "dead-lettered and none in flight proceeds", wantOutcome: AttachmentWaitProducerFailed,
			state:   securityalert.ImpactProducerState{Succeeded: 1, Failed: 1, Total: 2},
			wantLog: "attachment_failed=1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			var logs bytes.Buffer
			port := &stubAttachmentReadiness{state: tc.state}
			handler, loader, writer, issuer := attachmentWaitHandler(port, inst, &logs)
			_, err = handler.Handle(context.Background(), attachmentWaitIntent())
			if port.calls != 1 {
				t.Fatalf("probe calls = %d, want exactly 1 per attempt", port.calls)
			}
			if tc.wantDefer {
				var deferral sbomAttachmentInFlightError
				if !errors.As(err, &deferral) || deferral.FailureClass() != SBOMAttachmentNotReadyFailureClass ||
					!deferral.Retryable() || !deferral.ExpectedWait() {
					t.Fatalf("Handle() error = %v, want the retryable expected-wait %s deferral", err, SBOMAttachmentNotReadyFailureClass)
				}
				if issuer.issued != 0 || len(loader.kindCalls) != 0 || writer.calls != 0 {
					t.Fatalf("deferral ran tokens=%d loads=%d writes=%d, want none (probe runs before all of them)",
						issuer.issued, len(loader.kindCalls), writer.calls)
				}
				if !strings.Contains(logs.String(), "failure_class="+SBOMAttachmentNotReadyFailureClass) {
					t.Fatalf("defer log %q lacks the failure class", logs.String())
				}
			} else {
				if err != nil {
					t.Fatalf("Handle() error = %v, want the pass to proceed", err)
				}
				if issuer.issued != 1 || writer.calls != 1 {
					t.Fatalf("proceeding pass tokens=%d writes=%d, want 1 and 1", issuer.issued, writer.calls)
				}
			}
			got := attachmentWaitOutcomes(t, reader)
			if tc.wantOutcome == "" {
				if len(got) != 0 {
					t.Fatalf("outcomes = %v, want none recorded", got)
				}
				if logs.Len() != 0 {
					t.Fatalf("logged %q, want nothing for a silent proceed", logs.String())
				}
			} else if len(got) != 1 || got[tc.wantOutcome] != 1 {
				t.Fatalf("outcomes = %v, want exactly one %s", got, tc.wantOutcome)
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Fatalf("log %q lacks %q", logs.String(), tc.wantLog)
			}
		})
	}
}

// TestSupplyChainImpactAttachmentWaitProbeErrorStaysCounting pins that an
// unanswerable probe is a counting failure, never the non-counting class: a
// probe that always fails must dead-letter, not retry forever.
func TestSupplyChainImpactAttachmentWaitProbeErrorStaysCounting(t *testing.T) {
	t.Parallel()
	port := &stubAttachmentReadiness{err: errors.New("connection reset by peer")}
	handler, loader, writer, issuer := attachmentWaitHandler(port, nil, nil)
	_, err := handler.Handle(context.Background(), attachmentWaitIntent())
	if err == nil {
		t.Fatal("Handle() error = nil, want the probe error")
	}
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) && classified.FailureClass() == SBOMAttachmentNotReadyFailureClass {
		t.Fatalf("probe error classified as %s, want a counting class", SBOMAttachmentNotReadyFailureClass)
	}
	if !strings.Contains(err.Error(), "check sbom attestation attachment readiness") {
		t.Fatalf("Handle() error = %v, want the probe context", err)
	}
	if issuer.issued != 0 || len(loader.kindCalls) != 0 || writer.calls != 0 {
		t.Fatalf("probe error ran tokens=%d loads=%d writes=%d, want none", issuer.issued, len(loader.kindCalls), writer.calls)
	}
}

// TestSupplyChainImpactAttachmentWaitNilPortKeepsPriorBehavior pins that an
// unwired port neither probes nor records anything.
func TestSupplyChainImpactAttachmentWaitNilPortKeepsPriorBehavior(t *testing.T) {
	t.Parallel()
	handler, _, writer, _ := attachmentWaitHandler(nil, nil, nil)
	if _, err := handler.Handle(context.Background(), attachmentWaitIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want the pre-#11105 pass", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writes = %d, want 1", writer.calls)
	}
}
