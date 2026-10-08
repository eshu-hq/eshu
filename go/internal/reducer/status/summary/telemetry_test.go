// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRunCountsAnOverrunWhenTheModelsTogetherOutlastTheInterval: the overrun is
// per pass, so two model transactions that are each shorter than the interval
// but sum past it are one overrun, and the warning names each model's time. Two
// that sum under the interval are none.
func TestRunCountsAnOverrunWhenTheModelsTogetherOutlastTheInterval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		each         time.Duration
		wantOverruns int64
		wantPass     string
	}{
		{"3 s and 3 s at a 5 s interval", 3 * time.Second, 1, `"pass_ms":6000`},
		{"2 s and 2 s at a 5 s interval", 2 * time.Second, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, _, primary, logs := newPassRunner(t)
			instruments, reader := newTestInstruments(t)
			runner.Instruments = instruments
			companion := withCompanion(runner)
			primary.cost = tc.each
			companion.clock, companion.cost = primary.clock, tc.each
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			waiter := &scriptedWait{limit: 1, cancel: cancel}
			runner.Wait = waiter.wait

			if err := runner.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil on cancel", err)
			}

			if got := counterValue(t, reader, "eshu_dp_status_summary_writer_overrun_total",
				telemetry.AttrModelKey(store.ModelActiveWorkSummary)); got != tc.wantOverruns {
				t.Fatalf("overrun_total = %d, want %d", got, tc.wantOverruns)
			}
			warnings := strings.Count(logs.String(), `"msg":"status summary writer pass overran its interval"`)
			if int64(warnings) != tc.wantOverruns {
				t.Fatalf("overrun warnings = %d, want %d", warnings, tc.wantOverruns)
			}
			if tc.wantOverruns == 0 {
				return
			}
			for _, want := range []string{tc.wantPass, `"active_work_summary_ms":3000`, `"terraform_state_ms":3000`} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("overrun warning lacks %s: %s", want, logs.String())
				}
			}
			if len(waiter.waits) != 1 || waiter.waits[0] != 4*time.Second {
				t.Fatalf("waits = %v, want the next start on the following 5 s boundary (4 s)", waiter.waits)
			}
		})
	}
}

// TestPassSpanIsAnErrorWhenAnyModelFails: the pass span records every failed
// model's error and is marked an error when any model failed, not only the
// first model.
func TestPassSpanIsAnErrorWhenAnyModelFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		failFirst  bool
		failSecond bool
		wantErrors int
		wantStatus codes.Code
	}{
		{"neither fails", false, false, 0, codes.Unset},
		{"only the companion fails", false, true, 1, codes.Error},
		{"only the first model fails", true, false, 1, codes.Error},
		{"both fail", true, true, 2, codes.Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			runner, _, primary, _ := newPassRunner(t)
			runner.Tracer = provider.Tracer("status-summary-test")
			companion := withCompanion(runner)
			if tc.failFirst {
				primary.err = errors.New("active work boom")
			}
			if tc.failSecond {
				companion.err = errors.New("tfstate boom")
			}

			runner.RunOnce(context.Background())

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want the one pass span", len(spans))
			}
			errorEvents, modelEvents := 0, 0
			for _, event := range spans[0].Events() {
				switch event.Name {
				case "exception":
					errorEvents++
				case "status_summary.model":
					modelEvents++
				}
			}
			if errorEvents != tc.wantErrors || modelEvents != 2 {
				t.Fatalf("span events: %d exceptions and %d model events, want %d and 2", errorEvents, modelEvents, tc.wantErrors)
			}
			if got := spans[0].Status().Code; got != tc.wantStatus {
				t.Fatalf("span status = %v, want %v", got, tc.wantStatus)
			}
		})
	}
}
