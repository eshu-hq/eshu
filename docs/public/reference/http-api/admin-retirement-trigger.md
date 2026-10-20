# Admin Retirement Trigger

`POST /api/v0/admin/repositories/retire` retires git repositories. It
resolves every repository selector, runs the phase-1 mark-and-fence, then
purges the fenced rows, in that order. The tombstone and read-surface
contract is in [Repositories, Ingesters, Bundles](repositories-ingesters-bundles.md);
the purge signals are in [Retirement Purge Signals](../telemetry/retirement-purge.md).

## Invoke it

```bash
eshu admin retire-repositories \
  --repository acme/api \
  --reason "decommissioned upstream; purge after the fence" \
  --idempotency-key retire-2026-10-19-acme-api
```

Repeat `--repository` for up to 25 repositories. When `--idempotency-key`
is omitted the CLI mints one and prints it to stderr before the call, so a
call that fails after the fence commits stays retryable with the printed
key. A production retirement should still pass an explicit
`--idempotency-key` recorded with the change. The same call as HTTP needs
an admin (all-scopes) token:

```bash
curl -X POST http://localhost:8080/api/v0/admin/repositories/retire \
  -H "Authorization: Bearer $ESHU_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"repositories":["acme/api"],"reason":"decommissioned upstream","idempotency_key":"retire-2026-10-19-acme-api"}'
```

## What runs in order

1. **Resolve.** Every selector must match exactly one git default-branch
   repository. A problem refuses the whole request before anything is
   marked, and the idempotency key stays unconsumed.
2. **Fence.** The phase-1 fence commits the tombstone rows and the
   generation marks in one transaction. Mark and fence are one atomic step
   inside the fence: the trigger never purges what the fence did not mark.
3. **Purge.** Runs only after the fence committed, one transaction per
   repository. A blocked repository keeps its fenced tombstone open for a
   later pass.

## Refusals

| Condition | Status | Retry |
| --- | --- | --- |
| No repositories, over 25, missing reason or key | `400` | Fix the request. |
| Scoped or limited token | `403` | Use an admin (all-scopes) token. |
| Selector matches nothing | `404` | Fix the selector. |
| Selector matches several repositories | `409` | Narrow the selector. |
| Selector is not a git default-branch scope | `422` | Only git repositories retire. |
| Fence blocked (live lease, lock wait, busy claim fence) | `409` | Wait, then retry with the same key. |
| Fence over budget (`request_too_large`) | `409` | Split the request; retrying the same set refuses again. |
| Retirement already open under a different key | `409` | Reuse the original key. |
| Purge finds no open tombstone or a moved tombstone | `409` | Never purged partially; re-fence first. |
| Generations still live (`status: blocked`) | `200` | Retry with the same key after they go terminal. |

## Audit

Every accepted or refused trigger records an `admin_retirement_action`
governance audit event with no repository, scope, or payload identifiers.
Reason codes start with `retire_`: `retire_accepted` when every repository
purged, `retire_accepted_blocked` when a fenced tombstone stays open, and
`retire_refused_*` for every refusal above.

## Signals and retry

A completed purge increments
`eshu_dp_repository_retirements_purged_total` and logs one INFO
`repository retirement trigger accepted` line with the retirement ids, repo
ids, and the completed/blocked counts. A retry with the same idempotency key
resumes the open tombstone rows instead of opening second ones. A completed
tombstone is history: the next ingest of the same repository re-admits it as
a fresh first generation, and there is no undo.
