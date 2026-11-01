// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// eventEngineRun is the event_name on the per-run log line.
const eventEngineRun = "media.transcript_engine.run"

// runStats carries bounded numeric evidence for one run. It never holds
// transcript text, stderr content, paths, or source names.
type runStats struct {
	inputBytes      int64
	audioMillis     int64
	stdoutBytes     int64
	stderrBytes     int64
	peakRSSBytes    int64
	segmentsKept    int64
	segmentsDropped int64
}

type engineRun struct {
	engine  *Engine
	ctx     context.Context
	span    trace.Span
	started time.Time
	stats   runStats
}

func (e *Engine) startRun(ctx context.Context, input docmedia.Media) *engineRun {
	tracer := e.cfg.Tracer
	if tracer == nil {
		tracer = noop.NewTracerProvider().Tracer("")
	}
	ctx, span := tracer.Start(ctx, telemetry.SpanMediaTranscriptEngineRun, trace.WithAttributes(
		attribute.String("media.transcript.engine", EngineName),
		attribute.String("media.transcript.engine_version", e.cfg.EngineVersion),
		attribute.String("media.transcript.model_digest", e.modelDigest),
		attribute.String("media.transcript.format", input.Format),
	))
	return &engineRun{engine: e, ctx: ctx, span: span, started: time.Now()}
}

// finish records the closed outcome on the span, metrics, and one log line.
func (r *engineRun) finish(err error) {
	outcome := outcomeOf(err)
	elapsed := time.Since(r.started)
	r.span.SetAttributes(
		attribute.String("media.transcript.outcome", outcome),
		attribute.Int64("media.transcript.input_bytes", r.stats.inputBytes),
		attribute.Int64("media.transcript.audio_millis", r.stats.audioMillis),
		attribute.Int64("media.transcript.stdout_bytes", r.stats.stdoutBytes),
		attribute.Int64("media.transcript.stderr_bytes", r.stats.stderrBytes),
		attribute.Int64("media.transcript.engine_peak_rss_bytes", r.stats.peakRSSBytes),
		attribute.Int64("media.transcript.segments_kept", r.stats.segmentsKept),
		attribute.Int64("media.transcript.segments_non_speech_dropped", r.stats.segmentsDropped),
	)
	if err != nil && outcome != string(ClassNoSpeech) {
		r.span.SetStatus(codes.Error, outcome)
	}
	r.span.End()

	if inst := r.engine.cfg.Instruments; inst != nil {
		outcomeAttr := metric.WithAttributes(telemetry.AttrOutcome(outcome))
		inst.MediaTranscriptEngineRuns.Add(r.ctx, 1, outcomeAttr)
		inst.MediaTranscriptEngineDuration.Record(r.ctx, elapsed.Seconds(), outcomeAttr)
		if r.stats.segmentsKept > 0 {
			inst.MediaTranscriptEngineSegments.Add(r.ctx, r.stats.segmentsKept,
				metric.WithAttributes(attribute.String(telemetry.MetricDimensionKind, "kept")))
		}
		if r.stats.segmentsDropped > 0 {
			inst.MediaTranscriptEngineSegments.Add(r.ctx, r.stats.segmentsDropped,
				metric.WithAttributes(attribute.String(telemetry.MetricDimensionKind, "non_speech_dropped")))
		}
	}

	logger := r.engine.cfg.Logger
	if logger == nil {
		return
	}
	attrs := []any{
		telemetry.EventAttr(eventEngineRun),
		telemetry.PhaseAttr(telemetry.PhaseParsing),
		slog.String("engine", EngineName),
		slog.String("engine_version", r.engine.cfg.EngineVersion),
		slog.String("model_digest", r.engine.modelDigest),
		slog.String("outcome", outcome),
		slog.Float64("elapsed_seconds", elapsed.Seconds()),
		slog.Int64("input_bytes", r.stats.inputBytes),
		slog.Int64("audio_millis", r.stats.audioMillis),
		slog.Int64("stdout_bytes", r.stats.stdoutBytes),
		slog.Int64("stderr_bytes", r.stats.stderrBytes),
		slog.Int64("engine_peak_rss_bytes", r.stats.peakRSSBytes),
		slog.Int64("segments_kept", r.stats.segmentsKept),
		slog.Int64("segments_non_speech_dropped", r.stats.segmentsDropped),
	}
	level := slog.LevelInfo
	if err != nil && outcome != string(ClassNoSpeech) {
		level = slog.LevelWarn
		attrs = append(attrs, telemetry.FailureClassAttr(outcome))
	}
	logger.Log(r.ctx, level, "media transcript engine run", attrs...)
}
