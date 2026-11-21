// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"io"
	"log/slog"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// BenchmarkHandleRoundCutPass times one whole pass cut at the 8-round cap
// (#11156): the seeded expanding fixture, the real handler, a recording
// writer and a JSON logger writing to io.Discard, so the frontier INFO line is
// paid for. It uses no symbol the frontier diagnostic adds, so the same file
// runs on the commit before the diagnostic for the before/after comparison.
func BenchmarkHandleRoundCutPass(b *testing.B) {
	intent := reducercontract.Intent{
		IntentID:     "intent-7154",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-7154",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		handler := SupplyChainImpactHandler{
			FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
			FactLoader:         expandingImpactLoader(),
			Writer:             &recordingSupplyChainImpactWriter{},
			Logger:             slog.New(slog.NewJSONHandler(io.Discard, nil)),
		}
		if _, err := handler.Handle(context.Background(), intent); err != nil {
			b.Fatalf("Handle() error = %v", err)
		}
	}
}
