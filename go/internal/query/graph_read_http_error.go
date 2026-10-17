// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// WriteGraphReadError writes the stable HTTP contract for a bounded graph-read
// availability error. It returns false without touching the response when err
// is not one of the shared graph-read errors.
//
// The implementation moved to querycontract for #6060 so a handler-family
// subpackage can write the same contract without importing this package.
func WriteGraphReadError(w http.ResponseWriter, r *http.Request, err error, capability string) bool {
	return querycontract.WriteGraphReadError(w, r, err, capability)
}

// writeReadFailure answers a failed read step of a root route (#7674).
// WriteGraphReadError runs first, so a stale or timed-out PostgreSQL reader
// answers the retryable 503 with Retry-After under capability, and a graph
// verdict its 503 or 504. Anything else answers message through
// tracing.WriteServerFailure: 500 with err recorded on the request span, or
// 499 with only the client-cancel event when the caller canceled the request.
// err's text never reaches the body. capability must be a capability-matrix
// key.
func writeReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
