// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"io"
	"log/slog"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// changedImpactWriter is a writer that reports one changed CVE, so the bench
// pays for the changed label the way a rewriting pass
// does. It lives here, not in impact_test.go, so the same file also builds on
// the tree before #10972 for the before and after comparison.
type changedImpactWriter struct {
	recordingSupplyChainImpactWriter
}

func (w *changedImpactWriter) WriteSupplyChainImpactFindings(ctx context.Context, write SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error) {
	result, err := w.recordingSupplyChainImpactWriter.WriteSupplyChainImpactFindings(ctx, write)
	result.ChangedCVEIDs = []string{"CVE-2026-0001"}
	return result, err
}

// BenchmarkHandleStampedPassWithSignals measures one stamped, rewriting pass
// with the registered instruments, a JSON logger and the pass-outcome signals
// (#10972), against an in-memory loader and writer. It isolates the handler's
// own cost; the signals add one counter add, three span attributes and one
// INFO line, and no read.
func BenchmarkHandleStampedPassWithSignals(b *testing.B) {
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider().Meter("bench"))
	if err != nil {
		b.Fatal(err)
	}
	intent := reducercontract.Intent{
		IntentID:     "intent-10972",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-10972",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
		AttemptCount: 1,
		Payload:      scopedStampPayload("CVE-2026-0001"),
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		handler := SupplyChainImpactHandler{
			FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
			FactLoader:         &stubSupplyChainImpactFactLoader{},
			Writer:             &changedImpactWriter{},
			Instruments:        inst,
			Logger:             slog.New(slog.NewJSONHandler(io.Discard, nil)),
		}
		if _, err := handler.Handle(context.Background(), intent); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}
