# AGENTS.md — internal/reducer/maintenance/orphan

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `internal/telemetry` and `pkg/log`. It must
**never** import the parent `internal/reducer` package, directly or
transitively. `PartitionLeaseManager` is a local interface mirror of
the root contract for exactly this reason; interfaces satisfy
structurally, so no alias trick is needed.

## What must stay conservative

- `Runner` MUST claim its single-owner partition lease
  (`graph_orphan_sweep` domain) before sweeping when a `LeaseManager`
  is wired, so concurrent reducer replicas never contend on the same
  static-label Cypher writes.
- The lease release MUST run through a context that survives the
  cycle's own cancellation with a bounded timeout; never release
  through the cancelled cycle context (#6747 shape A).
- Do not lengthen the lease TTL to fit a slow cycle; use the TTL/2
  same-owner re-claim precedent (#4449).

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `runner.go` needs a row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path;
  it carries a `No-Observability-Change` marker naming the covering
  signals, since this leaf registers no instrument of its own.
- **`verify-dirgate.sh`** — re-derive the `internal/reducer` row with
  `verify-dirgate.sh --digest internal/reducer` and regenerate the
  mirror; never hand-edit either.

## Do not

- Do not move a `Service`-level "starts side runner" wiring test into
  this leaf. `Service` is root-owned; that proof stays in
  `internal/reducer` beside the other `TestServiceStarts*` tests
  (`graph_orphan_sweep_runner_service_test.go`).
