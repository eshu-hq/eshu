# Incident HTTP Surface

The HTTP read for bounded PagerDuty incident context: the handler behind
`GET /api/v0/incidents/{incident_id}/context`, its scoped-token
authorization boundary, and its answer-packet companion.

Layout:

- `handler.go` — `IncidentHandler`: route registration, capability and
  profile gating, filter normalization, store dispatch, and the response
  envelope.
- `scope.go` — the scoped-token boundary over the durable
  incident→repository edge (fail-closed not-found on every denial, so a
  scoped caller can never distinguish out-of-grant from nonexistent).
- `answer.go` — the answer-packet companion for the incident response.
- `capabilities.go` — the family's capability registration (lives with the
  routes so this package's tests observe production's profile gates).

The response types live in `incident/model/`, the Postgres reads in
`incident/store/`, the query text in `incident/sql/`. The handler depends
only on the store and authorizer interfaces; wiring builds the concretes.

## Move evidence

The family moved here verbatim from the query root (`incident_context_handler.go`,
`incident_context_scope.go`, plus the answer trio from
`answer_packet_routes.go` whose only caller is this handler); only the
package clause, the `model` / `querycontract` / `auth` / `tracing`
qualifications, and the destuttered file names changed. The capability row
moved from the root contract matrix to `capabilities.go` with identical
ceilings, following the service-leaf precedent.

No-Regression Evidence: baseline `459b83f4c` vs this branch —
`go test ./internal/query/...` passes with 0 failures (counts in the lane
handoff); the handler and scope behavior is pinned by the companion
`handler_test.go` and `scope_test.go`, moved with their subjects. The
capability-gated tests (bounded store, ambiguous candidates, scoped grants)
pass against the leaf registration exactly as they did against the root
matrix.

No-Observability-Change: the route keeps
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total` via the unchanged query-surface
middleware. The handler span keeps its name
(`telemetry.SpanQueryIncidentContext`), route, and capability attributes;
only the tracer handle moved from the root package-local to this
package's local (same underlying provider), so emitted spans and the
dashboards built on them are unaffected. No log text changed.

## Failed reads (#7674)

A failed read on `GET /api/v0/incidents/{incident_id}/context` answers a
fixed message, never the backend error text: `read incident context failed`
for the context read and `incident context authorization failed` for the
scoped-grant check in `scope.go`, which still fails closed.
`writeIncidentReadFailure` runs `querycontract.WriteGraphReadError` first (a
stale or timed-out reader answers the retryable 503 with `Retry-After`), then
`tracing.WriteServerFailure`: `500` with the error on the handler span, or
`499` when the caller canceled the request. A missing incident answers 404
with the `model.ErrIncidentContextNotFound` text, not a wrapped error's text;
the ambiguous 409 keeps its caller-input text.

No-Regression Evidence (#7674): the change runs only after a read has already
returned an error. No SQL, query parameter, call count, or success path
changed. A failure now costs one span `RecordError`/`SetStatus` and a
fixed-string write instead of formatting the error into the body.
`go test ./internal/query/... ./internal/queryplan/... -count=1` and
`go test -race ./internal/query/incident/...` exit 0.

Observability Evidence (#7674): a server fault records the backend error on
the handler span as an `exception` event and sets status Error with the
step's fixed message; a client cancel adds `eshu.request.client_canceled`,
leaves the status Unset, and answers `499`. `server_failure_test.go` asserts
both span shapes for both steps with a recording tracer.
