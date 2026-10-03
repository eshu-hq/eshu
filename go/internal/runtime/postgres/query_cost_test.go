// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"math"
	"net"
	"os"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	metricpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type queryCostCollector struct {
	collectorpb.UnimplementedTraceServiceServer
	mu    sync.Mutex
	spans []*tracepb.Span
}

func (c *queryCostCollector) Export(_ context.Context, req *collectorpb.ExportTraceServiceRequest) (*collectorpb.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, resource := range req.ResourceSpans {
		for _, scope := range resource.ScopeSpans {
			for _, span := range scope.Spans {
				c.spans = append(c.spans, proto.Clone(span).(*tracepb.Span))
			}
		}
	}
	return &collectorpb.ExportTraceServiceResponse{}, nil
}

func (c *queryCostCollector) snapshot() []*tracepb.Span {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.spans)
}

type queryCostMetricSink struct {
	metricpb.UnimplementedMetricsServiceServer
}

func (*queryCostMetricSink) Export(context.Context, *metricpb.ExportMetricsServiceRequest) (*metricpb.ExportMetricsServiceResponse, error) {
	return &metricpb.ExportMetricsServiceResponse{}, nil
}

type queryCostFixture struct {
	collector *queryCostCollector
	providers *telemetry.Providers
	tracer    trace.Tracer
	parent    trace.SpanContext
	access    *Access
}

func newQueryCostFixture(t *testing.T) *queryCostFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("listen for owned OTLP collector")
	}
	collector := &queryCostCollector{}
	server := grpc.NewServer()
	collectorpb.RegisterTraceServiceServer(server, collector)
	metricpb.RegisterMetricsServiceServer(server, &queryCostMetricSink{})
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-done })
	endpoint := "http://" + listener.Addr().String()
	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"} {
		t.Setenv(key, endpoint)
	}
	for _, key := range []string{"OTEL_EXPORTER_OTLP_INSECURE", "OTEL_EXPORTER_OTLP_TRACES_INSECURE", "OTEL_EXPORTER_OTLP_METRICS_INSECURE"} {
		t.Setenv(key, "true")
	}
	for _, key := range []string{"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS", "OTEL_EXPORTER_OTLP_METRICS_HEADERS"} {
		t.Setenv(key, "")
	}
	oldTracer, oldMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetTracerProvider(oldTracer); otel.SetMeterProvider(oldMeter) })
	bootstrap, err := telemetry.NewBootstrap("reader-signal-cost-test")
	if err != nil {
		t.Fatal("create telemetry bootstrap")
	}
	providers, err := telemetry.NewProviders(t.Context(), bootstrap)
	if err != nil {
		t.Fatal("create production batch OTLP providers")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if providers.Shutdown(ctx) != nil {
			t.Error("shutdown production OTLP providers")
		}
	})
	tracer := providers.TracerProvider.Tracer("reader-signal-cost-test")
	observer, err := NewObserver(providers.MeterProvider.Meter("reader-signal-cost-test"), tracer)
	if err != nil {
		t.Fatal("create reader observer")
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled, Remote: true})
	return &queryCostFixture{collector: collector, providers: providers, tracer: tracer, parent: parent, access: &Access{observer: observer}}
}

func (f *queryCostFixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if f.providers.TracerProvider.ForceFlush(ctx) != nil {
		t.Fatal("flush trace delivery")
	}
}

func (f *queryCostFixture) checkDelivered(t *testing.T, before, count, events int, pid uint32, remote string) {
	t.Helper()
	spans := f.collector.snapshot()
	if len(spans)-before != count {
		t.Fatalf("delivered spans=%d want=%d", len(spans)-before, count)
	}
	traceID, parentID := f.parent.TraceID(), f.parent.SpanID()
	sequences := map[int64]bool{}
	for _, span := range spans[before:] {
		if !bytes.Equal(span.TraceId, traceID[:]) || !bytes.Equal(span.ParentSpanId, parentID[:]) || span.DroppedEventsCount != 0 || len(span.Events) != events {
			t.Fatal("missing, dropped, or uncorrelated query event")
		}
		for _, event := range span.Events {
			if event.Name != readerQueryStartEventName {
				t.Fatal("unexpected query event")
			}
			attrs := map[string]string{}
			var sequence int64
			for _, a := range event.Attributes {
				switch a.Key {
				case string(readerQuerySequenceKey):
					sequence = a.Value.GetIntValue()
				case string(readerQueryPIDKey):
					if a.Value.GetIntValue() != int64(pid) {
						t.Fatal("wrong native event PID")
					}
					attrs[a.Key] = "pid"
				default:
					attrs[a.Key] = a.Value.GetStringValue()
				}
			}
			if sequence <= 0 || sequences[sequence] || attrs[string(readerQueryRoleKey)] != "reader" {
				t.Fatal("wrong role or duplicate event sequence")
			}
			sequences[sequence] = true
			if pid == 0 {
				if len(attrs) != 2 || attrs[string(readerQueryIdentityKey)] != "unavailable" {
					t.Fatal("unavailable identity was misreported")
				}
			} else if len(attrs) != 4 || attrs[string(readerQueryIdentityKey)] != "available" || attrs[string(readerQueryRemoteKey)] != remote || attrs[string(readerQueryPIDKey)] != "pid" {
				t.Fatal("native identity fields were not retained")
			}
		}
	}
}

func TestReaderQuerySignalProductionOTLPDelivery(t *testing.T) {
	fixture := newQueryCostFixture(t)
	pool := sql.OpenDB(idleConnector{})
	defer pool.Close()
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal("borrow local fake lease")
	}
	defer conn.Close()
	ctx := trace.ContextWithRemoteSpanContext(t.Context(), fixture.parent)
	ctx, span := fixture.tracer.Start(ctx, "request.query")
	fixture.access.startReaderQuery(ctx, conn)
	span.End()
	fixture.flush(t)
	fixture.checkDelivered(t, 0, 1, 1, 0, "")
}

func openCostReaderLease(t *testing.T, dsn string) *sql.Conn {
	t.Helper()
	cfg, err := parsePhysicalEndpoint(dsn)
	if err != nil {
		t.Fatal("parse reader fixture")
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	pool := stdlib.OpenDB(*cfg)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal("borrow native reader lease")
	}
	var recovery bool
	var readOnly string
	if conn.QueryRowContext(ctx, "SELECT pg_is_in_recovery(), current_setting('transaction_read_only')").Scan(&recovery, &readOnly) != nil || !recovery || readOnly != "on" {
		_ = conn.Close()
		t.Fatal("native fixture is not a read-only standby")
	}
	return conn
}

func nearestCostDuration(values []time.Duration, percentile int) time.Duration {
	copyValues := slices.Clone(values)
	slices.Sort(copyValues)
	return copyValues[int(math.Ceil(float64(len(copyValues))*float64(percentile)/100))-1]
}

func timedQueryCostPhase(t *testing.T, f *queryCostFixture, conn *sql.Conn, events int) ([]time.Duration, float64) {
	t.Helper()
	const count = 128
	samples := make([]time.Duration, 0, count)
	parent := trace.ContextWithRemoteSpanContext(t.Context(), f.parent)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range count {
		started := time.Now()
		ctx, span := f.tracer.Start(parent, "request.query")
		for range events {
			f.access.startReaderQuery(ctx, conn)
		}
		span.End()
		samples = append(samples, time.Since(started))
	}
	runtime.ReadMemStats(&after)
	return samples, float64(after.Mallocs-before.Mallocs) / count
}

// TestReaderQuerySignalCostNative measures the actual pgx identity getter and
// recording span through production batch enqueue. Export delivery is checked
// outside the timer. This is a local callback experiment, not endpoint p95.
func TestReaderQuerySignalCostNative(t *testing.T) {
	dsn := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if dsn == "" {
		t.Skip("owned read-only PostgreSQL fixture not configured")
	}
	fixture := newQueryCostFixture(t)
	conn := openCostReaderLease(t, dsn)
	defer conn.Close()
	identity := captureReaderBackendIdentity(conn)
	if !identity.available {
		t.Fatal("native identity is unavailable")
	}
	for _, eventCount := range []int{1, 2, 22} {
		for round := range 2 {
			order := []int{0, eventCount, eventCount, 0}
			if round%2 == 1 {
				order = []int{eventCount, 0, 0, eventCount}
			}
			for _, events := range order {
				before := len(fixture.collector.snapshot())
				samples, allocations := timedQueryCostPhase(t, fixture, conn, events)
				fixture.flush(t)
				fixture.checkDelivered(t, before, len(samples), events, identity.pid, identity.remote)
				t.Logf("COST events=%d round=%d samples=%d p50_ns=%d p95_ns=%d process_allocations_per_sample=%.3f", events, round, len(samples), nearestCostDuration(samples, 50).Nanoseconds(), nearestCostDuration(samples, 95).Nanoseconds(), allocations)
			}
		}
	}
}
