# Admin Handler Family

The admin HTTP surface for operators: recovery, work-item inspection,
dead-lettering and dead-letter reads, replay with idempotency, backfill,
replay events, projection decisions, and input-invalid facts.

Layout:

- `handler.go`, `facts.go`, `replay.go`, `generations.go`,
  `deadletters.go`, `inputinvalid.go`, `safety.go` — the `Handler` type,
  its routes, the `Store` port, the shared row/filter models, and the
  replay-safety set.
- `identity/` — tenant identity reads and mutations.
- `provider/config/` — identity provider-config reads and mutations.
- `store/` — the Postgres `Store` implementation and replay ledger.
- `audit/` — the shared audit/permission glue (actor mapping, correlation,
  permission gate, identity hashes).

The OpenAPI fragments documenting these routes stay in the query root. The
root `admin_alias.go` keeps every pre-move `query.Admin*` spelling working
so wiring and callers outside the family are untouched.
