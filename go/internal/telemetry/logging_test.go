package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestNewLoggerOutputsJSON(t *testing.T) {
	var buf bytes.Buffer

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)

	logger := NewLoggerWithWriter(b, "api", "api", &buf)

	logger.Info("test message", slog.String("key", "value"))

	output := buf.String()
	require.NotEmpty(t, output)

	// Parse as JSON
	var logEntry map[string]interface{}
	err = json.Unmarshal([]byte(output), &logEntry)
	require.NoError(t, err, "output should be valid JSON")

	// Verify unified field names
	assert.Equal(t, "test-service", logEntry["service_name"])
	assert.Equal(t, "eshu", logEntry["service_namespace"])
	assert.Equal(t, "test message", logEntry["message"])
	assert.Equal(t, "value", logEntry["key"])

	// Verify renamed fields are absent
	_, hasMsg := logEntry["msg"]
	assert.False(t, hasMsg, "legacy 'msg' key should not be present")
	_, hasLevel := logEntry["level"]
	assert.False(t, hasLevel, "legacy 'level' key should not be present")
	_, hasTime := logEntry["time"]
	assert.False(t, hasTime, "legacy 'time' key should not be present")

	// Verify unified field names are present
	assert.NotEmpty(t, logEntry["timestamp"], "timestamp should be present")
	assert.Equal(t, "INFO", logEntry["severity_text"])
	assert.Equal(t, "api", logEntry["component"])
	assert.Equal(t, "api", logEntry["runtime_role"])
}

func TestTraceHandlerInjectsTraceIDAndSeverityNumber(t *testing.T) {
	var buf bytes.Buffer

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)
	logger := NewLoggerWithWriter(b, "collector", "ingester", &buf)

	// Create mock span context
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	logger.InfoContext(ctx, "test with trace")

	output := buf.String()
	require.NotEmpty(t, output)

	var logEntry map[string]interface{}
	err = json.Unmarshal([]byte(output), &logEntry)
	require.NoError(t, err)

	// Verify trace context was injected
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", logEntry["trace_id"])
	assert.Equal(t, "00f067aa0ba902b7", logEntry["span_id"])

	// Verify severity_number is injected when span context is active
	assert.Equal(t, float64(9), logEntry["severity_number"], "INFO should map to severity_number 9")
}

func TestTraceHandlerNoSpanOmitsTraceID(t *testing.T) {
	var buf bytes.Buffer

	b, err := NewBootstrap("test-service")
	require.NoError(t, err)
	logger := NewLoggerWithWriter(b, "api", "api", &buf)

	// Log without span context
	logger.InfoContext(context.Background(), "test without trace")

	output := buf.String()
	require.NotEmpty(t, output)

	var logEntry map[string]interface{}
	err = json.Unmarshal([]byte(output), &logEntry)
	require.NoError(t, err)

	// Verify trace fields are absent
	_, hasTraceID := logEntry["trace_id"]
	_, hasSpanID := logEntry["span_id"]
	assert.False(t, hasTraceID, "trace_id should not be present without span context")
	assert.False(t, hasSpanID, "span_id should not be present without span context")
}

func TestSeverityNumberMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		logFn    func(*slog.Logger, context.Context)
		expected float64
	}{
		{"DEBUG maps to 5", func(l *slog.Logger, ctx context.Context) { l.DebugContext(ctx, "d") }, 5},
		{"INFO maps to 9", func(l *slog.Logger, ctx context.Context) { l.InfoContext(ctx, "i") }, 9},
		{"WARN maps to 13", func(l *slog.Logger, ctx context.Context) { l.WarnContext(ctx, "w") }, 13},
		{"ERROR maps to 17", func(l *slog.Logger, ctx context.Context) { l.ErrorContext(ctx, "e") }, 17},
	}

	// Create a valid span context so severity_number is injected.
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			b, err := NewBootstrap("test-service")
			require.NoError(t, err)

			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level:       slog.LevelDebug,
				ReplaceAttr: unifiedReplaceAttr,
			})
			tracingHandler := &TraceHandler{inner: handler}
			logger := slog.New(tracingHandler)
			_ = b // bootstrap not needed for this sub-test

			tc.logFn(logger, ctx)

			var logEntry map[string]interface{}
			err = json.Unmarshal(buf.Bytes(), &logEntry)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, logEntry["severity_number"])
		})
	}
}

func TestEventAttr(t *testing.T) {
	t.Parallel()

	attr := EventAttr("http.request.completed")
	assert.Equal(t, "event_name", attr.Key)
	assert.Equal(t, "http.request.completed", attr.Value.String())
}

func TestNewPhaseConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "query", PhaseQuery)
	assert.Equal(t, "serve", PhaseServe)
}

func TestScopeAttrs(t *testing.T) {
	attrs := ScopeAttrs("scope-123", "gen-456", "git")

	require.Len(t, attrs, 3)

	// Convert to map for easier assertion
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value.String()
	}

	assert.Equal(t, "scope-123", attrMap[LogKeyScopeID])
	assert.Equal(t, "gen-456", attrMap[LogKeyGenerationID])
	assert.Equal(t, "git", attrMap[LogKeySourceSystem])
}

func TestDomainAttrs(t *testing.T) {
	attrs := DomainAttrs("repository", "org/repo")

	require.Len(t, attrs, 2)

	// Convert to map for easier assertion
	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value.String()
	}

	assert.Equal(t, "repository", attrMap[LogKeyDomain])
	assert.Equal(t, "org/repo", attrMap[LogKeyPartitionKey])
}

func TestAcceptanceAttrs(t *testing.T) {
	attrs := AcceptanceAttrs("scope-1", "unit-2", "run-3", "gen-4")

	require.Len(t, attrs, 4)

	attrMap := make(map[string]string)
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value.String()
	}

	assert.Equal(t, "scope-1", attrMap[LogKeyAcceptanceScopeID])
	assert.Equal(t, "unit-2", attrMap[LogKeyAcceptanceUnitID])
	assert.Equal(t, "run-3", attrMap[LogKeyAcceptanceSourceRunID])
	assert.Equal(t, "gen-4", attrMap[LogKeyAcceptanceGenerationID])
}

func TestAcceptanceStaleCountAttr(t *testing.T) {
	attr := AcceptanceStaleCountAttr(7)
	assert.Equal(t, LogKeyAcceptanceStaleCount, attr.Key)
	assert.Equal(t, int64(7), attr.Value.Int64())
}

func TestTraceHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: unifiedReplaceAttr,
	})
	tracingHandler := &TraceHandler{inner: handler}

	// Add attributes via WithAttrs
	withAttrs := tracingHandler.WithAttrs([]slog.Attr{
		slog.String("request_id", "req-123"),
	})

	logger := slog.New(withAttrs)
	logger.Info("test message")

	output := buf.String()
	var logEntry map[string]interface{}
	err := json.Unmarshal([]byte(output), &logEntry)
	require.NoError(t, err)

	assert.Equal(t, "req-123", logEntry["request_id"])
}

func TestPhaseAttr(t *testing.T) {
	t.Parallel()

	phases := []string{
		PhaseDiscovery,
		PhaseParsing,
		PhaseEmission,
		PhaseProjection,
		PhaseReduction,
		PhaseShared,
		PhaseQuery,
		PhaseServe,
	}

	for _, phase := range phases {
		attr := PhaseAttr(phase)
		assert.Equal(t, LogKeyPipelinePhase, attr.Key)
		assert.Equal(t, phase, attr.Value.String())
	}
}

func TestFailureClassAttr(t *testing.T) {
	t.Parallel()

	attr := FailureClassAttr("commit_failure")
	assert.Equal(t, LogKeyFailureClass, attr.Key)
	assert.Equal(t, "commit_failure", attr.Value.String())
}

func TestTraceHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer

	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: unifiedReplaceAttr,
	})
	tracingHandler := &TraceHandler{inner: handler}

	grouped := tracingHandler.WithGroup("test_group")
	logger := slog.New(grouped)
	logger.Info("test message", slog.String("inner_key", "inner_value"))

	output := buf.String()
	require.NotEmpty(t, output)

	// Verify output contains grouped structure
	assert.True(t, strings.Contains(output, "test_group"))
}
