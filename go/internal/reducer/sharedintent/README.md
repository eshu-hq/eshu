# internal/reducer/sharedintent

The shape of one shared-domain projection intent, its deterministic identity,
and the freshness key read off it. Plain data and pure functions — no queue, no
graph handle, no worker.

## What lives here

| symbol | what it is |
|---|---|
| `Row` | one durable shared-domain projection intent, as stored and drained |
| `Input` | the parameters `Build` needs |
| `Build` | constructs a `Row`, deriving its identity |
| `StableIntentID` | SHA256 over the sorted identity fields |
| `AcceptanceKey` | the bounded-unit freshness slice |
| `Row.AcceptanceKey()` | reads that slice off a row, reporting whether one exists |

## Why this is a leaf, and why it mattered

Domain families build intents. Before this package, `Row`, `Input` and `Build`
lived in the reducer root next to the worker, runner, lease-heartbeat and
batch-selection machinery. A family that only wanted to construct an intent had
to import the root — and the root imports the families. That is an import cycle.

It is the most common single blocker in issue #6061. Measured against
`origin/main`: those three symbols are referenced by **47 non-test files in the
reducer root, spanning roughly 23 domains**. Two families were picked as
"next to move" and disqualified after measurement for exactly this reason —
`crossrepo`'s own exported function signatures name `SharedProjectionIntentRow`,
so it can never become a subpackage while that type is in the root.

Moving the shape out removes the blocker once instead of fighting the same cycle
once per family.

## What deliberately did NOT move

`shared_projection.go` keeps the worker, runner, readiness, lease-heartbeat,
unroutable-quarantine and batch-selection code — 11 non-test files and around
7000 lines. That is the reducer's concurrency core and it is not a shape a
family needs. Issue #6061 pins it to the root, and this change respects that:
what moved is the ~110 lines of data and pure derivation the families actually
consume.

## The identity function is a contract, not an implementation detail

`StableIntentID` serializes the identity map as compact JSON with sorted keys
and hashes it. Two properties depend on that exact byte sequence:

- **Idempotency under retry.** The same logical work always names the same row,
  so a redelivery updates rather than duplicates. Changing the derivation
  silently orphans every intent already persisted under the old identity.
- **Cross-implementation agreement.** It matches the original Python
  `_stable_intent_id` byte for byte. `internal/reducer/AGENTS.md` warns against
  changing it without auditing all in-flight intents in Postgres; that warning
  still applies here.

## Import rule

This package must never import the reducer root, directly or transitively. It
imports `payloadcore` for one string coercion and otherwise only the standard
library. A new dependency here is a design change, not a convenience: the whole
point is that a family can depend on this without depending on the root.

## Compatibility

The root keeps aliases under the original names and one forwarder, so no caller
changed when this moved:

```go
type SharedProjectionIntentRow = sharedintent.Row
type SharedProjectionIntentInput = sharedintent.Input
type SharedProjectionAcceptanceKey = sharedintent.AcceptanceKey

func BuildSharedProjectionIntent(input SharedProjectionIntentInput) SharedProjectionIntentRow
```

Callers in `internal/storage/postgres`, `internal/ifa/materializededges` and
`internal/replay/offlinetier` reach these through the root today and are
untouched by this change.
