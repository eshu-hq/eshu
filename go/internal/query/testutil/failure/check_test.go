// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

const (
	probePath    = "/api/v0/probe"
	probeMessage = "probe read failed"
)

// probeTracer is the sample package's swappable handler tracer, the same
// shape every converted query family keeps.
var probeTracer = tracing.HandlerTracer()

// answer is how a sample route turns its read failure into a response.
type answer func(w http.ResponseWriter, r *http.Request, err error)

// probe is a sample route whose one read fails with err. ownSpan opens a
// handler span with probeTracer before answering.
type probe struct {
	err     error
	ownSpan bool
	answer  answer
}

func (p probe) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+probePath, func(w http.ResponseWriter, r *http.Request) {
		if p.ownSpan {
			var span trace.Span
			r, span = tracing.StartHandlerSpanWith(probeTracer, r, "probe.read", probePath, "probe")
			defer span.End()
		}
		p.answer(w, r, p.err)
	})
}

// correct is the converted shape: the reader fence first, then the shared
// server-failure helper with a fixed message.
func correct(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "probe") {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, probeMessage)
}

// leaky marks the span and picks the status exactly as the helper does, then
// echoes the backend error beside the fixed message.
func leaky(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "probe") {
		return
	}
	marked := httptest.NewRecorder()
	tracing.WriteServerFailure(marked, r, err, http.StatusInternalServerError, probeMessage)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(marked.Code)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": probeMessage, "debug": err.Error()})
}

// fenceAs500 skips the reader-fence mapping, so a stale reader reads as a
// server fault.
func fenceAs500(w http.ResponseWriter, r *http.Request, err error) {
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, probeMessage)
}

// cancelAs500 is the pre-#7674 shape: every failure records an exception,
// sets Error, and answers 500, a client cancel included.
func cancelAs500(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "probe") {
		return
	}
	span := trace.SpanFromContext(r.Context())
	span.RecordError(err)
	span.SetStatus(codes.Error, probeMessage)
	querycontract.WriteError(w, http.StatusInternalServerError, probeMessage)
}

// cancelStatus500 marks a client cancel on the span exactly as the helper
// does but answers 500 instead of 499.
func cancelStatus500(w http.ResponseWriter, r *http.Request, err error) {
	if tracing.ClientCanceled(r.Context(), err) {
		trace.SpanFromContext(r.Context()).AddEvent(tracing.ClientCanceledEvent)
		querycontract.WriteError(w, http.StatusInternalServerError, probeMessage)
		return
	}
	correct(w, r, err)
}

// cancelRecordsException answers a client cancel 499 with the cancel event
// but still records the error as an exception.
func cancelRecordsException(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		span := trace.SpanFromContext(r.Context())
		span.RecordError(err)
		span.AddEvent(tracing.ClientCanceledEvent)
		querycontract.WriteError(w, querycontract.StatusClientClosedRequest, probeMessage)
		return
	}
	correct(w, r, err)
}

// innerCancelAs499 drops the request-context half of the cancel test, so a
// context.Canceled from an inner context on a live request hides a fault.
func innerCancelAs499(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		trace.SpanFromContext(r.Context()).AddEvent(tracing.ClientCanceledEvent)
		querycontract.WriteError(w, querycontract.StatusClientClosedRequest, probeMessage)
		return
	}
	correct(w, r, err)
}

// wrongMessage answers with text other than the route's fixed message.
func wrongMessage(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "probe") {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, "something else")
}

// probeSuite builds a one-route suite whose route answers with respond.
// handlerSpan names probeTracer as the suite's handler tracer; ownSpan makes
// the route actually open a span with it.
func probeSuite(handlerSpan, ownSpan bool, respond answer) failure.Suite {
	suite := failure.Suite{Routes: []failure.Route{{
		Name: "probe", Path: probePath, Message: probeMessage,
		Handler: func(err error) failure.Mounter { return probe{err: err, ownSpan: ownSpan, answer: respond} },
	}}}
	if handlerSpan {
		suite.HandlerTracer = &probeTracer
	}
	return suite
}

// TestCheckPassesConvertedRoute is the GREEN half: a route built on
// querycontract.WriteGraphReadError and tracing.WriteServerFailure passes
// every case, whether it opens its own handler span or answers on the
// request span. Not parallel: the handler-span suite swaps probeTracer.
func TestCheckPassesConvertedRoute(t *testing.T) {
	original := probeTracer
	for _, ownSpan := range []bool{true, false} {
		suite := probeSuite(ownSpan, ownSpan, correct)
		for _, tc := range failure.Cases() {
			if err := failure.Check(suite, suite.Routes[0], tc); err != nil {
				t.Errorf("ownSpan=%v %s: Check() = %v, want nil", ownSpan, tc.Name, err)
			}
		}
		t.Run(fmt.Sprintf("ownSpan=%v", ownSpan), func(t *testing.T) { failure.Run(t, suite) })
	}
	if probeTracer != original {
		t.Fatal("Check left probeTracer swapped; want it restored")
	}
}

// TestCheckCatchesWrongRoutes is the RED half: each sample route is wrong in
// one way, and Check must fail exactly the cases that expose it with the
// named assertion, and pass the rest. Not parallel: it swaps probeTracer.
func TestCheckCatchesWrongRoutes(t *testing.T) {
	faults := []failure.Kind{failure.Fault, failure.ClientCancel, failure.InnerCancel}
	for _, sample := range []struct {
		name    string
		respond answer
		ownSpan bool
		failing []failure.Kind
		want    string
	}{
		{name: "leaks backend text", respond: leaky, ownSpan: true, failing: faults, want: "leaked"},
		{name: "reader fence as 500", respond: fenceAs500, ownSpan: true, failing: []failure.Kind{failure.ReaderFence}, want: "want 503"},
		{name: "client cancel as 500", respond: cancelAs500, ownSpan: true, failing: []failure.Kind{failure.ClientCancel}, want: "want 499"},
		{name: "client cancel as 500 with a clean span", respond: cancelStatus500, ownSpan: true, failing: []failure.Kind{failure.ClientCancel}, want: "want 499"},
		{name: "client cancel records exception", respond: cancelRecordsException, ownSpan: true, failing: []failure.Kind{failure.ClientCancel}, want: "exceptions = 1"},
		{name: "inner cancel as 499", respond: innerCancelAs499, ownSpan: true, failing: []failure.Kind{failure.InnerCancel}, want: "want 500"},
		{name: "wrong fixed message", respond: wrongMessage, ownSpan: true, failing: faults, want: "want the fixed"},
		{name: "no handler span", respond: correct, ownSpan: false, failing: faults, want: "handler span"},
	} {
		suite := probeSuite(true, sample.ownSpan, sample.respond)
		caught := 0
		for _, tc := range failure.Cases() {
			err := failure.Check(suite, suite.Routes[0], tc)
			wantFail := false
			for _, kind := range sample.failing {
				wantFail = wantFail || kind == tc.Kind
			}
			if wantFail && err != nil {
				caught++
			}
			switch {
			case wantFail && err == nil:
				t.Errorf("%s / %s: Check() = nil, want a failure naming %q", sample.name, tc.Name, sample.want)
			case wantFail && !strings.Contains(err.Error(), sample.want):
				t.Errorf("%s / %s: Check() = %v, want it to name %q", sample.name, tc.Name, err, sample.want)
			case !wantFail && err != nil:
				t.Errorf("%s / %s: Check() = %v, want nil", sample.name, tc.Name, err)
			}
		}
		if caught != len(sample.failing) {
			t.Errorf("%s: caught on %d cases, want %d (one per kind %v); a case is missing from Cases()",
				sample.name, caught, len(sample.failing), sample.failing)
		}
	}
}

// recordingTB records Error calls instead of failing the enclosing test.
type recordingTB struct {
	testing.TB
	errors []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Error(args ...any) { r.errors = append(r.errors, fmt.Sprint(args...)) }

// TestRunReportsEveryWrongCase proves Run reports each failing case, named by
// route and case, and stays quiet on a converted route. Not parallel: it
// swaps probeTracer.
func TestRunReportsEveryWrongCase(t *testing.T) {
	wrong := &recordingTB{TB: t}
	failure.Run(wrong, probeSuite(true, true, leaky))
	if len(wrong.errors) != 3 {
		t.Fatalf("Run on a leaking route reported %d errors, want 3 (fault, client cancel, inner cancel): %q",
			len(wrong.errors), wrong.errors)
	}
	for _, msg := range wrong.errors {
		if !strings.HasPrefix(msg, "probe / ") || !strings.Contains(msg, "leaked") {
			t.Fatalf("Run error %q, want it to name the route and the leak", msg)
		}
	}
	clean := &recordingTB{TB: t}
	failure.Run(clean, probeSuite(true, true, correct))
	if len(clean.errors) != 0 {
		t.Fatalf("Run on a converted route reported %q, want nothing", clean.errors)
	}
}

// TestCasesCarryCanaryAndCause pins the case table: one case per kind, every
// error quoting the canary, and each wrapping the cause its kind names.
func TestCasesCarryCanaryAndCause(t *testing.T) {
	t.Parallel()
	seen := map[failure.Kind]bool{}
	for _, tc := range failure.Cases() {
		if seen[tc.Kind] {
			t.Fatalf("kind %v appears twice", tc.Kind)
		}
		seen[tc.Kind] = true
		if !strings.Contains(tc.Err.Error(), failure.Canary) {
			t.Fatalf("%s: error %q does not quote the canary", tc.Name, tc.Err)
		}
		canceled := errors.Is(tc.Err, context.Canceled)
		if wantCanceled := tc.Kind == failure.ClientCancel || tc.Kind == failure.InnerCancel; canceled != wantCanceled {
			t.Fatalf("%s: wraps context.Canceled = %v, want %v", tc.Name, canceled, wantCanceled)
		}
	}
	for _, kind := range []failure.Kind{failure.Fault, failure.ClientCancel, failure.InnerCancel, failure.ReaderFence} {
		if !seen[kind] {
			t.Fatalf("kind %v has no case", kind)
		}
	}
}

// TestMessageReadsBothErrorShapes pins Message on the plain detail body and
// the envelope body.
func TestMessageReadsBothErrorShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ body, want string }{
		{`{"detail":"fixed"}`, "fixed"},
		{`{"error":{"code":"internal_error","message":"fixed"}}`, "fixed"},
		{`not json`, ""},
	} {
		if got := failure.Message([]byte(tc.body)); got != tc.want {
			t.Fatalf("Message(%s) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
