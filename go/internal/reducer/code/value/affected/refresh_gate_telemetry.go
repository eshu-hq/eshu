// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package affected

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Value-flow refresh emit-gate outcomes (issue #6785), the closed outcome
// label set for eshu_dp_value_flow_refresh_gate_evaluations_total. The helper
// lives in this leaf (not the reducer root) so every producer handler — the
// three reducer-root producers and the iamcan producer — records the same
// counter and span without an import cycle.
const (
	// RefreshGateAffected means the graph read found cloud-calling repos and
	// the ACK may emit.
	RefreshGateAffected = "affected"
	// RefreshGateSuppressed means an explicit zero withholds the event.
	RefreshGateSuppressed = "suppressed"
	// RefreshGateFailOpen means the gate was unwired or its read errored, so
	// the ACK emits rather than risk silent accuracy loss.
	RefreshGateFailOpen = "fail_open"
)

// RefreshGateTelemetry bounds one producer's affected-repo gate read in a
// span and records the counter point on end. Nil tracer or instruments degrade
// to no-ops so test wiring without telemetry still gates.
type RefreshGateTelemetry struct {
	ctx         context.Context
	span        trace.Span
	instruments *telemetry.Instruments
	domain      reducercontract.Domain
}

// BeginRefreshGateEvaluation starts the gate span for one producer run.
func BeginRefreshGateEvaluation(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	domain reducercontract.Domain,
) (context.Context, *RefreshGateTelemetry) {
	eval := &RefreshGateTelemetry{ctx: ctx, instruments: instruments, domain: domain}
	if tracer != nil {
		ctx, eval.span = tracer.Start(ctx, telemetry.SpanReducerValueFlowRefreshGate,
			trace.WithAttributes(telemetry.AttrDomain(string(domain))),
		)
	}
	return ctx, eval
}

// End records one eshu_dp_value_flow_refresh_gate_evaluations_total point and
// closes the span, annotating both with the outcome and affected repo count.
func (e *RefreshGateTelemetry) End(outcome string, affectedRepos int) {
	if e == nil {
		return
	}
	if e.instruments != nil && e.instruments.ValueFlowRefreshGateEvaluations != nil {
		e.instruments.ValueFlowRefreshGateEvaluations.Add(
			e.ctx, 1, metric.WithAttributes(
				telemetry.AttrDomain(string(e.domain)),
				telemetry.AttrOutcome(outcome),
			),
		)
	}
	if e.span != nil {
		e.span.SetAttributes(
			telemetry.AttrOutcome(outcome),
			attribute.Int("affected_repo_count", affectedRepos),
		)
		e.span.End()
	}
}
