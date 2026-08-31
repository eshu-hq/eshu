# Reducer payload core package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/payloadcore/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- This package must remain a leaf below `internal/reducer`. Never import the
  parent reducer package, a family subpackage, storage, graph, queue, or
  telemetry. `internal/facts` and the standard library are the whole budget.
- A symbol qualifies only if it would still be meaningful with the family it
  came from deleted. Naming a domain concept does not disqualify it —
  `OCIRepositoryID`, `SupplyChainWorkloadIDsFromPayload` and
  `SourceOrderKeyField` all do, and all belong here. A DEPENDENCY disqualifies
  it. Handlers, writers,
  lookups, and decisions are a family's product and stay with the family even
  when several families read them.
- Do not consolidate the near-duplicate accessors. `PayloadStr`,
  `PayloadString` and `SemanticPayloadString` differ on non-string values and on
  the `"<nil>"` rendering; `PayloadBool` and `BoolPayload` differ on whether a
  `"true"` string counts. The README carries the table.
- Symbols arriving here are cut-paste, never retyped. The permitted edits are
  capitalizing the declared identifier AND every moved identifier the body
  references — six of the 28 moved bodies reference a sibling that moved with
  them, and capitalizing only the declared name does not compile — plus
  rewriting the doc comment for the exported name.

## Common changes

Hoisting another generic helper: move the body verbatim, capitalize the
identifier, write a real doc comment, and leave an unexported function-statement
forwarder in the file it came from. Never a function-valued variable — this is
on the write path and a func var cannot be inlined.

Deleting a forwarder: only once its last caller in the reducer root has moved
into a family subpackage.

## Failure modes

- Retyping a body instead of moving it silently drops a guard. `PayloadStr`'s
  `"<nil>"` check was lost exactly this way during scoping. Nothing in CI gates
  this: compare each body against the base commit yourself, not the signature,
  because a dropped guard leaves the signature identical.
- Importing the parent package from here creates the cycle this package exists
  to prevent, and it will not be obvious until a family tries to move.
- Adding a domain-aware helper here erodes the boundary that lets families be
  extracted independently.
