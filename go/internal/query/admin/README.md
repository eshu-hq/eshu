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
