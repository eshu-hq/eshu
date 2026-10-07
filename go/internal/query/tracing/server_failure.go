// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tracing

import (
	"context"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ClientCanceledEvent is the span event a server-failure helper adds, with no
// attributes and no error text, when the caller canceled its own request. It
// replaces the exception event and Error status a server fault records, so a
// client walking away never reads as a backend failure on the span.
const ClientCanceledEvent = "eshu.request.client_canceled"

// WriteServerFailure answers a server-side read failure with a fixed body.
//
// message is the only text the client sees: never pass err.Error() or a
// string formatted with err, because backend errors quote SQL, Cypher, hosts,
// and credentials. status is http.StatusInternalServerError or
// http.StatusGatewayTimeout (a route's own read budget ran out); any other
// value answers 500. A server fault records err on the request span (the
// exception event) and sets the span status to Error with message as its
// description.
//
// A client cancel answers querycontract.StatusClientClosedRequest (499)
// instead, with the same fixed body: the span status stays unset, err is not
// recorded, and only ClientCanceledEvent is added. It counts as a client
// cancel only when err wraps context.Canceled AND r.Context() itself is
// canceled; a context.Canceled from some inner context on a live request is
// still a server fault.
//
// Call it last, after the route's own 503 verdicts,
// querycontract.WriteGraphReadError, and the route's own 400 sentinels.
func WriteServerFailure(w http.ResponseWriter, r *http.Request, err error, status int, message string) {
	status = markServerFailure(r.Context(), err, status, message)
	querycontract.WriteError(w, status, message)
}

// ServerFailureEnvelope is WriteServerFailure for a seam that returns an
// envelope to its caller instead of writing the response. It returns 500, or
// querycontract.StatusClientClosedRequest for a client cancel of ctx, with an
// internal_error envelope whose Message is message and whose Capability is
// capability. It marks ctx's span exactly as WriteServerFailure marks the
// request span, so every caller of the seam, HTTP or in-process, gets the same
// signal.
func ServerFailureEnvelope(ctx context.Context, err error, message, capability string) (int, *querycontract.ErrorEnvelope) {
	status := markServerFailure(ctx, err, http.StatusInternalServerError, message)
	return status, &querycontract.ErrorEnvelope{
		Code:       querycontract.ErrorCodeInternalError,
		Message:    message,
		Capability: capability,
	}
}

// markServerFailure records the failure on ctx's span and returns the status
// to answer: 499 for a client cancel, otherwise status (504 kept, anything
// else 500).
func markServerFailure(ctx context.Context, err error, status int, message string) int {
	span := trace.SpanFromContext(ctx)
	if errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		span.AddEvent(ClientCanceledEvent)
		return querycontract.StatusClientClosedRequest
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, message)
	if status != http.StatusGatewayTimeout {
		return http.StatusInternalServerError
	}
	return status
}
