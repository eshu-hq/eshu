# Completed Work Reopen

`POST /api/v0/admin/reopen` reopens completed work for one domain and scope.
It is the admin surface for the #7285 hand-SQL repair (`ReplayDomain` has no
other production caller; replay requeues only `dead_letter` and `failed`
rows). The workflow it mirrors is
[Safe Replay Workflow](status-admin.md#safe-replay-workflow).

## Three domains only

- `repo_dependency` reopens one completed intent row per acceptance unit at
  the accepted source run — the most recently accepted run when a unit
  carries more than one. One row is enough because the unit's cycle loads
  every row of the unit.
- `workload_materialization` and `submodule_pin` reopen succeeded reducer
  rows.

Every other domain is refused with `422`. The refusal happens before the
idempotency claim, so the key is not consumed.

## Scope and generation resolution

`scope_id` accepts the raw scope id or the scope's source key, resolved by the
shared skip/reopen resolver. An unknown
scope is `404` and a scope with no active generation is `422`, both refused
before the idempotency claim so the key stays usable for a corrected retry.
(On a resolve race after the claim the same refused body is returned and the
key stays in progress.)
A selector matching more than one scope (one scope's id colliding with
another scope's source key) fails closed with `409`, naming the matched
scopes so the operator can resubmit with the exact scope id
([scope selector](status-admin-scope-selector.md)); the refusal
records a `reopen_refused_ambiguous_scope` governance audit event and leaves
the idempotency key unconsumed.
Reducer reopens run against the resolved active generation; intent reopens
select by accepted source run. Intent selection deliberately follows the
accepted run across generations: the newest acceptance for a unit may point
at an intent written by a non-active generation, and that row is the one
reopened. The reported `generation_id` is informational for
`repo_dependency` (scope context, not a selection key). Selection locks
with `SKIP LOCKED` under a
5 s `lock_timeout`, and the reducer reset sets every state column the claim
path reads, as `replaySucceededReducerDomainQuery` does.

## Guardrails

An explicit `reason` and `idempotency_key` are required (`400` when
missing); an admin (all-scopes) token is required (`403` for a scoped
token); each accepted or refused reopen records a governance
`admin_recovery_action` audit event carrying no identifiers; and the shared
`admin_replay_requests` ledger dedupes delivery (a repeated key returns
`duplicate=true` with the combined totals; a key reused with different
selectors, or still in progress, returns `409`).

A `200` with `reopened_total_count` 0 means nothing matched. The first
completion reports the resolved `generation_id` plus per-kind counts and,
for the intent domain, the reopened units with their accepted source runs.
