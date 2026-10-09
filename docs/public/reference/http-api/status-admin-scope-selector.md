# Admin Scope Selector

`POST /api/v0/admin/skip` (`repository_id`) and `POST /api/v0/admin/reopen`
(`scope_id`) resolve the operator's scope selector through one shared
resolver: the raw scope id or the scope's source key. Both routes apply the
same decision, so they cannot diverge on the same input (#7732).

- One match resolves to that scope, and the route acts on exactly that
  scope: skip's dead-letter UPDATE filters on the resolved scope id, and
  reopen resolves the scope's active generation from it.
- Zero matches: skip returns `200` with `count` 0 (nothing matched, nothing
  changed); reopen returns `404`.
- More than one match (one scope's id colliding with another scope's source
  key) fails closed with `409` on both routes. The response names the
  matched scopes so the operator can resubmit with the exact scope id, and
  each route records a denied governance audit event
  (`skip_refused_ambiguous_scope`, `reopen_refused_ambiguous_scope`).
  Nothing is dead-lettered or reopened, and on reopen the idempotency key
  is not consumed. There is no schema constraint making the collision
  impossible, so the check runs at resolve time on every call.
