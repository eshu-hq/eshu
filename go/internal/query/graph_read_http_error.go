// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

// graphReadErrorEnvelope returns the same stable status and error envelope that
// WriteGraphReadError would write, for seams that return an envelope to their
// caller instead of writing the response themselves (for example
// BuildServiceStoryEnvelope).
func graphReadErrorEnvelope(err error, capability string) (int, *ErrorEnvelope, bool) {
	return querycontract.GraphReadErrorEnvelope(err, capability)
}
