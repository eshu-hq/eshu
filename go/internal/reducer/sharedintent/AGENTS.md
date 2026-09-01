# Agent instructions: internal/reducer/sharedintent

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The shape of one shared-domain projection intent (`Row`), its parameters
(`Input`), its deterministic identity (`Build`, `StableIntentID`), and the
bounded-unit freshness key read off it (`AcceptanceKey`, `Row.AcceptanceKey`).

It exists so a domain family can construct and read an intent without importing
the reducer root. That is not a style preference: the root imports the families,
so a family importing the root is an import cycle, and that cycle is what blocks
most of the remaining moves in issue #6061.

## Hard rules

**Never import the reducer root**, directly or transitively. If you find
yourself wanting a type from `internal/reducer`, the answer is either to move
that type here too, or that the code you are writing does not belong in this
package. Adding the import defeats the entire reason this package exists and
re-blocks 47 files.

**Keep it plain data and pure functions.** No queue handle, no graph handle, no
worker, no lease, no `context.Context`, no I/O. The current dependency set is
the standard library plus `payloadcore` for one string coercion. A new
dependency is a design change to be justified in the PR, not a convenience.

**Do not change `StableIntentID`'s derivation.** It is a persisted contract in
two directions:

- Every intent already in Postgres is keyed by it. Changing the byte sequence
  orphans in-flight rows rather than updating them, which breaks idempotency
  under retry — the property the whole intent mechanism rests on.
- It matches the original Python `_stable_intent_id` exactly: compact JSON,
  sorted keys, `{"identity":{...}}`, SHA256, lowercase hex.

`internal/reducer/AGENTS.md` already warns against touching it without auditing
all in-flight intents. If you believe it must change, that is an owner decision
with a migration, not a refactor.

## Changing `Row` or `Input`

Adding a field is usually safe. Removing or renaming one is not: the root
aliases these types under their original names, so every caller in
`internal/storage/postgres`, `internal/ifa/materializededges` and
`internal/replay/offlinetier` sees the change immediately even though none of
them import this package directly. Check those three trees before touching a
field.

Adding a field to the **identity set** inside `Build` is a `StableIntentID`
change by another route — it alters the hashed bytes. Same rule applies.

## `Row.AcceptanceKey` fallbacks

The method falls back to the payload's `scope_id` and `acceptance_unit_id` when
the columns are empty, and to `RepositoryID` when no acceptance unit is named.
Those fallbacks exist because rows are persisted from more than one path and not
all of them populate every column. Removing one will look harmless in unit tests
and will silently drop rows from acceptance in a live drain.

Returning `false` means "this row cannot name a freshness slice", which callers
treat as not-eligible rather than as an error. Do not change it to return a
zero-value key with `true`.
