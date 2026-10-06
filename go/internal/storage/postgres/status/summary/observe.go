// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Span attribute names the status snapshot span carries for the active-work
// read, next to #7647's status.active_work.summary_mode.
const (
	spanAttrSource      = "status.active_work.source"
	spanAttrAgeSeconds  = "status.active_work.as_of_age_seconds"
	spanAttrFallbackWhy = "status.active_work.fallback_reason"
)

// fallbackWarnEvery bounds how often one process logs a fallback of one reason.
const fallbackWarnEvery = time.Minute

// Observation is what one status read decided about the active-work summary.
type Observation struct {
	// ModelKey is the model the read selected.
	ModelKey string
	// Source says where the answer came from.
	Source Source
	// Reason explains Source.
	Reason Reason
	// AsOf is the stored row's as_of; zero when no row was read.
	AsOf time.Time
	// Age is the stored row's age at the read; zero when no row was read.
	Age time.Duration
}

// Observe records one read decision: the read counter, the served-age
// histogram for a stored row that was served, the source, age, and fallback
// reason on the current span, and a rate-limited Warn when the live statement
// ran because the stored row could not be served. instruments may be nil.
// A flag-off read and a served row never log.
func Observe(ctx context.Context, instruments *telemetry.Instruments, o Observation) {
	source, reason := string(o.Source), string(o.Reason)
	if instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrModelKey(o.ModelKey), telemetry.AttrSummarySource(source), telemetry.AttrSummaryReason(reason),
		)
		if instruments.StatusSummaryReads != nil {
			instruments.StatusSummaryReads.Add(ctx, 1, attrs)
		}
		if o.Source == SourceModel && instruments.StatusSummaryReadAge != nil {
			instruments.StatusSummaryReadAge.Record(ctx, o.Age.Seconds(),
				metric.WithAttributes(telemetry.AttrModelKey(o.ModelKey)))
		}
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String(spanAttrSource, source))
	if !o.AsOf.IsZero() {
		span.SetAttributes(attribute.Float64(spanAttrAgeSeconds, o.Age.Seconds()))
	}
	if o.Source == SourceLiveFallback {
		span.SetAttributes(attribute.String(spanAttrFallbackWhy, reason))
		if fallbackLimiter.allow(o.Reason, time.Now()) {
			slog.Default().WarnContext(ctx, "status summary row not served; running the live active-work statement",
				slog.String("model_key", o.ModelKey),
				slog.String("source", source),
				slog.String("reason", reason),
				slog.Float64("age_seconds", o.Age.Seconds()),
				slog.String("failure_class", "status_summary_fallback"),
			)
		}
	}
}

// fallbackLimiter spaces the per-process fallback Warn by reason.
var fallbackLimiter warnLimiter

// warnLimiter allows one log per reason per fallbackWarnEvery.
type warnLimiter struct {
	mu   sync.Mutex
	last map[Reason]time.Time
}

func (l *warnLimiter) allow(reason Reason, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous, ok := l.last[reason]; ok && now.Sub(previous) < fallbackWarnEvery {
		return false
	}
	if l.last == nil {
		l.last = make(map[Reason]time.Time)
	}
	l.last[reason] = now
	return true
}

// reset forgets every reason, for tests that assert the first log.
func (l *warnLimiter) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last = nil
}
