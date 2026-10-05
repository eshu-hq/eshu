// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codetopicparallel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type probeTraceQueryer func(context.Context, string, ...any) (db.Rows, error)

func (q probeTraceQueryer) QueryContext(ctx context.Context, sql string, args ...any) (db.Rows, error) {
	return q(ctx, sql, args...)
}

type probeTraceRows struct {
	remaining  int
	scanErr    error
	scanFailAt int
	scanned    int
	rowErr     error
	closeErr   error
	closeWait  func()
	closes     atomic.Int32
}

func (r *probeTraceRows) Next() bool {
	if r.remaining == 0 {
		return false
	}
	r.remaining--
	return true
}

func (r *probeTraceRows) Scan(dest ...any) error {
	r.scanned++
	if r.scanErr != nil && (r.scanFailAt == 0 || r.scanned == r.scanFailAt) {
		return r.scanErr
	}
	*dest[0].(*string) = "file"
	*dest[1].(*string) = "term"
	return nil
}

func (r *probeTraceRows) Err() error { return r.rowErr }
func (r *probeTraceRows) Close() error {
	r.closes.Add(1)
	if r.closeWait != nil {
		r.closeWait()
	}
	return r.closeErr
}

type probeTraceSet struct {
	readers [Partitions]db.Queryer
	closes  atomic.Int32
}

func (s *probeTraceSet) Reader(i int) (db.Queryer, error) { return s.readers[i], nil }
func (s *probeTraceSet) Close() error {
	s.closes.Add(1)
	return nil
}

type probeTraceStore struct{ set *probeTraceSet }

func (s probeTraceStore) MaxReadConnections() int { return Partitions }
func (s probeTraceStore) BeginReadOnlySnapshotSet(context.Context, int) (db.ReadSnapshotSet, error) {
	return s.set, nil
}

type stalledProbeTraceExporter struct {
	entered chan struct{}
	release chan struct{}
}

func (e *stalledProbeTraceExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	select {
	case e.entered <- struct{}{}:
	default:
	}
	<-e.release
	return nil
}

func (*stalledProbeTraceExporter) Shutdown(context.Context) error { return nil }

func probeTraceAttributes(span sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	attrs := make(map[attribute.Key]attribute.Value)
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value
	}
	return attrs
}

func probeTraceRun(t *testing.T, ctx context.Context, set *probeTraceSet) ([]sdktrace.ReadOnlySpan, error) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	err := probeTraceRunInto(t, ctx, set, recorder)
	return recorder.Ended(), err
}

func probeTraceRunInto(t *testing.T, ctx context.Context, set *probeTraceSet, recorder *tracetest.SpanRecorder) error {
	t.Helper()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, parent := provider.Tracer("test").Start(ctx, "postgres.query")
	terms := make([]string, 16)
	for i := range terms {
		terms[i] = fmt.Sprintf("term-%d", i)
	}
	_, err := Investigate(ctx, probeTraceStore{set}, parent,
		codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 25}, 250, nil, nil,
		func(rows db.Rows) ([]codequery.CodeTopicEvidenceRow, bool, error) { return nil, false, nil })
	parent.End()
	return err
}

func TestInvestigatePartitionSpansKeepOrdinalAndParentAcrossCompletionOrder(t *testing.T) {
	set := &probeTraceSet{}
	for i := range set.readers {
		index := i
		set.readers[i] = probeTraceQueryer(func(ctx context.Context, sql string, _ ...any) (db.Rows, error) {
			if strings.Contains(sql, "jsonb_to_recordset") {
				return &probeTraceRows{}, nil
			}
			time.Sleep(time.Duration(Partitions-index) * time.Millisecond)
			return &probeTraceRows{remaining: index}, nil
		})
	}
	spans, err := probeTraceRun(t, context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != Partitions+1 {
		t.Fatalf("ended spans = %d, want parent plus four children", len(spans))
	}
	var parent sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "postgres.query" {
			parent = span
		}
	}
	if parent == nil {
		t.Fatal("parent span absent")
	}
	seen := make(map[int64]bool)
	for _, span := range spans {
		if span.Name() == "postgres.query" {
			continue
		}
		if span.Name() != "query.code_topic_partition" || span.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Errorf("span %q has wrong name or parent", span.Name())
		}
		if got := span.InstrumentationScope().Name; got != telemetry.DefaultSignalName {
			t.Errorf("partition instrumentation scope = %q, want %q", got, telemetry.DefaultSignalName)
		}
		attrs := probeTraceAttributes(span)
		ordinal := attrs["code_topic.partition"].AsInt64()
		if ordinal < 0 || ordinal >= Partitions || seen[ordinal] {
			t.Errorf("bad or repeated ordinal %d", ordinal)
		}
		seen[ordinal] = true
		if got := attrs["code_topic.partition_rows"].AsInt64(); got != ordinal {
			t.Errorf("partition %d rows = %d", ordinal, got)
		}
		if got := attrs["code_topic.partition_outcome"].AsString(); got != "ok" {
			t.Errorf("partition %d outcome = %q", ordinal, got)
		}
		if span.EndTime().Before(span.StartTime()) {
			t.Errorf("partition %d has negative duration", ordinal)
		}
	}
	if set.closes.Load() != 1 {
		t.Errorf("snapshot closes = %d", set.closes.Load())
	}
}

func TestInvestigatePartitionSpanEndsAfterCursorClose(t *testing.T) {
	set := &probeTraceSet{}
	closing := make(chan struct{})
	release := make(chan struct{})
	for i := range set.readers {
		index := i
		set.readers[i] = probeTraceQueryer(func(_ context.Context, sql string, _ ...any) (db.Rows, error) {
			rows := &probeTraceRows{}
			if index == 0 && !strings.Contains(sql, "jsonb_to_recordset") {
				rows.closeWait = func() {
					close(closing)
					<-release
				}
			}
			return rows, nil
		})
	}
	recorder := tracetest.NewSpanRecorder()
	done := make(chan error, 1)
	go func() { done <- probeTraceRunInto(t, context.Background(), set, recorder) }()
	select {
	case <-closing:
	case <-time.After(time.Second):
		t.Fatal("cursor Close did not start")
	}
	for _, span := range recorder.Ended() {
		if probeTraceAttributes(span)["code_topic.partition"].AsInt64() == 0 && span.Name() == "query.code_topic_partition" {
			t.Fatal("partition span ended before cursor Close returned")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := set.closes.Load(); got != 1 {
		t.Errorf("snapshot closes = %d", got)
	}
}

func TestInvestigatePartitionSpanClassifiesInFlightCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want string
	}{
		{"canceled", func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, "canceled"},
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 20*time.Millisecond)
		}, "deadline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()
			entered := make(chan struct{})
			set := &probeTraceSet{}
			for i := range set.readers {
				index := i
				set.readers[i] = probeTraceQueryer(func(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
					if index == 0 {
						close(entered)
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return &probeTraceRows{}, nil
				})
			}
			if tc.name == "canceled" {
				go func() { <-entered; cancel() }()
			}
			spans, err := probeTraceRun(t, ctx, set)
			if err == nil {
				t.Fatal("expected request cancellation")
			}
			found := false
			for _, span := range spans {
				if span.Name() != "query.code_topic_partition" || probeTraceAttributes(span)["code_topic.partition"].AsInt64() != 0 {
					continue
				}
				found = true
				if got := probeTraceAttributes(span)["code_topic.partition_outcome"].AsString(); got != tc.want {
					t.Errorf("outcome = %q, want %q", got, tc.want)
				}
			}
			if !found {
				t.Fatal("partition zero span absent")
			}
		})
	}
}

func TestInvestigatePartitionSpansDoNotMixConcurrentRequestParents(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var workers sync.WaitGroup
	start := make(chan struct{})
	parents := make(chan sdktrace.ReadOnlySpan, 2)
	requestErrors := make(chan error, 2)
	for request := 0; request < 2; request++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			set := &probeTraceSet{}
			for i := range set.readers {
				set.readers[i] = probeTraceQueryer(func(context.Context, string, ...any) (db.Rows, error) {
					return &probeTraceRows{}, nil
				})
			}
			ctx, parent := provider.Tracer("test").Start(context.Background(), "postgres.query")
			<-start
			terms := make([]string, 16)
			for i := range terms {
				terms[i] = "term"
			}
			_, err := Investigate(ctx, probeTraceStore{set}, parent,
				codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 25}, 250, nil, nil,
				func(db.Rows) ([]codequery.CodeTopicEvidenceRow, bool, error) { return nil, false, nil })
			requestErrors <- err
			parent.End()
		}()
	}
	close(start)
	workers.Wait()
	close(requestErrors)
	for err := range requestErrors {
		if err != nil {
			t.Fatalf("concurrent investigate: %v", err)
		}
	}
	counts := make(map[string]int)
	for _, span := range recorder.Ended() {
		if span.Name() == "postgres.query" {
			parents <- span
			continue
		}
		if span.Name() == "query.code_topic_partition" {
			counts[span.Parent().SpanID().String()]++
		}
	}
	if len(parents) != 2 || len(counts) != 2 {
		t.Fatalf("parents=%d child-parent groups=%d", len(parents), len(counts))
	}
	close(parents)
	for parent := range parents {
		if got := counts[parent.SpanContext().SpanID().String()]; got != Partitions {
			t.Errorf("parent %s children = %d, want %d", parent.SpanContext().SpanID(), got, Partitions)
		}
	}
}

func TestInvestigatePartitionTraceFullBatchQueueDoesNotBlockRequest(t *testing.T) {
	exporter := &stalledProbeTraceExporter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter,
		sdktrace.WithMaxQueueSize(1), sdktrace.WithMaxExportBatchSize(1), sdktrace.WithBatchTimeout(time.Hour)))
	var release sync.Once
	unblock := func() { release.Do(func() { close(exporter.release) }) }
	t.Cleanup(func() {
		unblock()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := provider.Shutdown(shutdownCtx); err != nil {
			t.Errorf("trace provider shutdown: %v", err)
		}
	})
	run := func() error {
		set := &probeTraceSet{}
		for i := range set.readers {
			set.readers[i] = probeTraceQueryer(func(context.Context, string, ...any) (db.Rows, error) {
				return &probeTraceRows{}, nil
			})
		}
		ctx, parent := provider.Tracer("test").Start(context.Background(), "postgres.query")
		defer parent.End()
		terms := make([]string, 16)
		for i := range terms {
			terms[i] = "term"
		}
		_, err := Investigate(ctx, probeTraceStore{set}, parent,
			codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 25}, 250, nil, nil,
			func(db.Rows) ([]codequery.CodeTopicEvidenceRow, bool, error) { return nil, false, nil })
		return err
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exporter.entered:
	case <-time.After(time.Second):
		t.Fatal("batch exporter did not stall")
	}
	// One span is held by ExportSpans and the one-slot queue is full or
	// dropping. A second request must still finish without exporter progress.
	done := make(chan error, 1)
	go func() { done <- run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		unblock()
		t.Fatal("query blocked behind stalled trace exporter")
	}
}

func TestInvestigatePartitionSpanIncludesQueryThroughCloseFailures(t *testing.T) {
	for _, phase := range []string{"query", "scan", "partial_scan", "rows", "close"} {
		t.Run(phase, func(t *testing.T) {
			set := &probeTraceSet{}
			var target *probeTraceRows
			for i := range set.readers {
				index := i
				set.readers[i] = probeTraceQueryer(func(ctx context.Context, sql string, _ ...any) (db.Rows, error) {
					if strings.Contains(sql, "jsonb_to_recordset") {
						return &probeTraceRows{}, nil
					}
					if index != 2 {
						return &probeTraceRows{}, nil
					}
					if phase == "query" {
						return nil, errors.New("private query detail")
					}
					target = &probeTraceRows{remaining: 2}
					switch phase {
					case "scan":
						target.scanErr = errors.New("private scan detail")
					case "partial_scan":
						target.scanErr = errors.New("private scan detail")
						target.scanFailAt = 2
					case "rows":
						target.rowErr = errors.New("private cursor detail")
					case "close":
						target.closeErr = errors.New("private close detail")
					}
					return target, nil
				})
			}
			spans, err := probeTraceRun(t, context.Background(), set)
			if phase == "close" {
				if err != nil {
					t.Fatalf("cursor Close changed prior ignored-error behavior: %v", err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
			if len(spans) != Partitions+1 {
				t.Fatalf("spans = %d", len(spans))
			}
			foundTarget := false
			for _, span := range spans {
				if span.Name() != "query.code_topic_partition" {
					continue
				}
				attrs := probeTraceAttributes(span)
				if attrs["code_topic.partition"].AsInt64() != 2 {
					continue
				}
				foundTarget = true
				wantOutcome := "error"
				if phase == "close" {
					wantOutcome = "ok"
				}
				if attrs["code_topic.partition_outcome"].AsString() != wantOutcome {
					t.Errorf("outcome = %v", attrs["code_topic.partition_outcome"])
				}
				wantRows := int64(0)
				if phase == "partial_scan" {
					wantRows = 1
				}
				if phase == "rows" || phase == "close" {
					wantRows = 2
				}
				if got := attrs["code_topic.partition_rows"].AsInt64(); got != wantRows {
					t.Errorf("rows = %d, want %d", got, wantRows)
				}
				for key, value := range attrs {
					if strings.Contains(string(key), "private") || strings.Contains(value.String(), "private") {
						t.Errorf("private error leaked in %s", key)
					}
				}
			}
			if !foundTarget {
				t.Fatal("partition two span absent")
			}
			if target != nil && target.closes.Load() != 1 {
				t.Errorf("cursor closes = %d", target.closes.Load())
			}
			if set.closes.Load() != 1 {
				t.Errorf("snapshot closes = %d", set.closes.Load())
			}
		})
	}
}
