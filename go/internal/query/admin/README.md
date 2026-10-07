# Admin Handler Family

The admin HTTP surface for operators: recovery, work-item inspection,
dead-lettering and dead-letter reads, replay with idempotency, backfill,
replay events, projection decisions, and input-invalid facts.

Layout:

- `handler.go`, `facts.go`, `replay.go`, `replay_explicit.go`, `generations.go`,
  `deadletters.go`, `inputinvalid.go`, `safety.go` — the `Handler` type,
  its routes, the `Store` port, the shared row/filter models, and the
  replay-safety set.
- `reindex.go` — `POST /api/v0/admin/reindex`. With `scope=workspace` (the
  default) it records the fleet reindex watermark through `ReindexRequester`
  and returns it as `requested_at` (#7620). It accepts only
  `ingester=repository` and `force` true or omitted, and rejects unknown
  fields so the retired workspace `path`/`action` body fails instead of being
  ignored.
- `reindex_repository.go` — `scope=repository` with 1 to 100 `repositories`
  selectors. Each selector goes through `RepositoryCatalogMatcher` (the query
  `ContentReader`) and must match exactly one git default-branch scope
  (`git-repository-scope:` prefix, no `@ref`). Otherwise the request is a 400
  naming every bad selector, and nothing is recorded. A catalog failure is a
  500, not a 400. Distinct scopes are recorded in one
  `RepositoryReindexRequester` call and returned per repository.
- `identity/` — tenant identity reads and mutations.
- `provider/config/` — identity provider-config reads and mutations.
- `store/` — the Postgres `Store` implementation and replay ledger.
- `audit/` — the shared audit/permission glue (actor mapping, correlation,
  permission gate, identity hashes).

The OpenAPI fragments documenting these routes live in `openapi/paths/auth/`. The
root `admin_alias.go` keeps every pre-move `query.Admin*` spelling working
so wiring and callers outside the family are untouched.

## Move evidence

This family moved here verbatim from the query root (`admin.go`,
`admin_replay*.go`, and siblings); the replay store is the hot path in this
move because `store/idempotency.go` keeps the `INSERT ... ON CONFLICT DO
NOTHING` replay-claim statement.

No-Regression Evidence: baseline `d215fb2c5` vs this branch —
`go test ./internal/query/...` passes 25 packages with 0 failures, and a
normalized old-vs-new diff of the idempotency file shows the SQL text, call
order, and error wraps are byte-identical (only package and type qualifiers
changed). Input shape and row counts are unchanged because no statement
changed; the existing `admin/store` tests cover the claim and complete
paths.

No-Observability-Change: no instrument was added, removed, or renamed — the
admin routes keep `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`, and the telemetry-coverage row for this
family now points at `handler.go` where the moved `DeadLetterFilter` model
lives.

`Handler.ReadStore` supplies query-only inspection separately from the
writer-backed `Handler.Store`. A nil read port preserves legacy construction;
a configured read port never falls back to the writer after an error. The
three list-only MCP handlers accept `ReadStore` and expose no mutation routes.
