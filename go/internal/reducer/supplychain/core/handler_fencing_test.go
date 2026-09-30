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

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// orderRecordingImpactLoader records the order of its first read relative to
// the fencing-token issue, so a test can prove the token is issued before the
// evidence load.
type orderRecordingImpactLoader struct {
	*stubSupplyChainImpactFactLoader
	events *[]string
}

func (l orderRecordingImpactLoader) ListFacts(ctx context.Context, scopeID, generationID string) ([]facts.Envelope, error) {
	*l.events = append(*l.events, "load")
	return l.stubSupplyChainImpactFactLoader.ListFacts(ctx, scopeID, generationID)
}

func (l orderRecordingImpactLoader) ListFactsByKind(
	ctx context.Context, scopeID, generationID string, kinds []string,
) ([]facts.Envelope, error) {
	*l.events = append(*l.events, "load")
	return l.stubSupplyChainImpactFactLoader.ListFactsByKind(ctx, scopeID, generationID, kinds)
}

type orderRecordingTokenIssuer struct {
	events *[]string
	token  int64
	err    error
}

func (i orderRecordingTokenIssuer) NextSupplyChainImpactFencingToken(context.Context) (int64, error) {
	*i.events = append(*i.events, "issue")
	return i.token, i.err
}

func fencingTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-7142",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-7142",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	}
}

// TestSupplyChainImpactHandleIssuesFencingTokenBeforeEvidenceLoad pins #7142:
// the token ranks passes by evidence recency, so it must be issued before the
// first evidence read. A token issued after the load, or at commit, would let a
// worker that read stale evidence early out-rank a fresher one.
func TestSupplyChainImpactHandleIssuesFencingTokenBeforeEvidenceLoad(t *testing.T) {
	t.Parallel()

	var events []string
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:         orderRecordingImpactLoader{stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{}, events: &events},
		Writer:             writer,
		FencingTokenIssuer: orderRecordingTokenIssuer{events: &events, token: 9001},
	}
	if _, err := handler.Handle(context.Background(), fencingTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(events) == 0 || events[0] != "issue" {
		t.Fatalf("event order = %v, want the token issued before any evidence load", events)
	}
	if writer.write.FencingToken != 9001 {
		t.Fatalf("write.FencingToken = %d, want the issued token 9001", writer.write.FencingToken)
	}
}

// TestSupplyChainImpactHandleRequiresFencingTokenIssuer pins the fail-closed
// contract: no issuer means no ordering value, so the pass errors before it
// loads anything or writes.
func TestSupplyChainImpactHandleRequiresFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{FactLoader: loader, Writer: writer}
	if _, err := handler.Handle(context.Background(), fencingTestIntent()); err == nil ||
		!strings.Contains(err.Error(), "fencing token issuer is required") {
		t.Fatalf("Handle() error = %v, want the missing-issuer error", err)
	}
	if len(loader.kindCalls) != 0 || writer.calls != 0 {
		t.Fatalf("kindCalls=%d writes=%d, want none before the issuer check", len(loader.kindCalls), writer.calls)
	}
}

// TestSupplyChainImpactHandleFailsClosedWhenTokenIssueFails pins that a
// sequence failure fails the intent before the load rather than writing with a
// missing token.
func TestSupplyChainImpactHandleFailsClosedWhenTokenIssueFails(t *testing.T) {
	t.Parallel()

	var events []string
	loader := &stubSupplyChainImpactFactLoader{}
	writer := &recordingSupplyChainImpactWriter{}
	boom := errors.New("sequence unavailable")
	handler := SupplyChainImpactHandler{
		FactLoader:         loader,
		Writer:             writer,
		FencingTokenIssuer: orderRecordingTokenIssuer{events: &events, err: boom},
	}
	if _, err := handler.Handle(context.Background(), fencingTestIntent()); !errors.Is(err, boom) {
		t.Fatalf("Handle() error = %v, want the issuer error", err)
	}
	if len(loader.kindCalls) != 0 || writer.calls != 0 {
		t.Fatal("a pass with no token loaded evidence or wrote")
	}
}

type supersededWriter struct{}

func (supersededWriter) WriteSupplyChainImpactFindings(_ context.Context, write SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	return SupplyChainImpactWriteResult{}, supplyChainImpactWriteSupersededError{
		scopeID: write.ScopeID, generationID: write.GenerationID, fencingToken: write.FencingToken,
	}
}

// TestSupplyChainImpactHandleReportsSupersededWrite pins #7142 observability:
// a write the admission rejected returns the retryable superseded error
// unchanged (so the queue classifies it) and is counted and logged with the
// scope, generation and token an operator needs.
func TestSupplyChainImpactHandleReportsSupersededWrite(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logs := &bytes.Buffer{}
	var events []string
	handler := SupplyChainImpactHandler{
		FactLoader:         &stubSupplyChainImpactFactLoader{},
		Writer:             supersededWriter{},
		Instruments:        inst,
		Logger:             slog.New(slog.NewJSONHandler(logs, nil)),
		FencingTokenIssuer: orderRecordingTokenIssuer{events: &events, token: 31},
	}
	_, err = handler.Handle(context.Background(), fencingTestIntent())
	var classified failureClassifier
	if !errors.As(err, &classified) || !classified.Retryable() || classified.FailureClass() != SupplyChainImpactWriteSupersededFailureClass {
		t.Fatalf("Handle() error = %v, want the retryable superseded error preserved", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_write_superseded_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if dp.Attributes.Len() != 1 {
					t.Fatalf("superseded counter labels = %v, want only domain", dp.Attributes.ToSlice())
				}
				total += dp.Value
			}
		}
	}
	if total != 1 {
		t.Fatalf("eshu_dp_supply_chain_impact_write_superseded_total = %d, want 1", total)
	}
	for _, want := range []string{`"scope_id":"vuln-intel://osv/npm/example"`, `"generation_id":"generation-7142"`, `"fencing_token":31`, "write superseded"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("WARN log missing %s: %s", want, logs.String())
		}
	}
}
