# Agent instructions: internal/reducer/code/call/projection

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Runs the controlled code-call projection lane: drains code-call
shared-projection intents one repo/run at a time (issue #6061). Moved out of
the reducer root as its own package. See the README's Purpose and Ownership
boundary sections for what this package owns and reuses from
`intents/shared/worker`.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/call/projection/README.md`
- `go/internal/reducer/intents/shared/worker/README.md` (the shared partition-processing machinery this runner drives)
- `docs/internal/design/reducer-target-tree.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the code-call stanza of
  `compat_projection.go` and `Service.CodeCallProjectionRunner`'s field
  type), never the reverse.
- **`processPartitionOnce` releases the lease with the caller's context**,
  never the derived lease-heartbeat context that `stopHeartbeat` already
  canceled — see the doc comment at its `defer` block.
- **The refresh fence check MUST run before a file-scoped row is written**,
  never skipped as an optimization; see `codeCallProjectionRowBlockedByRepoFence`'s
  doc comment for the race it prevents.

## Common changes

Adding a new completion-history optimization: add its lookup port (mirroring
`CurrentRunPartitionHistoryLookup`), and thread it through
`shouldSkipCodeCallRetract`'s type-assertion chain in `rows.go` in the same
priority order (partition history, then file-scoped refresh history, then
whole-unit history).

## Failure modes to avoid

- Writing a file-scoped row without first checking the refresh fence — this
  reintroduces the race `codeCallProjectionRowBlockedByRepoFence` exists to
  prevent.
- Exporting a new unexported helper "just in case" a future caller needs it.
  `Runner`, `RunnerConfig`, `ReducerGraphDrain`, the reader/lookup
  interfaces, and the three `Default*`/`FilePartitionKeyPrefix` symbols are
  exported because the reducer root's compat forwarders and
  `internal/storage/postgres`'s compile-time interface assertions need them;
  nothing else in this package has an external caller today.

## Do not change without ADR review

- `FilePartitionKeyPrefix()`'s prefix format and the legacy/whole/file
  partition-kind classification (`partitions.go`) — `internal/storage/postgres`
  matches on this exact prefix; changing it is a data-shape change.
