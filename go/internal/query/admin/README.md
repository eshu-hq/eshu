# Admin Handler Family

The admin HTTP surface for operators: recovery, work-item inspection,
dead-lettering and dead-letter reads, replay with idempotency, backfill,
replay events, projection decisions, input-invalid facts, and sanctioned
never-claimed workflow retirement for deconfigured instances.

Layout:

- `handler.go`, `facts.go`, `replay.go`, `replay_explicit.go`, `generations.go`,
  `deadletters.go`, `inputinvalid.go`, `safety.go` — the `Handler` type,
  its routes, the `Store` port, the shared row/filter models, and the
  replay-safety set.
- `retire_unclaimed.go` — `POST /api/v0/admin/workflow-retire-unclaimed`.
  Mirrors the skip: the `Store` port's `RetireUnclaimedWorkflowItems`
  moves at most 100 never-claimed pending workflow work items of one
  deconfigured (kind, instance) pair to failed_terminal with the
  operator-retired class; unknown instances 404 and still-configured
  instances 409 with a deny audit event (#10532).
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
- `reindex_progress.go` — `GET /api/v0/admin/reindex/progress`. Read-only:
  it evaluates 1 to 100 `scope_id` parameters against the fleet watermark
  (live `GetReindexState`, or the `fleet_requested_at` override) and each
  scope's per-repository watermark through `ReindexProgressReader`, applying
  the collector's GREATEST-subsumption and satisfied rules to the sweep's
  own `FullReconcileState` read. Counts cover every scope; `scopes` samples
  at most 10, unfinished first. A store fault is a 500
  (`reindex progress query failed`); an unwired reader is a 503.
- `server_failure.go` — the fixed failure messages, one per failing route
  step, and the three helpers every 5xx goes through (#7674). Inspection
  reads answer the shared reader verdict (503 with `Retry-After`) first,
  each under its capability-matrix key (the five unbounded reads'
  keys are registered by #8176), the bounded reads then their
  `timeout_ms` 504, and everything else a 500
  with the step's fixed message through `tracing.WriteServerFailure`; a
  client cancel answers 499 with no span error. Only the three bounded
  reads answer 504: the other inspection reads and the reindex catalog
  lookup run with no route deadline, and no PostgreSQL read produces the
  graph-read deadline verdict, so they answer 503 or 500, never 504. The backend error goes on
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

`Handler.ReadStore` supplies query-only inspection separately from the
writer-backed `Handler.Store`. A nil read port preserves legacy construction;
a configured read port never falls back to the writer after an error. The
three list-only MCP handlers accept `ReadStore` and expose no mutation routes.

## W1 apilib delegation (eshu-hq/eshu-api#456)

The proof-scope handlers (`tuningReport`, `listWorkItems`) delegate to the
slim `go/apilib` module; `Handler.OmitProofReads` (wired from
`CoreServeDeps.AdminProofExternal`, env `ESHU_CORE_ADMIN_PROOF_EXTERNAL=1`,
default false) unmounts exactly those two routes. The delegation adds no
store, SQL, Cypher, lock, or goroutine path: same decode/default/trim/encode
ops, same fixed error texts, same fence-hook ordering. Performance and
observability evidence for this change lives in the PR body, per repo rule
(no ephemeral evidence markers in package docs).
