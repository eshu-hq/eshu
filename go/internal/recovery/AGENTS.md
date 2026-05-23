# internal/recovery Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `replay.go` before changing recovery contracts.
3. `go/internal/storage/postgres/recovery.go` before changing the store
   implementation.
4. `docs/public/deployment/service-runtimes.md` and
   `docs/public/architecture.md` before changing admin recovery behavior.

## Local Rules

- Recovery means queue replay or scoped refinalize, not direct graph mutation.
- Keep `ReplayStore` as the only storage injection point. This package must not
  open Postgres, graph, network, or filesystem connections.
- `ReplayFilter.Stage` must be `projector` or `reducer` before the store is
  called.
- `RefinalizeFilter.ScopeIDs` must stay explicit and non-empty. Unbounded
  refinalize is not supported.
- Handler time must remain UTC so store calls and tests are stable.
- Replay does not prove the root cause is fixed. Dead-lettered work can return
  to dead-letter immediately if the failure mode remains.

## Change Gates

- New recovery operations require validation tests, store interface tests, admin
  handler/docs updates, and operator-observable outcome fields.
- Replay semantics changes require matching Postgres tests for retry state,
  failure metadata, limit behavior, and stage scoping.
- Mass recovery workflows must account for bootstrap phase ordering and
  downstream consumers of reducer-derived state.

## Do Not Change Without Owner Review

- Stage string values.
- Explicit-scope requirement for refinalize.
- Replay-as-queue-work, not direct graph repair.
