// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func fanOutStampPayload() map[string]any {
	return map[string]any{PayloadKeyFanOutOrigin: map[string]any{"scope_id": "scope:other"}}
}

// TestStampedRetryIsAGenuinePassAndFansOutItsOwnChange is race 1 of the 1.5b
// review. A stamp written while a genuine pass was running reaches that pass's
// retry (attempt 2). The retry is a genuine pass again, so it fans out its real
// change and consumes the stamp; only the attempt the replay created is one hop.
func TestStampedRetryIsAGenuinePassAndFansOutItsOwnChange(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{holders: []string{"holder-a"}, payload: fanOutStampPayload()}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(2, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a"}) {
		t.Fatalf("reopened = %v, want the holder: a stamped retry owes its own fan-out", port.reopened)
	}
	if writer.writes[0].Recovery {
		t.Fatal("write.Recovery = true without an owed marker: a retry is not evidence that an attempt committed")
	}
	if _, has := port.payload[PayloadKeyFanOutOrigin]; has {
		t.Fatal("the stamp survived a retry that consumed it")
	}
}

// TestStampedFirstAttemptWithAContinuationDrainsTheList is race 2: a drain claim
// is stamped by another scope's fan-out. A continuation is not a hop, so the list
// is drained (not deleted), the rest is rewritten, and the stamp is consumed in
// the same update. The pass's own changes are not added: the claim that wrote
// them fanned them out.
func TestStampedFirstAttemptWithAContinuationDrainsTheList(t *testing.T) {
	t.Parallel()

	payload := fanOutStampPayload()
	payload[PayloadKeyFanOutRemaining] = []any{"holder-a", "holder-b"}
	port := &stubFanOut{holders: []string{"holder-never-asked"}, payload: payload}
	writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(port.reopened, []string{"holder-a", "holder-b"}) {
		t.Fatalf("reopened = %v, want the stored list drained", port.reopened)
	}
	if len(port.findCalls) != 0 {
		t.Fatalf("holder reads = %v, want none: a drain does not add the pass's own changes", port.findCalls)
	}
	if _, has := payload[PayloadKeyFanOutRemaining]; has {
		t.Fatal("the drained list is still stored")
	}
	if _, has := payload[PayloadKeyFanOutOrigin]; has {
		t.Fatal("the stamp survived the drain that consumed it")
	}
}

// TestStampConsumptionNeverRewritesTheContinuationList pins the explicit field:
// consuming a stamp with nothing owed must not touch fanout_remaining.
func TestStampConsumptionNeverRewritesTheContinuationList(t *testing.T) {
	t.Parallel()

	port := &stubFanOut{payload: fanOutStampPayload()}
	writer := &changeWriter{}
	if _, err := fanOutHandler(port, writer, nil).Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(port.stateCalls) != 1 || port.stateCalls[0].ReplaceRemaining {
		t.Fatalf("state calls = %+v, want one consume that leaves the list alone", port.stateCalls)
	}
}

// TestFanOutFailureOutcomesAreCounted keeps every failure visible on the counter:
// a timed-out holder read is read_timeout, not read_error, and a failed reopen or
// state update has its own outcome.
func TestFanOutFailureOutcomesAreCounted(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		port *stubFanOut
		want string
	}{
		"timeout":      {&stubFanOut{holders: []string{"a"}, findErr: ErrFanOutReadTimeout}, "read_timeout"},
		"read error":   {&stubFanOut{holders: []string{"a"}, findErr: errors.New("boom")}, "read_error"},
		"reopen error": {&stubFanOut{holders: []string{"a"}, reopenErr: map[string]error{"a": errors.New("boom")}}, "reopen_error"},
		"state error":  {&stubFanOut{holders: []string{"a", "b"}, stateFailures: []int{1}, payload: map[string]any{PayloadKeyFanOutRemaining: []any{"a"}}}, "state_error"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatal(err)
			}
			writer := &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}
			if _, err := fanOutHandler(tc.port, writer, inst).Handle(context.Background(), fanOutIntent(1, tc.port.payload)); err == nil {
				t.Fatal("Handle() error = nil, want the failure")
			}
			if got := fanOutCount(t, reader)[tc.want]; got != 1 {
				t.Fatalf("outcome %q = %d, want 1 (counter %v)", tc.want, got, fanOutCount(t, reader))
			}
		})
	}
}

// TestFanOutSpanIsAChildAndNeverEndsTheParent guards the span lifecycle. With a
// tracer the fan-out is a child span named supply_chain_impact.fanout; with none
// the span is a no-op. In neither case may the fan-out end the caller's span
// (the reducer.run span in production), which the first draft did.
func TestFanOutSpanIsAChildAndNeverEndsTheParent(t *testing.T) {
	t.Parallel()

	for name, withTracer := range map[string]bool{"with a tracer": true, "without a tracer": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			ctx, parent := provider.Tracer("test").Start(context.Background(), "reducer.run")
			handler := fanOutHandler(&stubFanOut{holders: []string{"holder-a"}}, &changeWriter{
				result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}},
			}, nil)
			if withTracer {
				handler.Tracer = provider.Tracer("fanout")
			}
			if _, err := handler.Handle(ctx, fanOutIntent(1, nil)); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			for _, ended := range recorder.Ended() {
				if ended.Name() == "reducer.run" {
					t.Fatal("the fan-out ended the caller's reducer.run span")
				}
			}
			if parent.IsRecording() == false {
				t.Fatal("the parent span stopped recording")
			}
			fanout := 0
			for _, ended := range recorder.Ended() {
				if ended.Name() == "supply_chain_impact.fanout" {
					fanout++
					if ended.Parent().SpanID() != parent.SpanContext().SpanID() {
						t.Fatal("the fan-out span is not a child of the caller's span")
					}
				}
			}
			if want := map[bool]int{true: 1, false: 0}[withTracer]; fanout != want {
				t.Fatalf("fan-out spans = %d, want %d", fanout, want)
			}
			parent.End()
		})
	}
}

// TestDeferredCounterCountsEachHolderOnce keeps the counter honest across a
// drain: 2*cap+1 holders defer cap+1 on the first claim and the second claim
// drains them without deferring anything new, so the counter reads cap+1, not
// the sum of what each claim left behind.
func TestDeferredCounterCountsEachHolderOnce(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	holders := make([]string, 0, 2*fanOutHolderCap+1)
	for i := 0; i < 2*fanOutHolderCap+1; i++ {
		holders = append(holders, fmt.Sprintf("holder-%05d", i))
	}
	port := &stubFanOut{holders: holders, payload: map[string]any{}}
	handler := fanOutHandler(port, &changeWriter{result: SupplyChainImpactWriteResult{ChangedCVEIDs: []string{"CVE-2026-9001"}}}, inst)
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatal(err)
	}
	drain := &changeWriter{}
	handler.Writer = drain
	if _, err := handler.Handle(context.Background(), fanOutIntent(1, port.payload)); err != nil {
		t.Fatal(err)
	}
	if got := fanOutCount(t, reader)["deferred"]; got != fanOutHolderCap+1 {
		t.Fatalf("deferred = %d, want %d (each holder deferred once)", got, fanOutHolderCap+1)
	}
}
