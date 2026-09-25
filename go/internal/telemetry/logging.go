// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package telemetry provides structured JSON logging with trace correlation.
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// unifiedReplaceAttr renames the built-in slog keys to match the platform's
// canonical Go logging schema.
func unifiedReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		a.Key = "timestamp"
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.UTC().Format(time.RFC3339Nano))
		}
	case slog.LevelKey:
		a.Key = "severity_text"
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// NewLogger creates a JSON logger with base service attributes following the
// unified logging standard.
func NewLogger(b Bootstrap, component, runtimeRole string) *slog.Logger {
	return NewLoggerWithWriter(b, component, runtimeRole, os.Stderr)
}

// NewLoggerWithWriter creates a JSON logger with base service attributes and
// writes records to the provided destination.
func NewLoggerWithWriter(b Bootstrap, component, runtimeRole string, writer io.Writer) *slog.Logger {
	output := io.Writer(os.Stderr)
	if writer != nil {
		output = writer
	}

	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: unifiedReplaceAttr,
	})

	tracingHandler := &TraceHandler{inner: handler}

	logger := slog.New(tracingHandler).With(
		slog.String("service_name", b.ServiceName),
		slog.String("service_namespace", b.ServiceNamespace),
		slog.String("component", component),
		slog.String("runtime_role", runtimeRole),
	)

	return logger
}

// EventAttr returns an event_name attribute for structured logging.
func EventAttr(name string) slog.Attr {
	return slog.String("event_name", name)
}

// severityNumber maps slog levels to OTEL severity numbers.
func severityNumber(level slog.Level) int {
	switch {
	case level < slog.LevelInfo:
		return 5 // DEBUG
	case level < slog.LevelWarn:
		return 9 // INFO
	case level < slog.LevelError:
		return 13 // WARN
	default:
		return 17 // ERROR
	}
}

// TraceHandler wraps an slog.Handler and injects trace_id and span_id from
// the active OTEL span context on every log record.
type TraceHandler struct {
	inner slog.Handler
	group string
	attrs []slog.Attr
}

// Enabled reports whether the handler handles records at the given level.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle processes a log record, injecting trace context and severity number
// when a valid span is active.
func (h *TraceHandler) Handle(ctx context.Context, record slog.Record) error {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanCtx.TraceID().String()),
			slog.String("span_id", spanCtx.SpanID().String()),
			slog.Int("severity_number", severityNumber(record.Level)),
		)
	}

	// Apply any accumulated attrs from WithAttrs
	for _, attr := range h.attrs {
		record.AddAttrs(attr)
	}

	return h.inner.Handle(ctx, record)
}

// WithAttrs returns a new handler with the given attributes added.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{
		inner: h.inner,
		group: h.group,
		attrs: append(h.attrs, attrs...),
	}
}

// WithGroup returns a new handler with the given group name.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	return &TraceHandler{
		inner: h.inner.WithGroup(name),
		group: name,
		attrs: h.attrs,
	}
}

// ScopeAttrs returns common scope attributes for structured logging.
func ScopeAttrs(scopeID, generationID, sourceSystem string) []slog.Attr {
	return []slog.Attr{
		slog.String(LogKeyScopeID, scopeID),
		slog.String(LogKeyGenerationID, generationID),
		slog.String(LogKeySourceSystem, sourceSystem),
	}
}

// DomainAttrs returns domain attributes for structured logging.
func DomainAttrs(domain, partitionKey string) []slog.Attr {
	return []slog.Attr{
		slog.String(LogKeyDomain, domain),
		slog.String(LogKeyPartitionKey, partitionKey),
	}
}

// AcceptanceAttrs returns acceptance-specific structured logging attributes.
func AcceptanceAttrs(scopeID, unitID, sourceRunID, generationID string) []slog.Attr {
	return []slog.Attr{
		slog.String(LogKeyAcceptanceScopeID, scopeID),
		slog.String(LogKeyAcceptanceUnitID, unitID),
		slog.String(LogKeyAcceptanceSourceRunID, sourceRunID),
		slog.String(LogKeyAcceptanceGenerationID, generationID),
	}
}

// File-group diagnostic keys are emitted only by the opt-in NornicDB
// files-phase probe. IDs and indices correlate attempts within one process;
// template IDs and outcomes use closed vocabularies. None carries query text,
// parameters, file paths, or raw driver errors.
const (
	// LogKeyFileGroupCallID correlates one process-local ExecuteGroup call.
	LogKeyFileGroupCallID = "group_call_id"
	// LogKeyFileGroupAttempt counts managed transaction callback invocations.
	LogKeyFileGroupAttempt = "attempt"
	// LogKeyFileGroupStatementIndex is the one-based position in a callback.
	LogKeyFileGroupStatementIndex = "statement_index"
	// LogKeyFileGroupStatementCount is the fixed group size.
	LogKeyFileGroupStatementCount = "statement_count"
	// LogKeyFileGroupTemplateID names one of six fixed File Cypher templates.
	LogKeyFileGroupTemplateID = "template_id"
	// LogKeyFileGroupRowCount is the number of rows passed to one statement.
	LogKeyFileGroupRowCount = "row_count"
	// LogKeyFileGroupRunDuration measures tx.Run wall time in seconds.
	LogKeyFileGroupRunDuration = "run_duration_s"
	// LogKeyFileGroupConsumeDuration measures Result.Consume wall time when called.
	LogKeyFileGroupConsumeDuration = "consume_duration_s"
	// LogKeyFileGroupOutcome is a closed statement or group result.
	LogKeyFileGroupOutcome = "outcome"
	// LogKeyFileGroupAttempts counts callbacks observed before ExecuteWrite returned.
	LogKeyFileGroupAttempts = "attempts"
	// LogKeyFileGroupDuration measures the whole ExecuteWrite call in seconds.
	LogKeyFileGroupDuration = "duration_s"
	// LogKeyFileGroupPostCallbackDuration measures time after a successful callback.
	LogKeyFileGroupPostCallbackDuration = "post_callback_duration_s"
)

// Pipeline phase constants for structured log correlation across the full
// ingestion pipeline. Every log line should carry one of these so operators
// can filter by phase when tracing end-to-end.
const (
	PhaseDiscovery  = "discovery"  // repo selection and scope assignment
	PhaseParsing    = "parsing"    // file parse, snapshot, content extraction
	PhaseEmission   = "emission"   // fact envelope creation and durable commit
	PhaseProjection = "projection" // fact-to-graph/content projection
	PhaseReduction  = "reduction"  // reducer intent execution
	PhaseShared     = "shared"     // shared projection partition processing
	PhaseQuery      = "query"      // read-path query operations
	PhaseServe      = "serve"      // API/MCP request handling
)

// PhaseAttr returns a pipeline_phase attribute for structured logging.
func PhaseAttr(phase string) slog.Attr {
	return slog.String(LogKeyPipelinePhase, phase)
}

// FailureClassAttr returns a failure_class attribute for structured logging.
func FailureClassAttr(class string) slog.Attr {
	return slog.String(LogKeyFailureClass, class)
}

// AcceptanceStaleCountAttr returns a stale count for acceptance logs: the
// stale intents the shared-projection lane filtered out, or (from the
// acceptance writer, #6679) the acceptance writes the advance-only upsert
// skipped because the stored generation was newer.
func AcceptanceStaleCountAttr(count int) slog.Attr {
	return slog.Int(LogKeyAcceptanceStaleCount, count)
}
