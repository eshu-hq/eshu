// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// writeReadFailure answers a failed read step of an evidence route (#7674).
// querycontract.WriteGraphReadError runs first, so a stale or timed-out
// PostgreSQL reader answers the retryable 503 with Retry-After under
// capability, and a graph verdict its 503 or 504. Anything else answers
// message through tracing.WriteServerFailure: 500 with err recorded on the
// request span, or 499 with only the client-cancel event when the caller
// canceled the request. err's text never reaches the body. capability must
// be a capability-matrix key.
func writeReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
