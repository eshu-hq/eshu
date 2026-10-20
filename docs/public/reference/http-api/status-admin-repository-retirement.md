# Repository Retirement

`POST /api/v0/admin/repository-retirements` retires indexed repositories:
it resolves repository selectors, then runs the tombstone mark, the
phase-1 fence, and the narrowed purge in that order (#8602, #7766). The
CLI form is `eshu admin repository retire --repository <selector>
--reason <why> [--idempotency-key <key>]`.

Retirement is always an explicit operator act: the tombstone reason code is
closed to `operator_retired`, and nothing in the collector emits tombstones
on its own. Deselected repositories stay indexed until an operator retires
them.

## Request

`repositories` takes 1 to 25 selectors (repository ID, scope ID, name,
slug, or path). Every selector must resolve to exactly one git
default-branch repository; a scope ID disambiguates the duplicate-name
case. The request is all or nothing: nothing is marked unless every
selector resolves.

`reason` must explain why the retirement is safe, and `idempotency_key`
makes the request safe under retries. Both are stored and emitted only as
safe hashes — raw reasons and keys never land in a tombstone row, a log,
or an audit event. With no key given, the CLI generates one.

Only an admin (all-scopes) token may retire; a scoped token gets 403.

## Response

A `202` carries the per-repository outcomes inline in `retirements`, each
with `retirement_id`, `repository_id`, `scope_id`, and `outcome`:

- `completed` — the repository was purged and tombstoned. Its scope rows,
  scope- and repo-keyed leftover rows, and fact-key sidecars are deleted;
  the scope delete cascades generations and their fact rows. Tenant and
  role grants for the repository are deliberately dropped and are not
  restored on re-admission; grant again.
- `blocked` — a designed purge exit that wrote nothing, with
  `blocked_reason`, `blocked_detail`, and `blocking_statuses` inline.
  `terminal_not_reached` names live generations only a new fence can move;
  re-POST (same key resumes) after fencing again. `scope_lock_busy` names
  reducer-retention contention; retry the fenced row.
- `refused` — the fence or the mark stopped the request before any purge;
  see [Admin Route Failures](admin-failures.md).
- `failed` — a step error: the purge attempt rolled back and the tombstone
  stays open; retry with the same key.

`duplicate: true` means the idempotency key resumed an open retirement
instead of opening a new one. A `409` names the fence refusal reason
(`projector_lease_live`, `inflight_reducers`, `lock_timeout`,
`claim_fence_busy`, `request_too_large`) or an already-open retirement
under a different key.

When the tombstone mark fails partway through a multi-repository request
(#8614), the repositories marked before the failure stay committed and
open, and the `409` (already-open conflict) or `500` (request failed) body
still carries one `retirements` entry per requested repository in request
order: marked and failing repositories carry their terminal outcome
(`refused` on conflict, `failed` on error), and repositories never
attempted carry the response-only `not_attempted` outcome, which is never
counted and never audited. There is no compensating close: re-POST with
the same key resumes the open rows and converges to the completed
retirement. An already-open conflict means a foreign open row exists under
a different idempotency key; the per-repository entry identifies which
repository.

Read surfaces report a fenced repository as `retiring` and a purged one as
`retired` (`GET /api/v0/repositories`, index status, freshness, and
`list_indexed_repositories` / `get_index_status`).

## Observability

- `eshu_dp_repository_retirements_total{outcome}` -- one `accepted` per
  marked repository plus exactly one terminal outcome per marked repository.
- `eshu_dp_repository_retirements_purged_total` — purge completions.
- WARN `repository retirement refused at fence`, WARN `repository
  retirement mark failed` (a mid-loop mark error stopped the request
  before any fence: `outcome`, the failing `repo_id`, the still-open
  `marked_repo_ids`), INFO `repository retirement purged`, WARN
  `repository retirement blocked: terminal generations not reached`.
- `admin_retirement_action` audit events for accepted, refused, blocked,
  completed, failed, and denied inputs.

See [Retirement Trigger And Purge Signals](../telemetry/retirement-purge.md).

## Inspecting a retirement

The `repository_retirements` row carries the phase state: `phase`
(`fenced` while the narrowed purge owns the row, `done` with `state`
`complete` after it), `intent_barrier_at` (stamped after
the shared-intent delete commits, before the graph retraction waits out
pre-barrier partition leases), and the graph-phase capture
`graph_repo_root` / `graph_scope_ids` (the stable keys the retraction
and the residue census run on once the purge deletes the live rows).
A `complete` row's `rows_deleted` ledger names every deleted-row count;
`failure_class` / `failure_details` name a closed failure instead.
