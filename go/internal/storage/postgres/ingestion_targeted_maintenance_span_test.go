// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Review N2: with no tracer the partition-scoped pass must not write onto
// the span its caller's context carries.
func TestTargetedMaintenanceSpanWithoutATracerLeavesTheCallerSpanAlone(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, parent := provider.Tracer("caller").Start(context.Background(), "caller")
	_, span, end := startTargetedMaintenanceSpan(ctx, nil)
	recordTargetedMaintenance(ctx, span, nil, time.Now(), nil, TargetedMaintenanceResult{},
		fmt.Errorf("boom: %w", errors.New("untyped")))
	end()
	parent.End()
	ended := recorder.Ended()
	if len(ended) != 1 || len(ended[0].Attributes()) != 0 || ended[0].Status().Code != codes.Unset {
		t.Fatalf("caller span = %+v, want untouched", ended)
	}
}

// Review N2: a designed hold (a typed refusal) is not a trace error; an
// untyped failure is.
func TestTargetedMaintenanceSpanMarksOnlyUntypedFailuresAsErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{"catalog_changed", fmt.Errorf("pass: %w", ErrTargetedMaintenanceCatalogChanged), codes.Unset},
		{"no_memo_baseline", fmt.Errorf("pass: %w", ErrTargetedMaintenanceNoMemoBaseline), codes.Unset},
		{"closure_too_deep", fmt.Errorf("pass: %w", ErrTargetedMaintenanceClosureTooDeep), codes.Unset},
		{"untyped", errors.New("connection reset"), codes.Error},
	} {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		ctx, span, end := startTargetedMaintenanceSpan(context.Background(), provider.Tracer("test"))
		recordTargetedMaintenance(ctx, span, nil, time.Now(), nil, TargetedMaintenanceResult{}, tc.err)
		end()
		ended := recorder.Ended()
		if len(ended) != 1 || ended[0].Status().Code != tc.want {
			t.Fatalf("%s: span status = %v, want %v", tc.name, ended[0].Status().Code, tc.want)
		}
	}
}
