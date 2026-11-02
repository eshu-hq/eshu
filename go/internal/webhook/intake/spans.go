// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"context"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// spanStatusStoreFailed is the fixed description on an Error span status. The
// raw store error can carry SQL text and identifiers, so it is never recorded
// on a span.
const spanStatusStoreFailed = "store failed"

// requestSpans is the span state of one intake request (#9815). The zero value
// is a request with no tracer: every method is then a no-op.
type requestSpans struct {
	tracer trace.Tracer
	handle trace.Span
	store  trace.Span

	// eventKind, decision, and status are the labels the handle span ends
	// with. They stay "unknown" until the request reaches the store decision.
	eventKind string
	decision  string
	status    string
}

// startHandleSpan starts webhook.handle and replaces the request context with
// the span's context, so the store call, the metrics, and any store span share
// one trace. A nil tracer leaves the context untouched.
func (o *requestObs) startHandleSpan(tracer trace.Tracer) {
	o.spans.eventKind, o.spans.decision, o.spans.status = valueUnknown, valueUnknown, valueUnknown
	if tracer == nil {
		return
	}
	o.spans.tracer = tracer
	o.ctx, o.spans.handle = tracer.Start(o.ctx, telemetry.SpanWebhookHandle,
		trace.WithAttributes(telemetry.AttrProvider(o.provider)))
}

// bind returns the request carrying the handle span's context, so the log
// lines written through it share the span's trace_id and span_id. A request
// with no tracer is returned unchanged, without the shallow copy
// http.Request.WithContext would allocate.
func (o *requestObs) bind(r *http.Request) *http.Request {
	if o.spans.handle == nil {
		return r
	}
	return r.WithContext(o.ctx)
}

// abandonStoreSpan ends a store span the handler never closed, which only
// happens when the store call panicked. The request counts as a failed handoff
// so neither span stays open. It is a no-op once the store span has ended.
func (o *requestObs) abandonStoreSpan() {
	if o.spans.store == nil {
		return
	}
	o.outcome = outcomeFailed
	o.reason = reasonStoreFailed
	o.endStoreSpan(outcomeFailed, statusFailed)
}

// noteDecision records the labels the handle span ends with.
func (o *requestObs) noteDecision(d triggerDecision, status string) {
	o.spans.eventKind = d.eventKind
	o.spans.decision = d.decision
	o.spans.status = status
}

// endHandleSpan sets the final bounded attributes and ends webhook.handle. It
// runs once, from finish, so every return path ends the span.
func (o *requestObs) endHandleSpan() {
	span := o.spans.handle
	if span == nil {
		return
	}
	span.SetAttributes(
		telemetry.AttrProvider(o.provider),
		telemetry.AttrOutcome(o.outcome),
		telemetry.AttrReason(o.reason),
		telemetry.AttrEventKind(o.spans.eventKind),
		telemetry.AttrDecision(o.spans.decision),
		telemetry.AttrStatus(o.spans.status),
	)
	if o.outcome == outcomeFailed {
		span.SetStatus(codes.Error, spanStatusStoreFailed)
	}
	span.End()
	o.spans.handle = nil
}

// beginStore starts webhook.store around the durable write and returns the
// context the store call must use, so the stores' postgres spans nest under
// it, plus the call's start time for the duration histogram.
func (o *requestObs) beginStore(d triggerDecision) (context.Context, time.Time) {
	started := time.Now()
	if o.spans.tracer == nil {
		return o.ctx, started
	}
	ctx, span := o.spans.tracer.Start(o.ctx, telemetry.SpanWebhookStore, trace.WithAttributes(
		telemetry.AttrProvider(d.provider),
		telemetry.AttrEventKind(d.eventKind),
		telemetry.AttrDecision(d.decision),
	))
	o.spans.store = span
	return ctx, started
}

// endStoreSpan sets the store outcome and ends webhook.store. A failed write
// sets status Error with a fixed description and records no error value.
func (o *requestObs) endStoreSpan(outcome, status string) {
	span := o.spans.store
	if span == nil {
		return
	}
	span.SetAttributes(telemetry.AttrOutcome(outcome), telemetry.AttrStatus(status))
	if outcome == outcomeFailed {
		span.SetStatus(codes.Error, spanStatusStoreFailed)
	}
	span.End()
	o.spans.store = nil
}
