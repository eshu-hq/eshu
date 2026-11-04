// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"errors"
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

// Extraction status values recorded on eshu_dp_media_transcript_extractions_total.
// They mirror media.Extract's transcript_status metadata; failed covers an
// Extract error.
const (
	statusCompleted = "completed"
	statusNoText    = "no_text"
	statusNoSpeech  = "no_speech"
	statusSkipped   = "skipped"
	statusFailed    = "failed"
)

// Extract runs media.Extract with this engine and records extraction
// telemetry. Preflight, the engine call, and redaction stay exactly as
// media.Extract implements them; this wrapper only sets req.Engine and
// observes the returned facts: status, segment count, and how many segments
// were redacted. It never reads or logs segment content.
func (e *Engine) Extract(ctx context.Context, req docmedia.Request) (docmedia.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	ctx, span := e.extractTracer().Start(ctx, telemetry.SpanMediaTranscriptExtract)
	defer span.End()

	req.Engine = e
	result, err := docmedia.Extract(ctx, req)

	status := result.Document.SourceMetadata["transcript_status"]
	if err != nil && status != statusSkipped {
		status = statusFailed
	}
	redacted := 0
	for _, section := range result.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			redacted++
		}
	}
	attrs := []attribute.KeyValue{
		attribute.String(telemetry.MetricDimensionStatus, status),
		attribute.Int("media.transcript.segment_count", len(result.Sections)),
		attribute.Int("media.transcript.redacted_segment_count", redacted),
		attribute.Int("media.transcript.source_bytes", len(req.Body)),
	}
	span.SetAttributes(attrs...)
	if err != nil {
		span.SetStatus(codes.Error, extractFailureClass(err))
	}
	if inst := e.cfg.Instruments; inst != nil {
		statusAttr := metric.WithAttributes(attribute.String(telemetry.MetricDimensionStatus, status))
		if inst.MediaTranscriptExtractions != nil {
			inst.MediaTranscriptExtractions.Add(ctx, 1, statusAttr)
		}
		if inst.MediaTranscriptRedactedSegments != nil && redacted > 0 {
			inst.MediaTranscriptRedactedSegments.Add(ctx, int64(redacted))
		}
	}
	if logger := e.cfg.Logger; logger != nil {
		level := slog.LevelInfo
		logAttrs := []slog.Attr{
			slog.String("transcript_status", status),
			slog.Int("segment_count", len(result.Sections)),
			slog.Int("redacted_segment_count", redacted),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		}
		if err != nil {
			level = slog.LevelWarn
			logAttrs = append(logAttrs, slog.String(telemetry.LogKeyFailureClass, extractFailureClass(err)))
		}
		logger.LogAttrs(ctx, level, "media transcript extraction finished", logAttrs...)
	}
	return result, err
}

// failureClassPreflight labels an Extract error that came from media
// preflight rather than from this engine.
const failureClassPreflight = "preflight_error"

// extractFailureClass returns the engine class for an engine error and
// preflight_error for any other media.Extract error. The engine is always set
// before media.Extract runs, so a non-engine error comes from preflight.
func extractFailureClass(err error) string {
	var engineErr *Error
	if errors.As(err, &engineErr) {
		return string(engineErr.Class)
	}
	return failureClassPreflight
}

func (e *Engine) extractTracer() trace.Tracer {
	if e.cfg.Tracer != nil {
		return e.cfg.Tracer
	}
	return noop.NewTracerProvider().Tracer("")
}
