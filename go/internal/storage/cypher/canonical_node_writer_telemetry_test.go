package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCanonicalNodeWriterCreatesWriteAndRetractSpans(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	writer := NewCanonicalNodeWriter(&mockPhaseGroupExecutor{}, 500, nil).
		WithTracer(tracerProvider.Tracer("test"))

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "repo",
			Path:   "/repo",
		},
		Files: []projector.FileRow{
			{Path: "/repo/main.go", RelativePath: "main.go", Name: "main.go", Language: "go", RepoID: "repo-1"},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	assertSpanEnded(t, spanRecorder.Ended(), telemetry.SpanCanonicalWrite)
	assertSpanEnded(t, spanRecorder.Ended(), telemetry.SpanCanonicalRetract)
}

func TestCanonicalNodeWriterMarksRetractSpanAndLogOnPhaseFailure(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(telemetry.NewLoggerWithWriter(
		telemetry.Bootstrap{ServiceName: "test", ServiceNamespace: "test"},
		"test",
		"test",
		&logs,
	))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	writer := NewCanonicalNodeWriter(&mockPhaseGroupExecutor{phaseGroupErr: errors.New("graph timeout")}, 500, nil).
		WithTracer(tracerProvider.Tracer("test"))

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "repo",
			Path:   "/repo",
		},
		Files: []projector.FileRow{
			{Path: "/repo/main.go", RelativePath: "main.go", Name: "main.go", Language: "go", RepoID: "repo-1"},
		},
	})
	if err == nil {
		t.Fatal("Write() error = nil, want non-nil")
	}

	retractSpan := requireSpanEnded(t, spanRecorder.Ended(), telemetry.SpanCanonicalRetract)
	if got, want := retractSpan.Status().Code, codes.Error; got != want {
		t.Fatalf("retract span status = %v, want %v", got, want)
	}
	logEntry := decodeSingleJSONLog(t, logs.Bytes())
	for key, want := range map[string]string{
		"message":  "canonical phase failed",
		"phase":    "retract",
		"scope_id": "scope-1",
		"mode":     "phase_group",
		"trace_id": retractSpan.SpanContext().TraceID().String(),
		"span_id":  retractSpan.SpanContext().SpanID().String(),
	} {
		if got, _ := logEntry[key].(string); got != want {
			t.Fatalf("canonical failure log[%s] = %#v, want %q; log=%s", key, logEntry[key], want, logs.String())
		}
	}
}

func decodeSingleJSONLog(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("decode log JSON: %v; log=%s", err, string(data))
	}
	return entry
}

func assertSpanEnded(t *testing.T, spans []sdktrace.ReadOnlySpan, want string) {
	t.Helper()

	_ = requireSpanEnded(t, spans, want)
}

func requireSpanEnded(t *testing.T, spans []sdktrace.ReadOnlySpan, want string) sdktrace.ReadOnlySpan {
	t.Helper()

	for _, span := range spans {
		if span.Name() == want {
			return span
		}
	}
	t.Fatalf("span %q not recorded; got %v", want, spanNamesForTest(spans))
	return nil
}

func spanNamesForTest(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}
