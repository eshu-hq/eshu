// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Canary stands in for backend error text that must never reach a response
// body: a credential, a host, a relation, and a Cypher fragment (#7674). Every
// case error quotes it, and Check scans the body for each of its tokens.
const Canary = `password=hunter2 host=pg-primary.canary.internal:5432 relation "canary_secret_table" MATCH (n)`

// canaryTokens are the parts of Canary a leaking body would carry. Check
// matches each one separately, so a body that quotes only the host or only
// the Cypher still fails.
var canaryTokens = []string{"hunter2", "pg-primary.canary.internal", "canary_secret_table", "MATCH (n)"}

// requestSpanName is the span Serve opens around each request. It stands in
// for the otelhttp server span the API wraps every route in.
const requestSpanName = "request"

// Kind is the way one case's backend read fails.
type Kind int

const (
	// Fault is a server fault whose text carries the Canary: 500 with the
	// route's fixed message, span status Error, one exception event.
	Fault Kind = iota + 1
	// ClientCancel is the caller canceling its own request: the request
	// context is canceled and the error wraps context.Canceled. 499 with the
	// fixed message, span status Unset, only the client-cancel event.
	ClientCancel
	// InnerCancel is a context.Canceled from an inner context while the
	// request is still live. It is a server fault, answered like Fault.
	InnerCancel
	// ReaderFence is a stale guarded PostgreSQL reader (db.ErrReaderStale):
	// 503 backend_unavailable with Retry-After.
	ReaderFence
)

// String names the kind in failure text.
func (k Kind) String() string {
	switch k {
	case Fault:
		return "fault"
	case ClientCancel:
		return "client cancel"
	case InnerCancel:
		return "inner cancel"
	case ReaderFence:
		return "reader fence"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// Case is one failure a route's backend read returns.
type Case struct {
	// Name labels the case in Check and Run errors ("<route> / <case>: ...").
	Name string
	// Kind is the failure class Check holds the answer to.
	Kind Kind
	// Err is the error the route's failing read returns. It quotes Canary.
	Err error
}

// Cases returns one case per Kind. Each call builds fresh errors.
func Cases() []Case {
	canceled := fmt.Errorf("backend: %s: %w", Canary, context.Canceled)
	return []Case{
		{Name: "backend failure", Kind: Fault, Err: errors.New("backend: " + Canary)},
		{Name: "client cancel", Kind: ClientCancel, Err: canceled},
		{Name: "inner context cancel on a live request", Kind: InnerCancel, Err: canceled},
		{Name: "reader stale", Kind: ReaderFence, Err: fmt.Errorf("backend: %s: %w", Canary, db.ErrReaderStale)},
	}
}

// Mounter is the route surface every query handler exposes.
type Mounter interface{ Mount(*http.ServeMux) }

// Route is one converted failure site: the request that reaches it, the
// handler whose read for that step fails with the case error, and the fixed
// message a server fault and a client cancel answer with.
type Route struct {
	// Name labels the route in Check and Run errors ("<route> / <case>: ...").
	Name string
	// Method is the request method; empty means GET.
	Method string
	// Path is the request target, query string included.
	Path string
	// Body is the request body; empty sends none.
	Body string
	// Message is the route's fixed failure message, normally a package
	// constant in the route's own package.
	Message string
	// Handler builds the handler whose read under test fails with err.
	Handler func(err error) Mounter
	// PlainDetail requires every case but ReaderFence to answer the fixed
	// message as a plain {"detail": ...} body. Without it either body shape
	// passes, as Message reads both.
	PlainDetail bool
	// Scope, when set, decorates the request context, for example with a
	// scoped auth grant.
	Scope func(context.Context) context.Context
}

// Suite is one package's set of converted routes.
type Suite struct {
	// HandlerTracer points at the package-level tracer the routes open their
	// handler span with (the var seeded from tracing.HandlerTracer). Serve
	// swaps it for a recording tracer for the request and restores it after,
	// and Check reads the failure off that handler span. Nil means the routes
	// open no span of their own, so Check reads it off the request span.
	HandlerTracer *trace.Tracer
	// Routes are the failure sites the suite drives.
	Routes []Route
}

// Run drives every route through every case and reports each wrong answer as
// a test error prefixed with the route and case names, so one run lists every
// failing site. It deliberately does not open subtests: a t.Run call in this
// non-test file would read as an unregistered query callsite to
// internal/queryplan's inventory (testutil invariant 4). With a HandlerTracer
// it swaps a package-level var, so a test that calls it must not be parallel.
func Run(t testing.TB, s Suite) {
	t.Helper()
	for _, route := range s.Routes {
		for _, tc := range Cases() {
			if err := Check(s, route, tc); err != nil {
				t.Error(err)
			}
		}
	}
}

// Serve serves one request for route through handler on a recording tracer
// and returns the response and every ended span. Only s.HandlerTracer is
// read: when non-nil it is swapped for the recording tracer and restored
// before Serve returns. cancel cancels the request context before the handler
// runs. The request asks for the envelope media type, so envelope-capable
// routes answer one.
func Serve(
	s Suite, route Route, handler Mounter, cancel bool,
) (*httptest.ResponseRecorder, []sdktrace.ReadOnlySpan) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() { _ = provider.Shutdown(context.Background()) }()
	tracer := provider.Tracer("query-failure-test")
	if s.HandlerTracer != nil {
		previous := *s.HandlerTracer
		*s.HandlerTracer = tracer
		defer func() { *s.HandlerTracer = previous }()
	}

	ctx, parent := tracer.Start(context.Background(), requestSpanName)
	if route.Scope != nil {
		ctx = route.Scope(ctx)
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if cancel {
		stop()
	}
	method := route.Method
	if method == "" {
		method = http.MethodGet
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(method, route.Path, strings.NewReader(route.Body)).WithContext(ctx)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	parent.End()
	return rec, recorder.Ended()
}
