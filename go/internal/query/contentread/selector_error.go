// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
)

// writeContentSelectorError answers a repository-selector resolution failure on
// the content read and search routes in the shared selector order: a reader
// fence verdict answers 503 through querycontract.WriteGraphReadError, any
// other failed catalog read answers 500 with the fixed
// selector.LookupFailureMessage body and an error on the request span, an
// unmatched selector answers 404, and anything else (an ambiguous match) 400
// (#7626). capability names the route's capability for the 503 envelope and
// must stay a plain argument at each call site: the root capability sweep
// resolves it through callers, not through a struct field.
func writeContentSelectorError(w http.ResponseWriter, r *http.Request, err error, capability string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	if selector.WriteLookupFailure(w, r, err) {
		return
	}
	status := http.StatusBadRequest
	if selector.IsNotFound(err) {
		status = http.StatusNotFound
	}
	querycontract.WriteError(w, status, err.Error())
}
