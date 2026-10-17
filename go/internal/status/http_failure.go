// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// LoadFailureMessage is the fixed body /admin/status answers when the status
	// reader fails. It never carries the reader's error text.
	LoadFailureMessage = "load status report failed"

	// RenderFailureMessage is the fixed body /admin/status answers when the
	// report cannot be rendered. It never carries the renderer's error text.
	RenderFailureMessage = "render status report failed"

	// StatusClientClosedRequest is the nonstandard 499 status an admin route
	// answers when the caller canceled its own request, so a client walking away
	// is not counted as a server fault. It matches the query routes' contract.
	StatusClientClosedRequest = 499

	// ClientCanceledEvent is the span event WriteReadFailure adds, with no
	// attributes and no error text, for a client cancel. It replaces the
	// exception event and Error status a server fault records.
	ClientCanceledEvent = "eshu.request.client_canceled"
)

// WriteReadFailure answers a failed status or metrics read with a fixed body.
//
// message is the only text the client sees: never pass err.Error() or a string
// formatted with err, because backend errors quote SQL, hosts, and credentials.
// A server fault answers 500, records err on the request span (exception event
// and Error status), and logs err server-side so an operator can find it.
//
// A client cancel answers StatusClientClosedRequest (499) with the same fixed
// body: err is not recorded or logged, the span status stays unset, and only
// ClientCanceledEvent is added. It counts as a client cancel only when err
// wraps context.Canceled AND r.Context() itself is canceled; a
// context.Canceled from some inner context on a live request is a fault.
func WriteReadFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	if errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		span.AddEvent(ClientCanceledEvent)
		http.Error(w, message, StatusClientClosedRequest)
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, message)
	slog.ErrorContext(ctx, message, "path", r.URL.Path, "error", err)
	http.Error(w, message, http.StatusInternalServerError)
}
