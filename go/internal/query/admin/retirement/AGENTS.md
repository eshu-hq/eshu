# AGENTS.md — query/admin/retirement guidance for LLM assistants

## Read first

1. `go/internal/query/admin/retirement/README.md` — what this leaf owns
2. `go/internal/query/admin/retirement/doc.go` — the godoc contract
3. `go/internal/query/admin/retirement/trigger.go` — the mark, fence, and
   purge order
4. `go/internal/query/admin/AGENTS.md` — the family failure-answer rules
   (#7674): fixed messages, no error-text leaks, the 499 comment beside
   every mutation call

## Invariants this leaf enforces

- **Trigger order is mark, fence, then purge.** The trigger returns before
  the fence when the mark fails and before the purge when the fence
  refuses. `trigger_test.go` pins the order with scripted ports; a change
  that reorders steps must fail those tests first.
- **Unfenced or live-generation repositories refuse.** A purge with no open
  tombstone errors, and live generations exit `terminal_not_reached`
  without writing. Both paths have tests; do not soften them into
  completions.
- **New families stay out of package admin.** Routes for other families go
  in their own leaf; only the query root `APIRouter` wires this one.
- **No raw operator values in stored or emitted data.** Reason and
  idempotency key travel hashed (`audit.IdentityHash`); audit events carry
  the bounded reason code and the scope hash. `TestAudit*` style fakes must
  assert through the real `governanceaudit.NormalizeEvent` path where one
  exists — a stubbed appender cannot catch a rejected event.
- **Failure answers follow #7674.** Fixed message constants, the shared
  retryable reader verdict under the registered
  `operator.repository_catalog.lookup` key for catalog lookups (register a
  new key in `specs/capability-matrix.v1.yaml` only for a genuinely new
  read), and `tracing.WriteServerFailure` for 5xx. The root query guards
  fail on a new leak site (`TestNoNewQueryErrorTextLeakSites`) or a direct
  status write (`TestNoDirectServerStatusWrites`).
- **OpenAPI stays in lockstep.** The fragment is
  `go/internal/query/openapi/paths/status/repository_retirement.go`,
  concatenated in `openapi/spec.go`, pinned by
  `TestOpenAPISpecAdminPathsMatchMountedContract`, and documented in
  `docs/public/reference/http-api/status-admin-repository-retirement.md`.

## Common changes and how to scope them

- **Change the trigger order or outcomes** → change `trigger.go` and its
  tests first (TDD), then the handler response rendering and the 202
  contract in the OpenAPI fragment and HTTP API doc.
- **Add a route to this leaf** → retirement-family routes only; anything
  else gets its own leaf. Wire through the root `APIRouter`, never from
  package admin.
- **Add a metric** → follow `telemetry-coverage-discipline`: register in
  `telemetry/instruments.go`, emit at the trigger (the owning
  dispatcher), add the coverage row, and run the verifier against the
  reviewed base (`origin/feat-7766-retirement` until #8610 merges).

## What NOT to change without an ADR

- **Going direct to Postgres from the CLI.** The CLI posts through the
  API; the credential bootstrap path is the deliberate exception, not the
  precedent.
- **Automatic (non-operator) tombstone emission.** The tombstone reason
  code is closed to `operator_retired`: retirement is always an explicit
  operator act, never a policy outcome.
