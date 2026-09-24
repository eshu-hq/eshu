// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

import "net/http"

// scopedInputInvalidFactListRoute reports whether r is the bounded durable
// input_invalid quarantine read (issue #4630), mirroring
// scopedDeadLetterListRoute for the sibling dead-letter route.
func scopedInputInvalidFactListRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/admin/input-invalid-facts/query"
}
