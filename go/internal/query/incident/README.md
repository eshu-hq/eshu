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
