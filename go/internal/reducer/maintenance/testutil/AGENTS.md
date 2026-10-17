# AGENTS.md — internal/reducer/maintenance/testutil

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

Test-only leaf: leaf test packages import it, production code never
does. It must **never** import the parent `internal/reducer` package,
directly or transitively.

## What must stay conservative

- Helpers stay generic over metric names and attributes; expected
  values and metric names live at the call site.
- A helper used by exactly one leaf's tests belongs in that leaf's
  own test files, not here. Hoist only on the second consumer.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `counter.go` needs a row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path;
  it carries a `No-Observability-Change` marker, since test helpers
  register no instrument.
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not add production helpers here to dodge the leaf import rule.
  Shared production contracts go to a shared-core tier
  (`reducer/sharedintent` or a new leaf), never to a test package.
