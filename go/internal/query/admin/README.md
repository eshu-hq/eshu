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
  (`recovery.IsGitDefaultBranchScope`: `git-repository-scope:` prefix, a
  non-blank remainder, no `@ref`; surrounding whitespace is trimmed, the same
  as `querycontract.CanonicalRepositoryIDForScopeID`). Otherwise the request is a 400
  naming every bad selector, and nothing is recorded. A catalog failure is a
  500 (`repository reindex selector lookup failed`), or the retryable 503
  for a stale reader, not a 400. Distinct scopes are recorded in one
  `RepositoryReindexRequester` call and returned per repository.
- `server_failure.go` — the fixed failure messages, one per failing route
  step, and the three helpers every 5xx goes through (#7674). Inspection
  reads answer the shared reader verdict (503 with `Retry-After`, or the
  graph-read 504) first, the bounded reads under their capability-matrix
  row and the rest under `querycontract.NoCapability` (no `capability`
  field; #8176 registers their rows), the bounded reads then their
  `timeout_ms` 504, and everything else a 500
  with the step's fixed message through `tracing.WriteServerFailure`; a
  client cancel answers 499 with no span error. The backend error goes on
  the request span, never in the body. `finalize.go` runs the replay,
  reopen, and recover-generations idempotency finalize and accepted audit
  append on a context detached from the request's cancel (2s bound,
  `Handler.FinalizeTimeout`), so a client leaving after the commit cannot
  strand the key in_progress; a finalize failure is always a 500, and a
  finalize after a cancel logs one INFO
  `admin_recovery.finalize_after_client_cancel` line. On any other
  mutation step, 499 only means the
  client left: each call site says what may already have committed. See
  [Admin route failures](../../../../docs/public/reference/http-api/admin-failures.md).
- `identity/` — tenant identity reads and mutations.
- `provider/config/` — identity provider-config reads and mutations.
- `store/` — the Postgres `Store` implementation and replay ledger.
- `audit/` — the shared audit/permission glue (actor mapping, correlation,
  permission gate, identity hashes).

The `/api/v0/admin/*` routes are documented in `openapi/paths/status/admin.go`
and the `/api/v0/auth/admin/*` identity and provider-config routes in
`openapi/paths/auth/`. The
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

## Fixed failure answers (#7674)

No-Regression Evidence: baseline `origin/main` at `7be0f4ce7` versus this
branch. No SQL, Cypher, transaction, or lock path changed: backend
(PostgreSQL), input shape, statement count, and row counts are the same as
the baseline. The handlers change their error branches, and the idempotency
finalize path DID change: for replay, reopen, and recover-generations, the
post-commit `CompleteReplayIdempotency` (one autocommit `UPDATE` by primary
key) and the accepted audit append now run on a context detached from the
request's cancel and bounded at 2s (`finalize.go`), so a client cancel no
longer strands the ledger row in_progress; a finalize failure is always a
500. Measured with stub stores, no live backend: `go test
./internal/query/... -count=1` passes 76 packages (143 live tests skip
without a backend). `server_failure_routes_test.go` has 68 route subtests
and `finalize_test.go` 15; the route subtests failed on the baseline
handlers (71 before the three finalize cancel variants moved to
`finalize_test.go`), and the finalize cancel and bound subtests (6) failed
before the finalize was detached.

No-Observability-Change: no instrument was added or renamed, and the routes
keep `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`. Observability Evidence: a failed step
marks the request span through `tracing.WriteServerFailure` (Error status
with the fixed message and one `exception` event, or only the
`eshu.request.client_canceled` event on a client cancel), pinned by
`assertAdminFailureSpan`. The changed-since child span no longer records a
client cancel as an exception (`TestPoisonedLinkChildSpanSkipsClientCancel`).
A finalize that recorded the outcome after the client left logs one INFO
`admin_recovery.finalize_after_client_cancel` event (`operation`, `route`,
`correlation_id`), pinned by `TestAdminMutationFinalizeOutlivesClientCancel`.
