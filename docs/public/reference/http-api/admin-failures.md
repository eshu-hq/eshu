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
- The request-checkpoint step runs in front of every admin `POST` route,
  that is, every route on this page. A failed
  checkpoint step answers `503` `database read unavailable` with
  `Retry-After`. A checkpoint source that is not wired answers the same
  `503` with no `Retry-After`.
- Request validation answers `400`, and the existing `403`, `404`, `409`,
  and `422` answers do not change.
- On the steps that read through the guarded PostgreSQL reader, a reader
  that missed the writer checkpoint, or whose connection or identity check
  timed out, answers the retryable `503` `backend_unavailable` with
  `Retry-After`.
- The three bounded reads answer `504` with their timeout text when their
  own `timeout_ms` budget expires while the read itself runs. If the budget
  expires earlier, during reader setup (a connection borrow, the identity
  check, or the wait for the replica to reach the checkpoint), the status
  depends on the reader topology. With a single PostgreSQL reader, the
  reader returns its own verdict and the route answers the retryable `503`.
  With reader fleet members, it depends on where the budget expires. While
  the reader waits for a member slot or between member attempts, the reader
  returns the bare deadline and the route answers the `504` timeout. Inside
  a member attempt, the route answers that attempt's reader verdict, the
  retryable `503`. On both paths, a reader replay window that expires while
  the route budget is still live is a reader verdict and answers the
  retryable `503`. Keep `timeout_ms` well above reader setup latency. No
  other admin route answers `504`, and the OpenAPI spec declares `504` on
  those three only.
- Any other failed step answers `500` with the fixed message below.

A failed step checks the reader verdict first, then the route timeout on the
bounded reads, then answers `500`. A `503` with `Retry-After` is a transient
reader or checkpoint condition and is safe to retry. A `503` without
`Retry-After` is a configuration state: a store, recovery handler, reindex
requester, or checkpoint source that is not wired. No `503` body carries
backend error text.

A client that cancels its request gets `499` with the same fixed message,
except at the ledger finalize described below. The
request span stays Unset and carries an `eshu.request.client_canceled` event
with no exception. No route documents `499` in the OpenAPI spec, because the
client has already gone. See
[Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).

## Inspection reads

These routes read through `Handler.ReadStore`, the guarded reader, so they
can answer the retryable `503`. Its error envelope names the read's
capability-matrix key in `capability`, listed per step below (#8176).

| Route | Failed step | Capability | Fixed message |
| --- | --- | --- | --- |
| `POST /api/v0/admin/work-items/query` | work-item read | `operator.work_items.list` | `work-item query failed` |
| `POST /api/v0/admin/decisions/query` | decision read | `operator.decisions.list` | `projection decision query failed` |
| `POST /api/v0/admin/decisions/query` | evidence read (`include_evidence`) | `operator.decision_evidence.list` | `projection decision evidence query failed` |
| `POST /api/v0/admin/replay-events/query` | replay-event read | `operator.replay_events.list` | `replay-event query failed` |
| `POST /api/v0/admin/dead-letters/query` | dead-letter read | `operator.dead_letters.list` | `dead-letter query failed`; `504` `dead-letter query timed out` |
| `POST /api/v0/admin/input-invalid-facts/query` | quarantine read | `operator.reducer_input_invalid_facts.list` | `input-invalid-facts query failed`; `504` `input-invalid-facts query timed out` |
| `POST /api/v0/admin/changed-since/poisoned-links/query` | link read | `operator.changed_since_poisoned_links.list` | `changed-since poisoned-links query failed`; `504` `changed-since poisoned-links query timed out` |

A stale reader on the three bounded reads answers the retryable `503`, not
`504`, even though the reader's verdict wraps a deadline. The work-item,
decision, and replay-event reads run with no route deadline, so they answer
the retryable `503` on a reader verdict and `500` otherwise, never `504`.

## Delta-union completeness

`GET /api/v0/admin/delta-union/completeness` (#7797) maps the shared
graph-read verdicts under the capability-matrix key
`operator.delta_union_completeness.check` (#8292), in its own
`{"status": "error", "detail": ...}` body (#7674):

- A graph outage answers the retryable `503` with `Retry-After` and the fixed
  graph-unavailable detail.
- A graph deadline answers `504` with the fixed deadline detail.
- A caller that sends `Accept: application/eshu.envelope+json` reads the same
  verdicts in the shared error envelope, which names the capability.
- Any other failed ACTIVE or graph read answers `500` with the fixed detail
  `delta-union completeness check failed`, or `499` when the client canceled.
  The ACTIVE read runs on the writer pool, so a PostgreSQL reader verdict does
  not reach this route.
- A filter past the repository cap keeps its `500` with the narrow-the-filter
  text, which names only the counts and the cap. A missing scope filter stays
  `400`, and an unconfigured check stays `503` with no `Retry-After`.

No body carries backend error text.

## Mutations

These routes write through the writer store. Only the repository reindex
and repository retirement selector lookups read through the guarded reader,
so they alone can answer the reader-verdict `503` with `Retry-After`. They
run with no route deadline and never answer `504`. Every mutation can still answer the checkpoint `503`
and its configuration `503`, as described in [Statuses](#statuses).

### Claim-fence busy responses

`POST /api/v0/admin/refinalize` and `POST /api/v0/admin/recover-generations`
make bounded attempts to admit work through the reducer claim fence. If the
fence stays busy, the route answers `409` with `blocked/claim_fence_busy`;
retry after queue contention subsides. For recover-generations, the failed
transaction is rolled back while the idempotency ledger remains `in_progress`,
so retry with a fresh `idempotency_key`.

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
| `POST /api/v0/admin/repository-retirements` | repository selector lookup | `repository retirement selector lookup failed` |
| `POST /api/v0/admin/repository-retirements` | retirement trigger | `repository retirement request failed` |
| `POST /api/v0/admin/reopen` | scope resolution | `reopen scope resolution failed` |
| `POST /api/v0/admin/reopen` | idempotency claim | `reopen idempotency claim failed` |
| `POST /api/v0/admin/reopen` | reopen transaction | `reopen failed` |
| `POST /api/v0/admin/reopen` | ledger finalize | `reopen applied but recording its idempotency outcome failed` |

A selector that matches no repository, or several, still answers `400` and
names the selector. A failed catalog lookup answers `500` without it.

A `409` or `500` from the retirement trigger carries the trigger's
per-repository entries alongside the fixed `detail` (#8614): one
`retirements` entry per requested repository in request order, plus the
`idempotency_key` and the `duplicate` flag. Repositories marked before a
mid-request mark failure stay committed and open with their terminal
outcome (`refused` on conflict, `failed` on error); repositories never
attempted report the response-only `not_attempted` outcome. A retry with
the same key resumes the open rows.

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
- After the replay, reopen, or recovery mutation committed, its idempotency
  finalize and its accepted audit event run on a context the client cancel
  does not reach, bounded at 2 seconds. A client that leaves then still gets
  its outcome recorded: the route answers `200` to nobody, logs one INFO
  `admin_recovery.finalize_after_client_cancel` line with the route and
  correlation id, and a retry with the same key returns the prior outcome
  (`duplicate=true`). These steps never answer `499`.
- A ledger-finalize failure, including that 2-second bound expiring, answers
  `500` with the step's fixed message and an Error span, client cancel or
  not. The replay, reopen, or recovery applied and only its idempotency record
  did not. The key stays in progress and a retry answers `409`. Inspect the
  work items before you use a new key.

The auth, session, and first-run setup routes follow the same rules; see
[Auth and session route failures](auth-session-failures.md).
