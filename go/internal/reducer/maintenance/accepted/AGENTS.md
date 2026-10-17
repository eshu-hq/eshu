# AGENTS.md — internal/reducer/maintenance/accepted

Scoped instructions for this leaf. The root `AGENTS.md` and the maintenance
parent `AGENTS.md` still apply; these add to them.

## Import rule

This leaf may import `reducer/sharedintent`, `internal/telemetry`, and
`pkg/log`. It must **never** import the parent `internal/reducer`
package, directly or transitively. `Lookup` and `Prefetch` are local
aliases of the root contracts for exactly this reason; extend the mirror
if the root shape changes, do not import root to reach the original.

## What must stay conservative

- The activation fence MUST apply only to source runs carrying a
  relationship generation ID (`requiresRelationshipGenerationGate`).
  Never widen it to a scope-generation-ID path (B-13).
- A non-nil lookup error MUST defer (fail safe), never grant authority.
- `Prefetch`'s nested return type breaks free alias interop one level
  down; do not try to make it interoperate directly. The boundary
  adapter in `cmd/reducer/main_helpers.go` stays the single crossing.

## Gates that will fire on your change

- **`verify-telemetry-coverage.sh`** — `gate.go` needs its row in
  `docs/public/observability/telemetry-coverage.md`, keyed by file path.
- **`verify-dirgate.sh`** — no ledger row to re-derive. A move or shrink needs no
  dirgate action; growth of a directory past max(40, its count at the base) needs a
  `Dirgate-Growth:` trailer. Never edit the write-once baselines.

## Do not

- Do not export a test helper from here for another package to reach, or
  import an unexported helper from elsewhere. Shared metric reads live in
  `maintenance/testutil`; family-local helpers stay in `helpers_test.go`.
