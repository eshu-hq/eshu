# Agent instructions: internal/reducer/code/value/affected

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The value-flow refresh emit gate (issue #6785). See the README for what this
package owns versus the producer handlers and ACK SQL.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/value/affected/README.md`
- `docs/internal/design/6785-value-flow-cloud-sink-refresh.md`

## Invariants

- **NornicDB-safe statements only.** UNWIND-first, single-hop MATCHes, raw
  rows deduped in Go — no aggregates, DISTINCT, WITH, subscripts, OPTIONAL
  MATCH, or variable-length hops (see `TestGateStatementsAvoidMisansweredShapes`).
- **Never alias a RETURN item to an UNWIND variable name.** On NornicDB
  v1.3.3 the output column resolves to the variable's value instead of the
  alias (see the `rid` comment on `reposWithCloudCallersCypher`).
- **No import of the reducer root, ever.** Same leaf rule as `code/value`:
  re-declare the graph runner port locally.
