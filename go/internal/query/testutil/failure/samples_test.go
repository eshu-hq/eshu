// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// wrongSample is a sample route that is wrong in exactly one way: it violates
// assertion and nothing else, on whichever cases expose it.
type wrongSample struct {
	assertion failure.Assertion
	respond   answer
	// sibling opens a second span directly under the request span.
	sibling bool
}

// wrongSamples holds one sample per assertion Check makes.
// TestEveryAssertionHasOneWrongSample requires this list and Assertions() to
// match one for one, and TestEveryReportedAssertionIsListed requires every
// Assertion that check code reports to be a constant listed in Assertions().
// Together they keep a new condition from landing without a sample that only
// it catches. NoCanaryInBody also has one single-token variant per canary
// token in TestCanaryScanChecksEveryToken, outside this registry.
var wrongSamples = []wrongSample{
	{assertion: failure.NoCanaryInBody, respond: leaky},
	{assertion: failure.FaultStatus, respond: faultAs504},
	{assertion: failure.ClientCancelStatus, respond: cancelStatus500},
	{assertion: failure.InnerCancelStatus, respond: innerCancelStatus499},
	{assertion: failure.FenceStatus, respond: fenceStatus500},
	{assertion: failure.FenceCode, respond: fenceInternalError},
	{assertion: failure.FenceRetryAfter, respond: fenceNoRetryAfter},
	{assertion: failure.FixedMessage, respond: wrongBodyMessage},
	{assertion: failure.OneHandlerSpan, respond: correct, sibling: true},
	{assertion: failure.FaultSpanError, respond: faultNoErrorStatus},
	{assertion: failure.FaultSpanDescription, respond: faultWrongDescription},
	{assertion: failure.FaultOneException, respond: faultTwoExceptions},
	{assertion: failure.FaultNoCancelEvent, respond: faultWithCancelEvent},
	{assertion: failure.CancelSpanUnset, respond: cancelErrorStatus},
	{assertion: failure.CancelNoException, respond: cancelRecordsException},
	{assertion: failure.CancelOneEvent, respond: cancelTwoEvents},
	{assertion: failure.CancelEventNoAttributes, respond: cancelEventWithAttribute},
}

// fault reports whether err is a server fault the route must answer 500:
// neither a reader fence nor a client cancel.
func fault(r *http.Request, err error) bool {
	_, _, fence := querycontract.GraphReadErrorEnvelope(err, "probe")
	return !fence && !tracing.ClientCanceled(r.Context(), err)
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

// leakOnly returns a route that marks the span and picks the status exactly
// as the helper does, then puts only token, one part of the Canary, beside the
// fixed message.
func leakOnly(token string) answer {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if querycontract.WriteGraphReadError(w, r, err, "probe") {
			return
		}
		marked := httptest.NewRecorder()
		tracing.WriteServerFailure(marked, r, err, http.StatusInternalServerError, probeMessage)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(marked.Code)
		_ = json.NewEncoder(w).Encode(map[string]string{"detail": probeMessage, "debug": token})
	}
}

// faultAs504 answers a plain server fault 504; a cancel-wrapped error on a
// live request still answers 500.
func faultAs504(w http.ResponseWriter, r *http.Request, err error) {
	if fault(r, err) && !errors.Is(err, context.Canceled) {
		tracing.WriteServerFailure(w, r, err, http.StatusGatewayTimeout, probeMessage)
		return
	}
	correct(w, r, err)
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

// innerCancelStatus499 marks an inner cancel on a live request as the fault it
// is but answers 499.
func innerCancelStatus499(w http.ResponseWriter, r *http.Request, err error) {
	if fault(r, err) && errors.Is(err, context.Canceled) {
		span := trace.SpanFromContext(r.Context())
		span.RecordError(err)
		span.SetStatus(codes.Error, probeMessage)
		querycontract.WriteError(w, querycontract.StatusClientClosedRequest, probeMessage)
		return
	}
	correct(w, r, err)
}

// fenceAnswer writes the reader-fence answer through edit, which may change
// the status, headers, or body the fence would have answered.
func fenceAnswer(w http.ResponseWriter, r *http.Request, err error, edit func(rec *httptest.ResponseRecorder)) bool {
	rec := httptest.NewRecorder()
	if !querycontract.WriteGraphReadError(rec, r, err, "probe") {
		return false
	}
	edit(rec)
	for key, values := range rec.Header() {
		w.Header()[key] = values
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
	return true
}

// fenceStatus500 answers the reader fence's envelope and Retry-After with
// status 500.
func fenceStatus500(w http.ResponseWriter, r *http.Request, err error) {
	if !fenceAnswer(w, r, err, func(rec *httptest.ResponseRecorder) { rec.Code = http.StatusInternalServerError }) {
		correct(w, r, err)
	}
}

// fenceInternalError answers the reader fence 503 with Retry-After but an
// internal_error code.
func fenceInternalError(w http.ResponseWriter, r *http.Request, err error) {
	if !fenceAnswer(w, r, err, func(rec *httptest.ResponseRecorder) {
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if envelope, ok := body["error"].(map[string]any); ok {
			envelope["code"] = string(querycontract.ErrorCodeInternalError)
		}
		rec.Body.Reset()
		_ = json.NewEncoder(rec.Body).Encode(body)
	}) {
		correct(w, r, err)
	}
}

// fenceNoRetryAfter answers the reader fence 503 envelope without Retry-After.
func fenceNoRetryAfter(w http.ResponseWriter, r *http.Request, err error) {
	if !fenceAnswer(w, r, err, func(rec *httptest.ResponseRecorder) { rec.Header().Del("Retry-After") }) {
		correct(w, r, err)
	}
}

// wrongBodyMessage marks the span with the fixed message but writes other text
// to the body.
func wrongBodyMessage(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "probe") {
		return
	}
	marked := httptest.NewRecorder()
	tracing.WriteServerFailure(marked, r, err, http.StatusInternalServerError, probeMessage)
	querycontract.WriteError(w, marked.Code, "something else")
}

// faultNoErrorStatus records the fault's exception but leaves the span status
// Unset.
func faultNoErrorStatus(w http.ResponseWriter, r *http.Request, err error) {
	if fault(r, err) {
		trace.SpanFromContext(r.Context()).RecordError(err)
		querycontract.WriteError(w, http.StatusInternalServerError, probeMessage)
		return
	}
	correct(w, r, err)
}

// faultWrongDescription sets status Error with a description other than the
// body's fixed message.
func faultWrongDescription(w http.ResponseWriter, r *http.Request, err error) {
	if fault(r, err) {
		span := trace.SpanFromContext(r.Context())
		span.RecordError(err)
		span.SetStatus(codes.Error, "other description")
		querycontract.WriteError(w, http.StatusInternalServerError, probeMessage)
		return
	}
	correct(w, r, err)
}

// faultTwoExceptions records the fault's error twice.
func faultTwoExceptions(w http.ResponseWriter, r *http.Request, err error) {
	if fault(r, err) {
		trace.SpanFromContext(r.Context()).RecordError(err)
	}
	correct(w, r, err)
}

// faultWithCancelEvent marks a server fault correctly and also adds the
// client-cancel event.
func faultWithCancelEvent(w http.ResponseWriter, r *http.Request, err error) {
	if fault(r, err) {
		trace.SpanFromContext(r.Context()).AddEvent(tracing.ClientCanceledEvent)
	}
	correct(w, r, err)
}

// cancelErrorStatus answers a client cancel 499 with the cancel event but sets
// the span status to Error.
func cancelErrorStatus(w http.ResponseWriter, r *http.Request, err error) {
	if tracing.ClientCanceled(r.Context(), err) {
		trace.SpanFromContext(r.Context()).SetStatus(codes.Error, probeMessage)
	}
	correct(w, r, err)
}

// cancelRecordsException answers a client cancel 499 with the cancel event
// but still records the error as an exception.
func cancelRecordsException(w http.ResponseWriter, r *http.Request, err error) {
	if tracing.ClientCanceled(r.Context(), err) {
		trace.SpanFromContext(r.Context()).RecordError(err)
	}
	correct(w, r, err)
}

// cancelTwoEvents adds the client-cancel event twice.
func cancelTwoEvents(w http.ResponseWriter, r *http.Request, err error) {
	if tracing.ClientCanceled(r.Context(), err) {
		trace.SpanFromContext(r.Context()).AddEvent(tracing.ClientCanceledEvent)
	}
	correct(w, r, err)
}

// cancelEventWithAttribute answers a client cancel 499 with one cancel event
// that carries a fixed "error" attribute.
func cancelEventWithAttribute(w http.ResponseWriter, r *http.Request, err error) {
	if tracing.ClientCanceled(r.Context(), err) {
		trace.SpanFromContext(r.Context()).AddEvent(tracing.ClientCanceledEvent,
			trace.WithAttributes(attribute.String("error", "client went away")))
		querycontract.WriteError(w, querycontract.StatusClientClosedRequest, probeMessage)
		return
	}
	correct(w, r, err)
}
