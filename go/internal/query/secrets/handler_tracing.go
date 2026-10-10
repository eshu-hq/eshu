// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"go.opentelemetry.io/otel/trace"
)

// Fixed failure messages, one per route step. They are the only text a failed
// read puts in a response body: backend errors quote SQL, Cypher, hosts, and
// credentials (#7674).
const (
	iamIdentityTrustChainsFailedMessage          = "secrets/IAM identity trust chain query failed"
	iamPrivilegePostureObservationsFailedMessage = "secrets/IAM privilege posture observation query failed"
	iamAccessPathsFailedMessage                  = "secrets/IAM secret access path query failed"
	iamPostureGapsFailedMessage                  = "secrets/IAM posture gap query failed"
	iamPostureSummaryFailedMessage               = "secrets/IAM posture summary query failed"
	iamGrantPostureFailedMessage                 = "secrets/IAM S3 external-principal grant posture query failed"
)

// secretsHandlerTracer is this package's tracer AND the seam its span tests
// swap. Seeding it from tracing.HandlerTracer keeps the swap private to
// this package rather than mutating what every other importer reads. See
// go/internal/query/package/registry/handler_tracing.go for the identical seam.
var secretsHandlerTracer = tracing.HandlerTracer()

// startQueryHandlerSpan wraps this route's HTTP handler in a stable span and
// attaches low-cardinality route/capability attributes for operator triage.
func startQueryHandlerSpan(r *http.Request, spanName, route, capability string) (*http.Request, trace.Span) {
	return tracing.StartHandlerSpanWith(secretsHandlerTracer, r, spanName, route, capability)
}

// writeSecretsReadFailure answers a failed secrets/IAM read-model or grant
// graph read. A stale or timed-out PostgreSQL reader, a graph outage, or a
// graph deadline gets its shared verdict from
// querycontract.WriteGraphReadError (503 with Retry-After, or 504). Anything
// else answers message through tracing.WriteServerFailure: 500 with err
// recorded on the request span, or 499 with only the client-cancel event when
// the caller canceled the request. err text never reaches the client (#7674).
func writeSecretsReadFailure(w http.ResponseWriter, r *http.Request, err error, capability, message string) {
	if querycontract.WriteGraphReadError(w, r, err, capability) {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
}
