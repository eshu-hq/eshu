# Admin Route Failures

The `/api/v0/admin/*` routes answer a failed store, recovery, or catalog step
with a fixed message for that step, never with the backend error text (#7674).
The backend error can quote SQL, relation names, hosts, and connection
settings, so it goes on the request span as an `exception` event and the span
status is Error with the fixed message as its description. The
[Status And Admin](status-admin.md) page describes what each route does.

## Statuses

- A configuration `503` when the route's store, recovery handler, or
  reindex requester is not wired on this API instance, with its own fixed
  text such as `admin store not configured` and no `Retry-After`.
- Request validation answers `400`, and the existing `403`, `404`, `409`,
  and `422` answers do not change.
- On the steps that read through the guarded PostgreSQL reader, a reader
  that missed the writer checkpoint, or whose connection or identity check
  timed out, answers the retryable `503` `backend_unavailable` with
  `Retry-After`. These steps answer the shared graph-read deadline verdict
  with `504` `backend_timeout` too.
- The bounded reads answer `504` with their timeout text when their own
  `timeout_ms` budget expires.
- Any other failed step answers `500` with the fixed message below.

A failed step checks the reader verdict first, then the route timeout, then
answers `500`.

A client that cancels its request gets `499` with the same fixed message. The
request span stays Unset and carries an `eshu.request.client_canceled` event
with no exception. No route documents `499` in the OpenAPI spec, because the
client has already gone. See
[Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).

## Inspection reads

These routes read through `Handler.ReadStore`, the guarded reader, so they
can answer the retryable `503`. The three bounded reads name their
capability-matrix row (`operator.dead_letters.list`,
`operator.reducer_input_invalid_facts.list`, and
`operator.changed_since_poisoned_links.list`) in an error envelope. The work-item,
decision, and replay-event reads, and the repository reindex selector lookup
below, are operator tooling with no capability-matrix row yet, so their error
envelope carries no `capability` field (`querycontract.NoCapability`, #8176).
The status, `Retry-After`, code, and message are the same.

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `POST /api/v0/admin/work-items/query` | work-item read | `work-item query failed` |
| `POST /api/v0/admin/decisions/query` | decision read | `projection decision query failed` |
| `POST /api/v0/admin/decisions/query` | evidence read (`include_evidence`) | `projection decision evidence query failed` |
| `POST /api/v0/admin/replay-events/query` | replay-event read | `replay-event query failed` |
| `POST /api/v0/admin/dead-letters/query` | dead-letter read | `dead-letter query failed`; `504` `dead-letter query timed out` |
| `POST /api/v0/admin/input-invalid-facts/query` | quarantine read | `input-invalid-facts query failed`; `504` `input-invalid-facts query timed out` |
| `POST /api/v0/admin/changed-since/poisoned-links/query` | link read | `changed-since poisoned-links query failed`; `504` `changed-since poisoned-links query timed out` |

A stale reader on the three bounded reads answers the retryable `503`, not
`504`, even though the reader's verdict wraps a deadline.

## Mutations

These routes write through the writer store. Only the repository reindex
selector lookup reads through the guarded reader, so it alone can answer the
retryable `503` or the graph-read `504`.

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `POST /api/v0/admin/refinalize` | refinalize | `refinalize failed` |
| `POST /api/v0/admin/reindex` | fleet watermark | `reindex request failed` |
| `POST /api/v0/admin/reindex` | repository selector lookup | `repository reindex selector lookup failed` |
| `POST /api/v0/admin/reindex` | repository watermarks | `repository reindex request failed` |
| `POST /api/v0/admin/recover-generations` | idempotency claim | `recover-generations idempotency claim failed` |
| `POST /api/v0/admin/recover-generations` | re-enqueue | `recover-generations failed` |
| `POST /api/v0/admin/recover-generations` | ledger finalize | `recover-generations applied but recording its idempotency outcome failed` |
| `POST /api/v0/admin/dead-letter` | dead-letter update | `dead-letter request failed` |
| `POST /api/v0/admin/skip` | skip update | `skip request failed` |
| `POST /api/v0/admin/backfill` | backfill insert | `backfill request failed` |
| `POST /api/v0/admin/replay` | superseded-generation check | `replay superseded-generation check failed` |
| `POST /api/v0/admin/replay` | unsafe-target check | `replay unsafe-target check failed` |
| `POST /api/v0/admin/replay` | idempotency claim | `replay idempotency claim failed` |
| `POST /api/v0/admin/replay` | replay update | `replay failed` |
| `POST /api/v0/admin/replay` | ledger finalize | `replay applied but recording its idempotency outcome failed` |
| `POST /api/v0/admin/reopen` | scope resolution | `reopen scope resolution failed` |
| `POST /api/v0/admin/reopen` | idempotency claim | `reopen idempotency claim failed` |
| `POST /api/v0/admin/reopen` | reopen transaction | `reopen failed` |
| `POST /api/v0/admin/reopen` | ledger finalize | `reopen applied but recording its idempotency outcome failed` |

A selector that matches no repository, or several, still answers `400` and
names the selector. A failed catalog lookup answers `500` without it.

## A 499 on a mutation

A `499` says only that the client left. It does not say that the mutation did
not apply. Before you retry, read the state the route writes:

- The replay and reopen checks, the reopen scope resolution, and the
  repository reindex selector lookup run before anything is written, so a
  `499` there means nothing changed and the idempotency key is unused.
- Refinalize, recover-generations, and reopen write in one transaction. A
  cancel rolls it back unless it lands while the commit runs.
- Dead-letter, skip, backfill, both reindex watermarks, and the idempotency
  claim are single statements. A cancel can arrive after the statement
  committed. Re-running dead-letter, skip, or a reindex is safe. A second
  backfill adds a second request row. A committed claim leaves the key in
  progress, so a retry with that key answers `409`.
- The replay update is a work-item update plus a replay-event insert, not one
  transaction, so a cancel can follow a partial replay. The key stays in
  progress and a retry answers `409`.
- A ledger-finalize failure, `499` included, means the replay, reopen, or
  recovery applied and only its idempotency record did not. The key stays in
  progress and a retry answers `409`. Inspect the work items before you use a
  new key.
