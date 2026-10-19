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
- `refused` / `failed` — the fence or a step error stopped the request
  before any purge; see [Admin Route Failures](admin-failures.md).

`duplicate: true` means the idempotency key resumed an open retirement
instead of opening a new one. A `409` names the fence refusal reason
(`projector_lease_live`, `inflight_reducers`, `lock_timeout`,
`claim_fence_busy`, `request_too_large`) or an already-open retirement
under a different key.

Read surfaces report a fenced repository as `retiring` and a purged one as
`retired` (`GET /api/v0/repositories`, index status, freshness, and
`list_indexed_repositories` / `get_index_status`).

## Observability

- `eshu_dp_repository_retirements_total{outcome}` — one `accepted` per
  marked repository plus exactly one terminal outcome per repository.
- `eshu_dp_repository_retirements_purged_total` — purge completions.
- WARN `repository retirement refused at fence`, INFO `repository
  retirement purged`, WARN `repository retirement blocked: terminal
  generations not reached`.
- `admin_retirement_action` audit events for accepted, refused, blocked,
  completed, failed, and denied inputs.

See [Retirement Trigger And Purge Signals](../telemetry/retirement-purge.md).
