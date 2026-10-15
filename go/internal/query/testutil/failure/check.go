// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Check serves route once for tc and returns nil when the answer is right,
// or an error naming the first wrong part:
//
//   - the body quotes no Canary token, for every case;
//   - Fault and InnerCancel answer 500, ClientCancel 499, ReaderFence 503;
//   - ReaderFence answers a backend_unavailable envelope with Retry-After;
//   - every other case answers exactly route.Message;
//   - on the handler span (or the request span when s.HandlerTracer is nil),
//     a ClientCancel leaves status Unset with no exception event and one
//     attribute-free tracing.ClientCanceledEvent, and a Fault or InnerCancel
//     sets status Error described by route.Message with exactly one exception
//     event and no cancel event.
//
// Check returns an error instead of failing a test so the helper's own tests
// can prove it rejects wrong routes; Run is the testing entry point.
func Check(s Suite, route Route, tc Case) error {
	rec, ended := Serve(s, route, route.Handler(tc.Err), tc.Kind == ClientCancel)
	if err := checkAnswer(route.Message, tc.Kind, rec); err != nil {
		return fmt.Errorf("%s / %s: %w", route.Name, tc.Name, err)
	}
	if tc.Kind == ReaderFence {
		return nil
	}
	if err := checkSpan(route.Message, tc.Kind, s.HandlerTracer != nil, ended); err != nil {
		return fmt.Errorf("%s / %s: %w", route.Name, tc.Name, err)
	}
	return nil
}

// checkAnswer checks the body for a leak, the status for kind, and the fence
// envelope or the fixed message.
func checkAnswer(message string, kind Kind, rec *httptest.ResponseRecorder) error {
	body := rec.Body.String()
	for _, token := range canaryTokens {
		if strings.Contains(body, token) {
			return fmt.Errorf("body leaked backend error text %q: %s", token, body)
		}
	}
	var want int
	switch kind {
	case Fault, InnerCancel:
		want = http.StatusInternalServerError
	case ClientCancel:
		want = querycontract.StatusClientClosedRequest
	case ReaderFence:
		want = http.StatusServiceUnavailable
	default:
		return fmt.Errorf("unknown failure kind %v", kind)
	}
	if rec.Code != want {
		return fmt.Errorf("status = %d, want %d; body = %s", rec.Code, want, body)
	}
	if kind == ReaderFence {
		var envelope querycontract.ResponseEnvelope
		err := json.Unmarshal(rec.Body.Bytes(), &envelope)
		if err != nil || envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable ||
			rec.Header().Get("Retry-After") == "" {
			return fmt.Errorf("reader fence answer = %s (decode err %v, Retry-After %q), want backend_unavailable with Retry-After",
				body, err, rec.Header().Get("Retry-After"))
		}
		return nil
	}
	if got := Message(rec.Body.Bytes()); got != message {
		return fmt.Errorf("body message = %q, want the fixed %q; body = %s", got, message, body)
	}
	return nil
}

// checkSpan finds the span the route marks and checks its status and events
// for kind.
func checkSpan(message string, kind Kind, handlerSpan bool, ended []sdktrace.ReadOnlySpan) error {
	span, err := markedSpan(handlerSpan, ended)
	if err != nil {
		return err
	}
	exceptions, cancelEvents, cancelAttrs := 0, 0, 0
	for _, event := range span.Events() {
		switch event.Name {
		case "exception":
			exceptions++
		case tracing.ClientCanceledEvent:
			cancelEvents++
			cancelAttrs += len(event.Attributes)
		}
	}
	status := span.Status()
	if kind == ClientCancel {
		if status.Code != codes.Unset || exceptions != 0 || cancelEvents != 1 || cancelAttrs != 0 {
			return fmt.Errorf("client-cancel span status = %v, exceptions = %d, %s events = %d (attributes %d); want Unset, 0, 1 (0)",
				status.Code, exceptions, tracing.ClientCanceledEvent, cancelEvents, cancelAttrs)
		}
		return nil
	}
	if status.Code != codes.Error || status.Description != message || exceptions != 1 || cancelEvents != 0 {
		return fmt.Errorf("fault span status = %v (%q), exceptions = %d, %s events = %d; want Error (%q), 1, 0",
			status.Code, status.Description, exceptions, tracing.ClientCanceledEvent, cancelEvents, message)
	}
	return nil
}

// markedSpan returns the request span, or with handlerSpan the one span
// opened directly under it.
func markedSpan(handlerSpan bool, ended []sdktrace.ReadOnlySpan) (sdktrace.ReadOnlySpan, error) {
	var request sdktrace.ReadOnlySpan
	for _, span := range ended {
		if span.Name() == requestSpanName && !span.Parent().IsValid() {
			request = span
		}
	}
	if request == nil {
		return nil, fmt.Errorf("no %q span recorded", requestSpanName)
	}
	if !handlerSpan {
		return request, nil
	}
	var children []sdktrace.ReadOnlySpan
	for _, span := range ended {
		if span.Parent().SpanID() == request.SpanContext().SpanID() {
			children = append(children, span)
		}
	}
	if len(children) != 1 {
		return nil, fmt.Errorf("ended spans under the request span = %d, want exactly one handler span", len(children))
	}
	return children[0], nil
}

// Message returns the text of an error body in either shape: the plain
// {"detail": ...} body or the {"error": {"message": ...}} envelope. It
// returns "" for any other body.
func Message(body []byte) string {
	var plain struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &plain) == nil && plain.Detail != "" {
		return plain.Detail
	}
	var envelope querycontract.ResponseEnvelope
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		return envelope.Error.Message
	}
	return ""
}
