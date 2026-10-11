// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package diagnostic

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	statusDiagnosticEnv         = "ESHU_STATUS_DIAGNOSTIC_REPORT"
	statusDiagnosticPath        = "/api/v0/status/ingesters/repository"
	statusDiagnosticHeader      = "X-Eshu-Diagnostic-ID"
	statusDiagnosticRoot        = "eshu.status_diagnostic_request"
	statusDiagnosticDispatch    = "eshu.status_diagnostic_dispatch"
	statusDiagnosticMaxRequests = 256
	statusDiagnosticMaxSpans    = 8192
	statusDiagnosticMaxPending  = 8192
)

type statusDiagnosticContextKey struct{}

type statusDiagnosticRequest struct {
	ID              string `json:"id"`
	TraceID         string `json:"trace_id"`
	StartUnixNano   int64  `json:"start_unix_nano"`
	EndUnixNano     int64  `json:"end_unix_nano"`
	StatusCode      int    `json:"status_code"`
	PostAuthReached bool   `json:"post_auth_reached"`
}

type statusDiagnosticSpan struct {
	RequestID     string `json:"request_id"`
	Name          string `json:"name"`
	SpanID        string `json:"span_id"`
	ParentSpanID  string `json:"parent_span_id"`
	StartUnixNano int64  `json:"start_unix_nano"`
	EndUnixNano   int64  `json:"end_unix_nano"`
	Role          string `json:"role,omitempty"`
	Stage         string `json:"stage,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
	Phase         string `json:"phase,omitempty"`
	QuerySummary  string `json:"query_summary,omitempty"`
}

type statusDiagnosticReport struct {
	SchemaVersion int                       `json:"schema_version"`
	Complete      bool                      `json:"complete"`
	Overflow      bool                      `json:"overflow"`
	Requests      []statusDiagnosticRequest `json:"requests"`
	Spans         []statusDiagnosticSpan    `json:"spans"`
	Errors        []string                  `json:"errors"`
}

type statusDiagnosticSpanKey struct {
	traceID trace.TraceID
	spanID  trace.SpanID
}

type statusDiagnosticPending struct {
	requestID string
	summary   string
}

// Collector retains bounded status-route span evidence until graceful shutdown.
type Collector struct {
	path         string
	tracer       trace.Tracer
	mu           sync.Mutex
	nextID       uint64
	requests     []statusDiagnosticRequest
	requestIndex map[string]int
	active       map[trace.TraceID]string
	pending      map[statusDiagnosticSpanKey]statusDiagnosticPending
	spans        []statusDiagnosticSpan
	overflow     bool
	closed       bool
}

var _ sdktrace.SpanProcessor = (*Collector)(nil)

// New registers a bounded processor only when the report path is set.
func New(getenv func(string) string, provider *sdktrace.TracerProvider) (*Collector, error) {
	path := getenv(statusDiagnosticEnv)
	if path == "" {
		return nil, nil
	}
	if provider == nil {
		return nil, fmt.Errorf("status diagnostic requires tracer provider")
	}
	d := &Collector{
		path:         path,
		tracer:       provider.Tracer("eshu-api/status-diagnostic"),
		requestIndex: make(map[string]int),
		active:       make(map[trace.TraceID]string),
		pending:      make(map[statusDiagnosticSpanKey]statusDiagnosticPending),
	}
	provider.RegisterSpanProcessor(d)
	return d, nil
}

func statusDiagnosticTarget(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == statusDiagnosticPath
}

// Middleware starts a diagnostic parent before the ordinary HTTP server and auth spans.
func (d *Collector) Middleware(next http.Handler) http.Handler {
	if d == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !statusDiagnosticTarget(r) {
			next.ServeHTTP(w, r)
			return
		}
		d.mu.Lock()
		d.nextID++
		id := strconv.FormatUint(d.nextID, 10)
		if d.closed || len(d.requests) >= statusDiagnosticMaxRequests {
			d.overflow = true
			d.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}
		d.requestIndex[id] = len(d.requests)
		d.requests = append(d.requests, statusDiagnosticRequest{ID: id, StartUnixNano: time.Now().UnixNano()})
		d.mu.Unlock()
		ctx := context.WithValue(r.Context(), statusDiagnosticContextKey{}, id)
		ctx, span := d.tracer.Start(ctx, statusDiagnosticRoot, trace.WithNewRoot())
		w.Header().Set(statusDiagnosticHeader, id)
		writer := &statusDiagnosticResponseWriter{ResponseWriter: w}
		completed := false
		defer func() {
			d.mu.Lock()
			index := d.requestIndex[id]
			d.requests[index].EndUnixNano = time.Now().UnixNano()
			if completed {
				d.requests[index].StatusCode = writer.statusCode()
			}
			d.mu.Unlock()
			span.End()
		}()
		next.ServeHTTP(writer, r.WithContext(ctx))
		completed = true
	})
}

// PostAuth marks entry to the API dispatcher, before its writer checkpoint.
func (d *Collector) PostAuth(next http.Handler) http.Handler {
	if d == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !statusDiagnosticTarget(r) {
			next.ServeHTTP(w, r)
			return
		}
		id, _ := r.Context().Value(statusDiagnosticContextKey{}).(string)
		if id == "" {
			next.ServeHTTP(w, r)
			return
		}
		d.mu.Lock()
		if index, ok := d.requestIndex[id]; ok {
			d.requests[index].PostAuthReached = true
		}
		d.mu.Unlock()
		ctx, span := d.tracer.Start(r.Context(), statusDiagnosticDispatch)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusDiagnosticResponseWriter struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the first response status before forwarding it.
func (w *statusDiagnosticResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

// Write records an implicit success status before forwarding the body.
func (w *statusDiagnosticResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusDiagnosticResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// Unwrap preserves access to the original writer through http.ResponseController.
func (w *statusDiagnosticResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func statusDiagnosticSelected(name string) bool {
	switch name {
	case statusDiagnosticRoot, statusDiagnosticDispatch, "eshu-api", "postgres.reader_access", "postgres.status_snapshot":
		return true
	default:
		return false
	}
}

// OnStart records only bounded correlation metadata and a closed status query label.
func (d *Collector) OnStart(parent context.Context, span sdktrace.ReadWriteSpan) {
	name := span.Name()
	if !statusDiagnosticSelected(name) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	id := d.active[span.SpanContext().TraceID()]
	if name == statusDiagnosticRoot {
		id, _ = parent.Value(statusDiagnosticContextKey{}).(string)
		if id == "" || len(d.active) >= statusDiagnosticMaxRequests {
			d.overflow = true
			return
		}
		d.active[span.SpanContext().TraceID()] = id
		if index, ok := d.requestIndex[id]; ok {
			d.requests[index].TraceID = span.SpanContext().TraceID().String()
		}
	}
	if id == "" {
		return
	}
	if len(d.pending) >= statusDiagnosticMaxPending {
		d.overflow = true
		return
	}
	d.pending[statusDiagnosticSpanKey{span.SpanContext().TraceID(), span.SpanContext().SpanID()}] = statusDiagnosticPending{requestID: id, summary: closedStatusQuerySummary(db.QuerySummaryFromContext(parent))}
}

// OnEnd copies allowlisted span facts without retaining events or raw errors.
func (d *Collector) OnEnd(span sdktrace.ReadOnlySpan) {
	if !statusDiagnosticSelected(span.Name()) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	key := statusDiagnosticSpanKey{span.SpanContext().TraceID(), span.SpanContext().SpanID()}
	pending, ok := d.pending[key]
	if !ok {
		return
	}
	delete(d.pending, key)
	if span.Name() == statusDiagnosticRoot {
		delete(d.active, span.SpanContext().TraceID())
	}
	if len(d.spans) >= statusDiagnosticMaxSpans {
		d.overflow = true
		return
	}
	record := statusDiagnosticSpan{
		RequestID: pending.requestID, Name: span.Name(),
		SpanID: span.SpanContext().SpanID().String(), ParentSpanID: span.Parent().SpanID().String(),
		StartUnixNano: span.StartTime().UnixNano(), EndUnixNano: span.EndTime().UnixNano(),
		QuerySummary: pending.summary,
	}
	for _, attr := range span.Attributes() {
		switch string(attr.Key) {
		case "role":
			record.Role = closedDiagnosticValue(attr.Value.AsString(), "writer", "reader")
		case "stage":
			record.Stage = closedDiagnosticValue(attr.Value.AsString(), "writer_checkpoint", "reader_borrow", "reader_identity", "reader_replay", "business_query", "transaction_control")
		case "outcome":
			record.Outcome = closedDiagnosticValue(attr.Value.AsString(), "ok", "error", "deadline", "canceled", "panic", "unknown")
		case "phase":
			record.Phase = closedDiagnosticValue(attr.Value.AsString(), "begin", "jit", "read", "commit", "rollback")
		}
	}
	d.spans = append(d.spans, record)
}

func closedDiagnosticValue(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "unknown"
}

func closedStatusQuerySummary(value string) string {
	switch value {
	case "scope_counts", "generation_counts", "generation_transitions", "active_work_summary", "active_work_summary_model", "producer_activity", "collector_generation_dead_letters", "coordinator", "registry_collectors", "aws_cloud_scans", "aws_freshness", "infra_inventory", "vulnerability_sources", "collector_fact_evidence", "terraform_state", "terraform_state_model", "semantic_extraction":
		return value
	default:
		return ""
	}
}

// Shutdown leaves the retained evidence available for the API's report flush.
func (*Collector) Shutdown(context.Context) error { return nil }

// ForceFlush is a no-op because span callbacks retain records synchronously.
func (*Collector) ForceFlush(context.Context) error { return nil }
