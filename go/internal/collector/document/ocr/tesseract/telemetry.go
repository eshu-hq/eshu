// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// Extraction status values recorded on eshu_dp_ocr_extractions_total. The
// first three mirror ocr.Extract's ocr_status metadata; failed covers an
// Extract error.
const (
	statusCompleted = "completed"
	statusNoText    = "no_text"
	statusSkipped   = "skipped"
	statusFailed    = "failed"
)

// Extract runs ocr.Extract with this engine and records extraction
// telemetry. Preflight, the engine call, and redaction stay exactly as
// ocr.Extract implements them; this wrapper only sets req.Engine and observes
// the returned facts: status, region count, and how many regions were
// redacted. It never reads or logs section content.
func (e *Engine) Extract(ctx context.Context, req ocr.Request) (ocr.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	ctx, span := e.tracer().Start(ctx, contract.SpanOCRExtract)
	defer span.End()

	req.Engine = e
	result, err := ocr.Extract(ctx, req)

	status := result.Document.SourceMetadata["ocr_status"]
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
		attribute.Int("ocr.region_count", len(result.Sections)),
		attribute.Int("ocr.redacted_region_count", redacted),
		attribute.Int("ocr.source_bytes", len(req.Body)),
	}
	span.SetAttributes(attrs...)
	if err != nil {
		span.SetStatus(codes.Error, string(OutcomeOf(err)))
	}
	if inst := e.cfg.Instruments; inst != nil {
		statusAttr := metric.WithAttributes(attribute.String(telemetry.MetricDimensionStatus, status))
		if inst.OCRExtractions != nil {
			inst.OCRExtractions.Add(ctx, 1, statusAttr)
		}
		if inst.OCRRedactedRegions != nil && redacted > 0 {
			inst.OCRRedactedRegions.Add(ctx, int64(redacted))
		}
	}
	if logger := e.cfg.Logger; logger != nil {
		level := slog.LevelInfo
		logAttrs := []slog.Attr{
			slog.String("ocr_status", status),
			slog.Int("region_count", len(result.Sections)),
			slog.Int("redacted_region_count", redacted),
			slog.Float64("duration_seconds", time.Since(start).Seconds()),
		}
		if err != nil {
			level = slog.LevelWarn
			logAttrs = append(logAttrs, slog.String(telemetry.LogKeyFailureClass, string(OutcomeOf(err))))
		}
		logger.LogAttrs(ctx, level, "ocr extraction finished", logAttrs...)
	}
	return result, err
}

func (e *Engine) tracer() trace.Tracer {
	if e.cfg.Tracer != nil {
		return e.cfg.Tracer
	}
	return noop.NewTracerProvider().Tracer("")
}

func (e *Engine) startRecognizeSpan(ctx context.Context, img ocr.Image) (context.Context, trace.Span) {
	return e.tracer().Start(ctx, contract.SpanOCRRecognize, trace.WithAttributes(
		attribute.Int("ocr.input_bytes", len(img.Body)),
	))
}

// recordRecognition emits the per-run metrics, span attributes, and one log
// line. Every value is bounded: outcome, format, sizes, counts, and the
// classified error, whose text never includes subprocess output.
func (e *Engine) recordRecognition(
	ctx context.Context,
	span trace.Span,
	stats recognizeStats,
	outcome Outcome,
	elapsed time.Duration,
	err error,
) {
	span.SetAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, string(outcome)),
		attribute.String("ocr.format", stats.format),
		attribute.Int64("ocr.pixels", stats.pixels),
		attribute.Bool("ocr.process_started", stats.started),
		attribute.Int64("ocr.peak_rss_bytes", stats.peakRSS),
		attribute.Int("ocr.stdout_bytes", stats.stdoutBytes),
		attribute.Int64("ocr.stderr_bytes", stats.stderrBytes),
		attribute.Int("ocr.region_count", stats.regions),
	)
	if err != nil {
		span.SetStatus(codes.Error, string(outcome))
	}
	if inst := e.cfg.Instruments; inst != nil {
		outcomeAttr := metric.WithAttributes(attribute.String(telemetry.MetricDimensionOutcome, string(outcome)))
		if inst.OCRRecognitions != nil {
			inst.OCRRecognitions.Add(ctx, 1, outcomeAttr)
		}
		if inst.OCRRecognitionDuration != nil {
			inst.OCRRecognitionDuration.Record(ctx, elapsed.Seconds(), outcomeAttr)
		}
		if inst.OCRPeakRSS != nil && stats.started && stats.peakRSS > 0 {
			inst.OCRPeakRSS.Record(ctx, stats.peakRSS)
		}
	}
	logger := e.cfg.Logger
	if logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyOutcome, string(outcome)),
		slog.String("format", stats.format),
		slog.Int("input_bytes", stats.inputBytes),
		slog.Int64("pixels", stats.pixels),
		slog.Int64("peak_rss_bytes", stats.peakRSS),
		slog.Int("region_count", stats.regions),
		slog.Float64("duration_seconds", elapsed.Seconds()),
	}
	if err != nil {
		attrs = append(attrs,
			slog.String(telemetry.LogKeyFailureClass, string(outcome)),
			slog.String("error", err.Error()),
		)
		logger.LogAttrs(ctx, slog.LevelWarn, "ocr recognition failed", attrs...)
		return
	}
	logger.LogAttrs(ctx, slog.LevelDebug, "ocr recognition finished", attrs...)
}
