// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNeo4jReaderParentDeadlineDoesNotRecordPolicyDeadlineOutcome(t *testing.T) {
	manualReader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(manualReader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("neo4j-read-policy-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	reader := newPolicyTestNeo4jReader(blockingPolicySession)
	reader.policy.instruments = instruments
	reader.policy.readTimeout = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _ = reader.Run(ctx, "RETURN 1", nil)

	var metrics metricdata.ResourceMetrics
	if err := manualReader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := graphReadMetricOutcome(metrics)
	if got != string(graphReadOutcomeCallerDeadline) {
		t.Fatalf("graph read metric outcome = %q, want %q", got, graphReadOutcomeCallerDeadline)
	}
	if got == string(graphReadOutcomeDeadline) {
		t.Fatal("parent deadline incremented the graph-policy deadline outcome")
	}
}

func TestNeo4jReaderSpansDoNotExposeQueryText(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{{
			Keys: []string{"secret"}, Values: []any{"redacted"},
		}}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	queryText := "RETURN 'private-query-marker' AS secret"

	if _, err := reader.Run(context.Background(), queryText, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, err := reader.RunSingle(context.Background(), queryText, nil); err != nil {
		t.Fatalf("RunSingle() error = %v", err)
	}

	for _, span := range recorder.Ended() {
		for _, field := range span.Attributes() {
			if strings.Contains(field.Value.String(), "private-query-marker") {
				t.Fatalf("span %q attribute %q exposed query text", span.Name(), field.Key)
			}
		}
	}
}

// TestNeo4jReaderWarningNamesStatementButNotDriverCause proves the warning
// names the exact statement shape (fingerprint plus bounded head, #7035)
// while still never exposing the raw driver failure cause (a Bolt address).
func TestNeo4jReaderWarningNamesStatementButNotDriverCause(t *testing.T) {
	const (
		queryText    = "MATCH (secret:PrivateThing) RETURN secret"
		privateCause = "bolt://private.example.invalid:7687"
	)
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, &neo4jdriver.ConnectivityError{Inner: errors.New(privateCause)}
		}}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	_, _ = reader.Run(context.Background(), queryText, nil)
	got := logs.String()
	if strings.Contains(got, privateCause) {
		t.Fatalf("warning exposed driver cause: %s", got)
	}
	if !strings.Contains(got, graphStatementFingerprint(queryText)) || !strings.Contains(got, graphStatementHead(queryText)) {
		t.Fatalf("warning = %s, want statement fingerprint and head", got)
	}
}

func TestNeo4jReaderSessionCleanupFailureIsObservableAndSanitized(t *testing.T) {
	const privateCause = "bolt://private-cleanup.example.invalid:7687"
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{
			result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}},
			close:  func(context.Context) error { return errors.New(privateCause) },
		}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, `"event_name":"query.graph_read.session_close_failed"`) ||
		!strings.Contains(got, `"failure_class":"session_close_error"`) {
		t.Fatalf("cleanup warning = %s, want bounded event and failure class", got)
	}
	if strings.Contains(got, privateCause) {
		t.Fatalf("cleanup warning exposed driver cause: %s", got)
	}
}

func TestNeo4jReaderRecoveredReadAnnotatesBoundedSpanOutcome(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	attempts := 0
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		attempts++
		if attempts == 1 {
			return &fakeNeo4jReadSession{run: connectivityErrorRun("temporary disconnect")}
		}
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadOutcome); got != string(graphReadOutcomeRecovered) {
		t.Fatalf("graph read outcome = %q, want %q", got, graphReadOutcomeRecovered)
	}
	if got := graphReadSpanInt(spans[0].Attributes(), telemetry.SpanAttrGraphReadAttempts); got != maxGraphReadAttempts {
		t.Fatalf("graph read attempts = %d, want %d", got, maxGraphReadAttempts)
	}
}

func TestNeo4jReaderNeo4jAvailabilityFailureEmitsSanitizedUnavailableTelemetry(t *testing.T) {
	const privateCause = "bolt://private-availability.example.invalid:7687"
	var logs bytes.Buffer
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{run: func(
			context.Context,
			string,
			map[string]any,
			...func(*neo4jdriver.TransactionConfig),
		) (neo4jReadResult, error) {
			return nil, &neo4jdriver.Neo4jError{
				Code: "Neo.TransientError.General.DatabaseUnavailable",
				Msg:  privateCause,
			}
		}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); !errors.Is(err, ErrGraphUnavailable) {
		t.Fatalf("Run() error = %v, want ErrGraphUnavailable", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadOutcome) != string(graphReadOutcomeUnavailable) ||
		graphReadSpanInt(spans[0].Attributes(), telemetry.SpanAttrGraphReadAttempts) != maxGraphReadAttempts {
		t.Fatalf("availability span = %#v, want unavailable with %d attempts", spans, maxGraphReadAttempts)
	}
	got := logs.String()
	if !strings.Contains(got, `"failure_class":"unavailable"`) || strings.Contains(got, privateCause) {
		t.Fatalf("availability warning = %s, want sanitized unavailable event", got)
	}
}

func TestNeo4jReaderSlowSuccessEmitsBoundedWarning(t *testing.T) {
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.slowThreshold = time.Nanosecond

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := logs.String(); !strings.Contains(got, `"failure_class":"slow"`) ||
		!strings.Contains(got, `"event_name":"query.graph_read.warning"`) {
		t.Fatalf("slow warning = %s, want bounded outcome and event", got)
	}
}

// TestNeo4jReaderDefaultsQueryNameWhenCallerSetNone pins the #7006 telemetry
// fix's default: a read whose context carries no querycontract.WithGraphQueryName
// value still gets a bounded, present (never blank/absent) query-name
// attribute and log field, so the operator signal is never silently missing.
func TestNeo4jReaderDefaultsQueryNameWhenCallerSetNone(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.slowThreshold = time.Nanosecond

	if _, err := reader.Run(context.Background(), "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadQueryName); got != querycontract.DefaultGraphQueryName {
		t.Fatalf("span query name = %q, want %q", got, querycontract.DefaultGraphQueryName)
	}
	if got := logs.String(); !strings.Contains(got, `"graph_query_name":"`+querycontract.DefaultGraphQueryName+`"`) {
		t.Fatalf("warning log = %s, want the default graph_query_name field", got)
	}
}

// TestNeo4jReaderRecordsCallerSuppliedQueryName pins the positive case: a
// caller-set querycontract.WithGraphQueryName value reaches both the span
// attribute and the bounded warning log.
func TestNeo4jReaderRecordsCallerSuppliedQueryName(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(func(context.Context, neo4jdriver.SessionConfig) neo4jReadSession {
		return &fakeNeo4jReadSession{result: &fakeNeo4jReadResult{records: []*neo4jdriver.Record{}}}
	})
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.slowThreshold = time.Nanosecond

	ctx := querycontract.WithGraphQueryName(context.Background(), "code_quality.complexity")
	if _, err := reader.Run(ctx, "RETURN 1", nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadQueryName); got != "code_quality.complexity" {
		t.Fatalf("span query name = %q, want %q", got, "code_quality.complexity")
	}
	if got := logs.String(); !strings.Contains(got, `"graph_query_name":"code_quality.complexity"`) {
		t.Fatalf("warning log = %s, want the caller-supplied graph_query_name field", got)
	}
}

// TestNeo4jReaderSharedBoundedDeadlineClassifiesAsPolicyDeadline pins the
// #7006 review's F1 fix: a shared per-label-loop budget from
// querycontract.WithBoundedGraphReadDeadline expires a few microseconds
// before the readCtx that runRead derives from it with the SAME duration
// (production: both 10s; here both reader.policy.readTimeout, so the test is
// fast and deterministic), so graphReadResult's parentCtx.Err() branch always
// fires first. Before the fix that branch unconditionally returned
// graphReadOutcomeCallerDeadline: no query.graph_read.warning log, no
// graph_query_name, and the caller got a raw context.DeadlineExceeded
// instead of the wrapped ErrGraphReadDeadline sentinel -- silently dropping
// the deadline outcome for every GET /entities/{id}/context and
// POST /infra/relationships timeout (both share this exact budget via their
// per-label anchor loop). Proven through the REAL Neo4jReader (not a fake
// GraphQuery), since only the real graphReadResult/recordGraphReadTelemetry
// path can misclassify this.
func TestNeo4jReaderSharedBoundedDeadlineClassifiesAsPolicyDeadline(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(blockingPolicySession)
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.readTimeout = 30 * time.Millisecond

	ctx, cancel := querycontract.WithBoundedGraphReadDeadlineFor(
		querycontract.WithGraphQueryName(context.Background(), "infra_relationships_test"),
		reader.policy.readTimeout,
	)
	defer cancel()

	_, err := reader.Run(ctx, "RETURN 1", nil)
	if !errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("Run() error = %v, want ErrGraphReadDeadline (a shared bounded-read budget is the graph-read policy's own deadline, not caller cancellation)", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadOutcome); got != string(graphReadOutcomeDeadline) {
		t.Fatalf("graph read outcome = %q, want %q", got, graphReadOutcomeDeadline)
	}

	got := logs.String()
	if !strings.Contains(got, `"event_name":"query.graph_read.warning"`) {
		t.Fatalf("warning log = %s, want the bounded-read warning event", got)
	}
	if !strings.Contains(got, `"failure_class":"deadline"`) {
		t.Fatalf("warning log = %s, want failure_class=deadline", got)
	}
	if !strings.Contains(got, `"graph_query_name":"infra_relationships_test"`) {
		t.Fatalf("warning log = %s, want the shared budget's graph_query_name", got)
	}
}

// TestNeo4jReaderParentDeadlineDoesNotRecordPolicyDeadlineOutcome (above)
// pins the control case this fix must not regress: an ORDINARY caller
// deadline that never went through WithBoundedGraphReadDeadline(For) --
// e.g. an MCP dispatch timeout, or a test's own context.WithTimeout -- must
// keep classifying as graphReadOutcomeCallerDeadline, not deadline. Only a
// context carrying the WithBoundedGraphReadDeadline(For) marker is the
// graph-read policy's own budget.

// TestNeo4jReaderShorterCallerDeadlineInsideBoundedCtxClassifiesAsCallerDeadline
// pins #7006 review round 4's F7: the F1 fix's marker was attached with
// context.WithValue on the ctx WithBoundedGraphReadDeadlineFor returns, so it
// identifies which ctx CARRIES the budget, not which deadline actually FIRED.
// A caller deadline set OUTSIDE the bounded ctx (e.g. an MCP dispatch
// timeout) that is SHORTER than the bounded budget still expires the same
// wrapped context, so IsBoundedGraphReadDeadline saw the marker and reported
// a policy deadline for a deadline the graph-read policy never set. Here a
// 30ms caller deadline sits inside a 500ms bounded budget (readTimeout is
// set above the budget so runRead's own per-read timeout cannot fire
// first); the caller's shorter deadline must still classify as
// graphReadOutcomeCallerDeadline, exactly like
// TestNeo4jReaderParentDeadlineDoesNotRecordPolicyDeadlineOutcome's ordinary
// caller deadline -- no ErrGraphReadDeadline, no
// query.graph_read.warning log, no graph_query_name.
func TestNeo4jReaderShorterCallerDeadlineInsideBoundedCtxClassifiesAsCallerDeadline(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var logs bytes.Buffer
	reader := newPolicyTestNeo4jReader(blockingPolicySession)
	reader.tracer = provider.Tracer("neo4j-read-policy-test")
	reader.policy.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	reader.policy.readTimeout = 500 * time.Millisecond

	callerCtx, callerCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer callerCancel()
	ctx, cancel := querycontract.WithBoundedGraphReadDeadlineFor(
		querycontract.WithGraphQueryName(callerCtx, "infra_relationships_test"),
		reader.policy.readTimeout,
	)
	defer cancel()

	_, err := reader.Run(ctx, "RETURN 1", nil)
	if errors.Is(err, ErrGraphReadDeadline) {
		t.Fatalf("Run() error = %v, want a plain caller deadline error, not ErrGraphReadDeadline (a shorter caller deadline is not the graph-read policy's own budget)", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := graphReadSpanString(spans[0].Attributes(), telemetry.SpanAttrGraphReadOutcome); got != string(graphReadOutcomeCallerDeadline) {
		t.Fatalf("graph read outcome = %q, want %q", got, graphReadOutcomeCallerDeadline)
	}

	if got := logs.String(); strings.Contains(got, `"event_name":"query.graph_read.warning"`) {
		t.Fatalf("warning log = %s, want no bounded-read warning for a shorter caller deadline", got)
	}
}

func graphReadMetricOutcome(metrics metricdata.ResourceMetrics) string {
	for _, scope := range metrics.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_neo4j_query_duration_seconds" {
				continue
			}
			histogram, ok := record.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 {
				return ""
			}
			value, _ := histogram.DataPoints[0].Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
			return value.AsString()
		}
	}
	return ""
}

func graphReadSpanString(attributes []attribute.KeyValue, key string) string {
	for _, candidate := range attributes {
		if string(candidate.Key) == key {
			return candidate.Value.AsString()
		}
	}
	return ""
}

func graphReadSpanInt(attributes []attribute.KeyValue, key string) int64 {
	for _, candidate := range attributes {
		if string(candidate.Key) == key {
			return candidate.Value.AsInt64()
		}
	}
	return 0
}
