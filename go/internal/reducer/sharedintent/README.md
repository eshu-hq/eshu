# internal/reducer/sharedintent

## Purpose

Owns the shape of one shared-domain projection intent, its deterministic
identity, and the freshness key read off it. Plain data and pure functions.

It exists so a domain family can construct and read an intent **without
importing the reducer root**. The root imports the families, so a family
importing the root closes an import cycle — and that cycle is the most common
single reason a family in issue #6061 cannot become a subpackage. Those symbols
are referenced by 47 non-test root files spanning roughly 23 domains.

## Ownership boundary

**Owns:** the intent row and input shapes, the identity derivation, the
acceptance-key shape and the method that reads it off a row.

**Does not own:** anything that runs. The worker, runner, readiness,
lease-heartbeat, unroutable-quarantine and batch-selection machinery — 11
non-test files, roughly 7000 lines — stays in `shared_projection.go` at the
root. Issue #6061 pins it there, and no family needs it.

## Exported surface

| symbol | what it is |
|---|---|
| `Row` | one durable shared-domain projection intent, as stored and drained |
| `Input` | the parameters `Build` needs |
| `Build` | constructs a `Row`, deriving its identity |
| `StableIntentID` | SHA256 over the sorted identity fields |
| `AcceptanceKey` | the bounded-unit freshness slice |
| `Row.AcceptanceKey()` | reads that slice off a row, reporting whether one exists |

The reducer root keeps aliases under the original names — `SharedProjectionIntentRow`,
`SharedProjectionIntentInput`, `SharedProjectionAcceptanceKey` — and a forwarder
for `BuildSharedProjectionIntent`, so callers in `internal/storage/postgres`,
`internal/ifa/materializededges` and `internal/replay/offlinetier` reach these
through the root and were untouched by the hoist.

## Dependencies

The standard library, plus `internal/reducer/payloadcore` for one string
coercion. `payloadcore` imports no reducer root either, so the chain is
leaf-to-leaf with no cycle.

**This package must never import `internal/reducer`**, directly or
transitively. Adding that import defeats the entire reason the package exists
and re-blocks 47 files. If you need a type from the root, either move that type
here too or reconsider whether the code belongs here.

## Telemetry

None. This package registers no metric, span, or log field, and performs no I/O.
The shared-projection instrumentation — `eshu_dp_shared_edge_write_groups_total`,
`eshu_dp_reducer_executions_total`, `eshu_dp_reducer_run_duration_seconds` —
lives with the machinery at the root and is unchanged by the hoist. See the
`No-Observability-Change` row for this file in
`docs/public/observability/telemetry-coverage.md`.

## Gotchas / invariants

**`StableIntentID`'s derivation is a persisted contract.** It serializes the
identity map as compact JSON with sorted keys and hashes it. Two properties
depend on that exact byte sequence:

- *Idempotency under retry.* The same logical work always names the same row, so
  a redelivery updates rather than duplicates. Changing the derivation silently
  orphans every intent already persisted under the old identity.
- *Cross-implementation agreement.* It matches the original Python
  `_stable_intent_id` byte for byte.

`intent_test.go` pins it to an exact digest rather than round-tripping it,
because a round-trip test stays green through precisely that change.

**`IdentityKey` affects the hashed identity only.** It overrides the partition
key for intent-ID construction while the stored `PartitionKey` keeps its
original value — several rows deliberately share one stored partition while
needing distinct intent IDs. A change that let it leak into the stored column
would pass any test that only checked the digest.

**`Row.AcceptanceKey` returns false rather than a zero key.** A row that cannot
name a freshness slice is not eligible for acceptance; returning `true` with a
zero-value key would collapse every such row into one bogus slice. Its fallbacks
to the payload's `scope_id` / `acceptance_unit_id` and then to `RepositoryID`
exist because rows are persisted from more than one path and not all populate
every column — removing one looks harmless in unit tests and silently drops rows
from acceptance in a live drain.

**Adding a field to the identity set inside `Build`** is a `StableIntentID`
change by another route: it alters the hashed bytes. Same rule applies.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/shared-projection.md` — the worker/runner machinery that stays at the root
- `docs/internal/design/package-restructure.md` — the #6061 restructure and this hoist's no-regression evidence
- `docs/public/observability/telemetry-coverage.md` — the coverage row for this file
