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
