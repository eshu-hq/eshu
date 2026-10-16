// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package compare

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// Fixed client-facing messages for the failed reads of
// POST /api/v0/compare/environments (#7674). Each read has its own constant,
// so an operator can tell from the body alone which step failed. The body is
// exactly that text: a graph or content-store error quotes Cypher, hosts, and
// credentials, so err.Error() never reaches a 5xx body. The cause is recorded
// on the request span instead.
const (
	compareWorkloadLookupFailedMessage  = "environment comparison workload lookup failed"
	compareServiceEvidenceFailedMessage = "environment comparison service evidence read failed"
	compareLeftSnapshotFailedMessage    = "environment comparison left environment snapshot read failed"
	compareRightSnapshotFailedMessage   = "environment comparison right environment snapshot read failed"
)

// writeCompareReadFailure answers one failed read of the comparison. The
// workload and snapshot reads go to the graph and the service evidence read
// goes through the guarded content reader, so querycontract.WriteGraphReadError
// runs first: a graph outage or a stale or timed-out reader answers the
// retryable 503 with Retry-After, and a graph deadline its 504. Anything else
// answers message through tracing.WriteServerFailure: 500 with err recorded on
// the request span, or 499 with only the client-cancel event when the caller
// canceled its own request. The body keeps the route's plain
// {"error", "detail"} shape, and err's text never reaches it.
func writeCompareReadFailure(w http.ResponseWriter, r *http.Request, err error, message string) {
	if querycontract.WriteGraphReadError(w, r, err, Capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
