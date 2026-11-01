// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const fanOutOperatorScope = "operator:vulnerability_suppressions"

// stubFanOut is an in-memory SupplyChainImpactFanOut. It records every call and
// mirrors the committing item's payload so a multi-claim drain can be replayed
// the way the queue would: the payload a claim sees is what the previous claim
// stored.
type stubFanOut struct {
	holders       []string
	findErr       error
	reopenErr     map[string]error
	outcomes      map[string]FanOutOutcome
	findCalls     [][]string
	findOwn       []string
	reopened      []string
	origins       []FanOutOrigin
	stateCalls    []FanOutState
	stateFailures []int // 1-based UpdateFanOutState call numbers that fail
	payload       map[string]any
	inFlight      []string
	inFlightErr   error
	inFlightCalls int
	owedRecorded  int
	// reads records the order of the two read calls.
	reads []string
}

// stubOwed is the recorder the fake writer runs the way the real writer does:
// inside the write, once the pass changed something.
type stubOwed struct{ port *stubFanOut }

func (r stubOwed) RecordFanOutOwed(context.Context, factwrite.Execer) error {
	r.port.owedRecorded++
	if r.port.payload != nil {
		r.port.payload[PayloadKeyFanOutOwed] = true
	}
	return nil
}

func (s *stubFanOut) FindInFlightScopes(_ context.Context, _ string) ([]string, error) {
	s.inFlightCalls++
	s.reads = append(s.reads, "in-flight")
	if s.inFlightErr != nil {
		return nil, s.inFlightErr
	}
	return append([]string(nil), s.inFlight...), nil
}

func (s *stubFanOut) OwedRecorder(reducercontract.Intent) FanOutOwedRecorder {
	return stubOwed{port: s}
}

func (s *stubFanOut) FindHolderScopes(_ context.Context, ownScopeID string, cveIDs []string) ([]string, error) {
	s.reads = append(s.reads, "rows")
	s.findOwn = append(s.findOwn, ownScopeID)
	s.findCalls = append(s.findCalls, append([]string(nil), cveIDs...))
	if s.findErr != nil {
		return nil, s.findErr
	}
	return append([]string(nil), s.holders...), nil
}

func (s *stubFanOut) ReopenHolder(_ context.Context, holderScopeID string, origin FanOutOrigin) (FanOutOutcome, error) {
	if err := s.reopenErr[holderScopeID]; err != nil {
		return "", err
	}
	s.reopened = append(s.reopened, holderScopeID)
	s.origins = append(s.origins, origin)
	if outcome, ok := s.outcomes[holderScopeID]; ok {
		return outcome, nil
	}
	return FanOutReopened, nil
}

func (s *stubFanOut) UpdateFanOutState(_ context.Context, _ reducercontract.Intent, state FanOutState) error {
	s.stateCalls = append(s.stateCalls, state)
	for _, call := range s.stateFailures {
		if call == len(s.stateCalls) {
			return errors.New("injected lease loss")
		}
	}
	if s.payload == nil {
		return nil
	}
	if state.ConsumeOrigin && reflect.DeepEqual(s.payload[PayloadKeyFanOutOrigin], state.ExpectedOrigin) {
		delete(s.payload, PayloadKeyFanOutOrigin)
	}
	if state.ClearOwed {
		delete(s.payload, PayloadKeyFanOutOwed)
	}
	if !state.ReplaceRemaining {
		return nil
	}
	if len(state.Remaining) == 0 {
		delete(s.payload, PayloadKeyFanOutRemaining)
		return nil
	}
	list := make([]any, 0, len(state.Remaining))
	for _, id := range state.Remaining {
		list = append(list, id)
	}
	s.payload[PayloadKeyFanOutRemaining] = list
	return nil
}

// changeWriter returns a fixed write result and records each write it saw.
type changeWriter struct {
	result SupplyChainImpactWriteResult
	writes []SupplyChainImpactWrite
	// err, when set, fails the write before anything commits.
	err error
}

func (w *changeWriter) WriteSupplyChainImpactFindings(ctx context.Context, write SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	w.writes = append(w.writes, write)
	if w.err != nil {
		return SupplyChainImpactWriteResult{}, w.err
	}
	if write.Owed != nil && len(w.result.ChangedCVEIDs) > 0 {
		if err := write.Owed.RecordFanOutOwed(ctx, nil); err != nil {
			return SupplyChainImpactWriteResult{}, err
		}
	}
	out := w.result
	out.FactsWritten = len(write.Findings)
	return out, nil
}

func fanOutHandler(port SupplyChainImpactFanOut, writer SupplyChainImpactWriter, inst *telemetry.Instruments) SupplyChainImpactHandler {
	scopeFacts := append(
		[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-9001", "nvd", "CVE-2026-9001", "Rejected")},
		retractionPackageEvidence("CVE-2026-9001", "nvd", "CVE-2026-9001")...,
	)
	return SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         &stubSupplyChainImpactFactLoader{scopeFacts: scopeFacts},
		Writer:             writer,
		FanOut:             port,
		Instruments:        inst,
		Now:                func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) },
	}
}

// fanOutIntent builds a claimed intent. The payload is copied, because a claim
// reads the item's payload as it was at claim time: the stub port plays the
// database, and a write inside the pass must not change what the pass already read.
func fanOutIntent(attempt int, payload map[string]any) reducercontract.Intent {
	if payload != nil {
		snapshot := make(map[string]any, len(payload))
		for key, value := range payload {
			snapshot[key] = value
		}
		payload = snapshot
	}
	return reducercontract.Intent{
		IntentID:     "intent-fanout",
		ScopeID:      "vuln-intel://nvd/cve",
		GenerationID: "generation-fanout",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
		AttemptCount: attempt,
		Payload:      payload,
	}
}

func fanOutCount(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fanout_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				out[outcome.AsString()] += dp.Value
			}
		}
	}
	return out
}
