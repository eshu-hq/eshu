// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// benchEnvelope builds a canonical tools/call envelope whose one-copy JSON
// size is at least targetBytes. Rows hold a 256-byte blob each, the same row
// shape the budget tests use, so the size grows by whole rows.
func benchEnvelope(targetBytes int) *query.ResponseEnvelope {
	blob := strings.Repeat("x", 256)
	rows := make([]any, 0, targetBytes/280+1)
	envelope := &query.ResponseEnvelope{
		Truth: query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"code_search.fuzzy_symbol",
			query.TruthBasisContentIndex,
			"resolved from content index fallback",
		),
	}
	for i := 0; ; i++ {
		rows = append(rows, map[string]any{"id": i, "blob": blob})
		envelope.Data = map[string]any{"source": "content", "results": rows}
		if i%8 == 7 {
			encoded, _ := json.Marshal(envelope)
			if len(encoded) >= targetBytes {
				return envelope
			}
		}
	}
}

// BenchmarkResultShapeRenderBudget times the per-call result path that every
// tools/call pays: the budget measurement (applyResponseBudget renders and
// marshals the result once) followed by the send-time render and marshal. It
// runs the real dispatch functions on a success envelope that fits the default
// budget, so no resource fallback or page applies, and reports the bytes
// written to the wire per shape so a shape comparison needs no second tool.
func BenchmarkResultShapeRenderBudget(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"2KB", 2 * 1024}, {"25KB", 25 * 1024}, {"120KB", 120 * 1024}} {
		envelope := benchEnvelope(size.bytes)
		oneCopy, _ := json.Marshal(envelope)
		for _, shape := range benchShapes {
			b.Run(fmt.Sprintf("%s/%s", size.name, shape.name), func(b *testing.B) {
				b.ReportAllocs()
				var wire int
				for i := 0; i < b.N; i++ {
					result := newBenchResult(envelope, shape)
					result = applyResponseBudget(result, "find_symbol", defaultToolResponseByteBudget, nil)
					if result.IsError || result.ResourceOnly {
						b.Fatalf("budget altered the %s result: error=%v resource_only=%v", size.name, result.IsError, result.ResourceOnly)
					}
					encoded, err := json.Marshal(renderToolResult("find_symbol", result))
					if err != nil {
						b.Fatal(err)
					}
					wire = len(encoded)
				}
				b.ReportMetric(float64(wire), "wire-B")
				b.ReportMetric(float64(len(oneCopy)), "payload-B")
			})
		}
	}
}
