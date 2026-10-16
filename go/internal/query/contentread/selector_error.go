// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// writeContentSelectorError answers a repository-selector resolution failure on
// the content read and search routes in the shared selector order: a reader
// fence verdict answers 503 through querycontract.WriteGraphReadError, any
// other failed catalog read answers 500 with the fixed
// selector.LookupFailureMessage body and an error on the request span, an
// unmatched selector answers 404 and an ambiguous one 400, each with the typed
// answer's own text (#7626). Any other error is a server fault: it answers
// selector.LookupFailureMessage through tracing.WriteServerFailure (500, or
// 499 on a client cancel), never a 400 that echoes its text (#7674).
// capability names the route's capability for the 503 envelope and must stay
// a plain argument at each call site: the root capability sweep resolves it
// through callers, not through a struct field.
func writeContentSelectorError(w http.ResponseWriter, r *http.Request, err error, capability string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	if selector.WriteLookupFailure(w, r, err) {
		return
	}
	var notFound selector.NotFoundError
	if errors.As(err, &notFound) {
		querycontract.WriteError(w, http.StatusNotFound, notFound.Error())
		return
	}
	var ambiguous selector.AmbiguousError
	if errors.As(err, &ambiguous) {
		querycontract.WriteError(w, http.StatusBadRequest, ambiguous.Error())
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, selector.LookupFailureMessage)
}
