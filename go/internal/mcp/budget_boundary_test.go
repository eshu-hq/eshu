// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
)

// TestResponseBudgetBoundaryIsInclusive proves a response exactly at the budget
// passes untouched and one byte over is acted on, for both shapes. The dual
// shape answers one byte over with the resource-only fallback; the structured
// shape, which has no second copy to drop, answers with the over-budget error.
func TestResponseBudgetBoundaryIsInclusive(t *testing.T) {
	t.Parallel()

	for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
		const rows, rowBytes = 40, 512
		size := estimateResponseBytes(dispatchShaped(t, mode, rows, rowBytes, 1<<30))
		if size <= 0 {
			t.Fatalf("%s: measured size = %d, want positive", mode, size)
		}

		atBudget := dispatchShaped(t, mode, rows, rowBytes, size)
		if atBudget.IsError || atBudget.ResourceOnly || atBudget.Envelope == nil || atBudget.Envelope.Error != nil {
			t.Fatalf("%s: size == budget (%d) was altered: %#v", mode, size, atBudget)
		}

		overByOne := dispatchShaped(t, mode, rows, rowBytes, size-1)
		switch mode {
		case resultshape.Dual:
			if !overByOne.ResourceOnly || overByOne.IsError {
				t.Fatalf("dual: size == budget+1 result = %#v, want the resource-only fallback", overByOne)
			}
		case resultshape.Structured:
			if !overByOne.IsError || overByOne.Envelope == nil || overByOne.Envelope.Error == nil ||
				overByOne.Envelope.Error.Code != errorCodeResponseOverBudget {
				t.Fatalf("structured: size == budget+1 result = %#v, want the over-budget error envelope", overByOne)
			}
		}
	}
}

// TestWithResultShapeNormalizesUnknownValues proves a value that did not come
// from resultshape.Parse cannot reach the render, budget, or metric-label code
// as a third shape: anything other than structured behaves as dual everywhere.
func TestWithResultShapeNormalizesUnknownValues(t *testing.T) {
	t.Parallel()

	s := NewServer(fixedBodyHandler(200, shapeFixtureOK), discardDispatchLogger(), WithResultShape(resultshape.Mode("bogus")))
	if got := s.ResultShape(); got != resultshape.Dual {
		t.Fatalf("ResultShape() with an unknown value = %q, want %q", got, resultshape.Dual)
	}
	if got := shapeAttr(resultshape.Mode("bogus")).Value.AsString(); got != string(resultshape.Dual) {
		t.Fatalf("shapeAttr(unknown) = %q, want %q", got, resultshape.Dual)
	}

	result := &dispatchResult{Envelope: nil, Value: map[string]any{"k": "v"}, Shape: resultshape.Mode("bogus")}
	applied := applyResponseBudget(result, "find_symbol", 1<<20, nil)
	if applied.Shape != resultshape.Dual {
		t.Fatalf("applyResponseBudget kept shape %q, want %q", applied.Shape, resultshape.Dual)
	}
}

// TestOverBudgetLogNamesResourceBytesOnlyUnderDual proves the over-budget log
// reports resource_bytes only where a resource copy was measured. The
// structured shape has no resource copy, so the field is absent there instead
// of repeating response_bytes.
func TestOverBudgetLogNamesResourceBytesOnlyUnderDual(t *testing.T) {
	t.Parallel()

	for _, mode := range []resultshape.Mode{resultshape.Dual, resultshape.Structured} {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		big := map[string]any{"rows": strings.Repeat("x", 4096)}
		result := &dispatchResult{Value: big, Shape: mode}
		_ = applyResponseBudget(result, "find_symbol", 64, logger)

		var record map[string]any
		if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
			t.Fatalf("%s: log line %q is not JSON: %v", mode, buf.String(), err)
		}
		_, has := record["resource_bytes"]
		if has != (mode == resultshape.Dual) {
			t.Fatalf("%s: resource_bytes present = %v in %v", mode, has, record)
		}
		if _, ok := record["response_bytes"]; !ok {
			t.Fatalf("%s: response_bytes missing in %v", mode, record)
		}
	}
}
