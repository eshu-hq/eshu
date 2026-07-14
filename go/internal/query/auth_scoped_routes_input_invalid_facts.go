// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// scopedInputInvalidFactListRoute reports whether r is the bounded durable
// input_invalid quarantine read (issue #4630), mirroring
// scopedDeadLetterListRoute for the sibling dead-letter route.
func scopedInputInvalidFactListRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/admin/input-invalid-facts/query"
}
