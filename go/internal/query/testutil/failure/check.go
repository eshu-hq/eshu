// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Assertion names one condition Check holds a route's answer to. Each one is
// evaluated on its own, so a route wrong in two ways reports both.
type Assertion string

// The assertions Check makes. Assertions returns them in evaluation order.
const (
	// NoCanaryInBody: for every case, the body quotes no Canary token.
	NoCanaryInBody Assertion = "body quotes no canary token"
	// FaultStatus: a Fault answers 500.
	FaultStatus Assertion = "fault answers 500"
	// ClientCancelStatus: a ClientCancel answers 499.
	ClientCancelStatus Assertion = "client cancel answers 499"
	// InnerCancelStatus: an InnerCancel on a live request answers 500.
	InnerCancelStatus Assertion = "inner cancel answers 500"
	// FenceStatus: a ReaderFence answers 503.
	FenceStatus Assertion = "reader fence answers 503"
	// FenceCode: a ReaderFence answers a backend_unavailable envelope.
	FenceCode Assertion = "reader fence answers backend_unavailable"
	// FenceRetryAfter: a ReaderFence answer carries Retry-After.
	FenceRetryAfter Assertion = "reader fence answer carries Retry-After"
	// FixedMessage: every case but ReaderFence answers exactly the route's
	// fixed message.
	FixedMessage Assertion = "body is the fixed message"
	// PlainDetailBody: with Route.PlainDetail, every case but ReaderFence
	// answers the fixed message as a plain {"detail": ...} body, not the error
	// envelope.
	PlainDetailBody Assertion = "fixed message is a plain detail body"
	// OneHandlerSpan: with a HandlerTracer, exactly one span opens directly
	// under the request span.
	OneHandlerSpan Assertion = "exactly one handler span"
	// FaultSpanError: a Fault or InnerCancel sets span status Error.
	FaultSpanError Assertion = "fault span status is Error"
	// FaultSpanDescription: a fault span's Error status is described by the
	// route's fixed message. Checked only when the status is Error.
	FaultSpanDescription Assertion = "fault span description is the fixed message"
	// FaultOneException: a fault span records exactly one exception event.
	FaultOneException Assertion = "fault span records one exception"
	// FaultNoCancelEvent: a fault span carries no client-cancel event.
	FaultNoCancelEvent Assertion = "fault span has no cancel event"
	// CancelSpanUnset: a ClientCancel leaves the span status Unset.
	CancelSpanUnset Assertion = "client-cancel span status stays Unset"
	// CancelNoException: a ClientCancel records no exception event.
	CancelNoException Assertion = "client-cancel span records no exception"
	// CancelOneEvent: a ClientCancel adds exactly one
	// tracing.ClientCanceledEvent.
	CancelOneEvent Assertion = "client-cancel span has one cancel event"
	// CancelEventNoAttributes: the client-cancel event carries no attributes.
	CancelEventNoAttributes Assertion = "client-cancel event carries no attributes"
)

// Assertions returns every assertion Check makes, in evaluation order. The
// package's own tests hold one wrong sample route per entry.
func Assertions() []Assertion {
	return []Assertion{
		NoCanaryInBody, FaultStatus, ClientCancelStatus, InnerCancelStatus,
		FenceStatus, FenceCode, FenceRetryAfter, FixedMessage, PlainDetailBody, OneHandlerSpan,
		FaultSpanError, FaultSpanDescription, FaultOneException, FaultNoCancelEvent,
		CancelSpanUnset, CancelNoException, CancelOneEvent, CancelEventNoAttributes,
	}
}

// Violation is one failed assertion and what the route answered instead.
type Violation struct {
	// Assertion is the condition the answer broke.
	Assertion Assertion
	// Detail is what the route answered instead.
	Detail string
}

// Error renders the violation as "<assertion>: <detail>".
func (v Violation) Error() string { return string(v.Assertion) + ": " + v.Detail }

// Check serves route once for tc and returns nil when the answer violates no
// assertion, or an error prefixed "<route> / <case>: " that joins every
// violation. Check returns an error instead of failing a test so the
// package's own tests can prove it rejects wrong routes; Run is the testing
// entry point.
func Check(s Suite, route Route, tc Case) error {
	found, err := Evaluate(s, route, tc)
	if err == nil && len(found) > 0 {
		joined := make([]error, len(found))
		for i := range found {
			joined[i] = found[i]
		}
		err = errors.Join(joined...)
	}
	if err != nil {
		return fmt.Errorf("%s / %s: %w", route.Name, tc.Name, err)
	}
	return nil
}

// Evaluate serves route once for tc and returns every assertion the answer
// violates. The error reports a harness fault, such as an unknown Kind or a
// missing request span, not a wrong route.
func Evaluate(s Suite, route Route, tc Case) ([]Violation, error) {
	rec, ended := Serve(s, route, route.Handler(tc.Err), tc.Kind == ClientCancel)
	found, err := answerViolations(route, tc.Kind, rec)
	if err != nil || tc.Kind == ReaderFence {
		return found, err
	}
	spanFound, err := spanViolations(route.Message, tc.Kind, s.HandlerTracer != nil, ended)
	return append(found, spanFound...), err
}

// statusAssertion returns the status kind must answer and the assertion that
// holds it.
func statusAssertion(kind Kind) (int, Assertion, error) {
	switch kind {
	case Fault:
		return http.StatusInternalServerError, FaultStatus, nil
	case ClientCancel:
		return querycontract.StatusClientClosedRequest, ClientCancelStatus, nil
	case InnerCancel:
		return http.StatusInternalServerError, InnerCancelStatus, nil
	case ReaderFence:
		return http.StatusServiceUnavailable, FenceStatus, nil
	default:
		// A zero-valued variable, not a "" literal: every constant Assertion
		// must be a listed one (TestEveryReportedAssertionIsListed).
		var none Assertion
		return 0, none, fmt.Errorf("unknown failure kind %v", kind)
	}
}

// answerViolations checks the body for a leak, the status for kind, and the
// fence envelope or the fixed message.
func answerViolations(route Route, kind Kind, rec *httptest.ResponseRecorder) ([]Violation, error) {
	want, statusCheck, err := statusAssertion(kind)
	if err != nil {
		return nil, err
	}
	var found []Violation
	body := rec.Body.String()
	for _, token := range canaryTokens {
		if strings.Contains(body, token) {
			found = append(found, Violation{NoCanaryInBody, fmt.Sprintf("body leaked %q: %s", token, body)})
			break
		}
	}
	if rec.Code != want {
		found = append(found, Violation{statusCheck, fmt.Sprintf("status = %d, want %d; body = %s", rec.Code, want, body)})
	}
	if kind == ReaderFence {
		var envelope querycontract.ResponseEnvelope
		err := json.Unmarshal(rec.Body.Bytes(), &envelope)
		if err != nil || envelope.Error == nil || envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
			found = append(found, Violation{FenceCode, fmt.Sprintf("body = %s (decode err %v)", body, err)})
		}
		if rec.Header().Get("Retry-After") == "" {
			found = append(found, Violation{FenceRetryAfter, "no Retry-After header; body = " + body})
		}
		return found, nil
	}
	if got := Message(rec.Body.Bytes()); got != route.Message {
		found = append(found, Violation{FixedMessage, fmt.Sprintf("body message = %q, want %q; body = %s", got, route.Message, body)})
	}
	if route.PlainDetail && plainDetail(rec.Body.Bytes()) == "" {
		found = append(found, Violation{PlainDetailBody, "body has no plain detail text; body = " + body})
	}
	return found, nil
}

// spanViolations finds the span the route marks and checks its status and
// events for kind.
func spanViolations(message string, kind Kind, handlerSpan bool, ended []sdktrace.ReadOnlySpan) ([]Violation, error) {
	span, found, err := markedSpan(handlerSpan, ended)
	if err != nil || span == nil {
		return found, err
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
	add := func(assertion Assertion, format string, args ...any) {
		found = append(found, Violation{assertion, fmt.Sprintf(format, args...)})
	}
	if kind == ClientCancel {
		if status.Code != codes.Unset {
			add(CancelSpanUnset, "status = %v (%q)", status.Code, status.Description)
		}
		if exceptions != 0 {
			add(CancelNoException, "exception events = %d", exceptions)
		}
		if cancelEvents != 1 {
			add(CancelOneEvent, "%s events = %d", tracing.ClientCanceledEvent, cancelEvents)
		}
		if cancelAttrs != 0 {
			add(CancelEventNoAttributes, "%s attributes = %d", tracing.ClientCanceledEvent, cancelAttrs)
		}
		return found, nil
	}
	if status.Code != codes.Error {
		add(FaultSpanError, "status = %v", status.Code)
	} else if status.Description != message {
		add(FaultSpanDescription, "description = %q, want %q", status.Description, message)
	}
	if exceptions != 1 {
		add(FaultOneException, "exception events = %d", exceptions)
	}
	if cancelEvents != 0 {
		add(FaultNoCancelEvent, "%s events = %d", tracing.ClientCanceledEvent, cancelEvents)
	}
	return found, nil
}

// markedSpan returns the request span, or with handlerSpan the one span
// opened directly under it. Any other number of such spans is a
// OneHandlerSpan violation with no span to check further.
func markedSpan(handlerSpan bool, ended []sdktrace.ReadOnlySpan) (sdktrace.ReadOnlySpan, []Violation, error) {
	var request sdktrace.ReadOnlySpan
	for _, span := range ended {
		if span.Name() == requestSpanName && !span.Parent().IsValid() {
			request = span
		}
	}
	if request == nil {
		return nil, nil, fmt.Errorf("no %q span recorded", requestSpanName)
	}
	if !handlerSpan {
		return request, nil, nil
	}
	var children []sdktrace.ReadOnlySpan
	for _, span := range ended {
		if span.Parent().SpanID() == request.SpanContext().SpanID() {
			children = append(children, span)
		}
	}
	if len(children) != 1 {
		return nil, []Violation{{OneHandlerSpan, fmt.Sprintf("spans under the request span = %d, want 1", len(children))}}, nil
	}
	return children[0], nil, nil
}

// plainDetail returns the text of a plain {"detail": ...} body, or "" for any
// other body.
func plainDetail(body []byte) string {
	var plain struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &plain) != nil {
		return ""
	}
	return plain.Detail
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
