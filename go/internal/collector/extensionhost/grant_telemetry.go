// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// GrantTelemetry turns producer-grant decisions into the three operator
// signals of #6726: the eshu_dp_component_producer_grant_decisions_total
// counter, a span event on the active span, and a structured log line. It
// implements component.GrantObserver and is safe for concurrent use.
//
// What each signal carries is deliberately narrow. The counter labels are the
// closed decision, stage, and reason sets plus a registry-bounded core fact
// kind. The producer id and version are operator-configured and unbounded, so
// they ride only on the span event and the log line, never on a metric label.
// No signal carries a credential, grant scope, config value, or fact payload.
//
// Logging follows the flood policy: every deny logs at WARN (a denied
// emission fails the result terminally, so denials are bounded by failures),
// an allow logs at INFO only at install and activation, and per-emission and
// readback allows are counted but never logged.
type GrantTelemetry struct {
	recorder *telemetry.ProducerGrantDecisionRecorder
	logger   *slog.Logger
}

// NewGrantTelemetry builds the adapter. A nil inst disables the counter and a
// nil logger disables logging; the span event fires whenever the context
// carries a recording span.
func NewGrantTelemetry(inst *telemetry.Instruments, logger *slog.Logger) *GrantTelemetry {
	return &GrantTelemetry{
		recorder: telemetry.NewProducerGrantDecisionRecorder(inst),
		logger:   logger,
	}
}

// ObserveGrantDecision implements component.GrantObserver.
func (g *GrantTelemetry) ObserveGrantDecision(ctx context.Context, d component.GrantDecision) {
	if g == nil {
		return
	}
	decision := grantDecisionLabel(d.Allowed)
	g.recorder.Record(ctx, string(d.Stage), decision, string(d.Reason), d.Kind)
	g.addSpanEvent(ctx, d, decision)
	g.log(ctx, d, decision)
}

// addSpanEvent records the decision on the span active at the decision site
// (the claimed-run span for the per-emission recheck). It is skipped for a
// non-recording span, which keeps unsampled hot-path emissions allocation-free.
func (g *GrantTelemetry) addSpanEvent(ctx context.Context, d component.GrantDecision, decision string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.AddEvent(contract.SpanEventProducerGrantDecision, trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionDecision, decision),
		attribute.String(telemetry.MetricDimensionStage, string(d.Stage)),
		attribute.String(telemetry.MetricDimensionReason, string(d.Reason)),
		attribute.String(telemetry.MetricDimensionFactKind, d.Kind),
		attribute.String(contract.SpanAttrProducerGrantProducerID, d.ProducerID),
		attribute.String(contract.SpanAttrProducerGrantVersion, d.Version),
	))
}

// log writes the decision line per the flood policy on GrantTelemetry.
func (g *GrantTelemetry) log(ctx context.Context, d component.GrantDecision, decision string) {
	if g.logger == nil {
		return
	}
	level := slog.LevelWarn
	if d.Allowed {
		if d.Stage != component.GrantStageInstall && d.Stage != component.GrantStageActivation {
			return
		}
		level = slog.LevelInfo
	}
	msg := "producer grant denied"
	if d.Allowed {
		msg = "producer grant allowed"
	}
	g.logger.LogAttrs(ctx, level, msg,
		slog.String(contract.LogKeyProducerGrantProducerID, d.ProducerID),
		slog.String(contract.LogKeyProducerGrantVersion, d.Version),
		slog.String(contract.LogKeyProducerGrantDecision, decision),
		slog.String(contract.LogKeyProducerGrantStage, string(d.Stage)),
		slog.String(contract.LogKeyProducerGrantReason, string(d.Reason)),
		slog.String(contract.LogKeyProducerGrantFactKind, d.Kind),
	)
}

// grantDecisionLabel maps the allow flag to the closed decision label value.
func grantDecisionLabel(allowed bool) string {
	if allowed {
		return contract.ProducerGrantDecisionAllow
	}
	return contract.ProducerGrantDecisionDeny
}

var _ component.GrantObserver = (*GrantTelemetry)(nil)
