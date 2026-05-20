package cypher

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
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
	logText := logs.String()
	for _, want := range []string{`"msg":"canonical phase failed"`, `"phase":"retract"`, `"scope_id":"scope-1"`, `"mode":"phase_group"`} {
		if !strings.Contains(logText, want) {
			t.Fatalf("canonical failure log = %s, want %s", logText, want)
		}
	}
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
