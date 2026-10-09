# Queue Status

## Purpose

`internal/status/queue` owns the work-queue coordination family of the
status report: why eligible work is blocked behind a conflict-domain gate,
and what the newest queued-work failure looked like. It exists so the root
`internal/status` package (which aggregates every family into `RawSnapshot`
and `Report`) has one place that owns "is queued work stuck, and on what."

## Ownership boundary

This package owns blockage-row and failure-snapshot shape, normalization,
and plain-text rendering. It does not own the aggregate `QueueSnapshot`
lifecycle counts (pending/in-flight/retrying/succeeded/failed/dead-letter),
which stay in root because they are computed across every domain, not one
family; it does not own domain backlog depth (`DomainBacklog`, also root).

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `queue`; `queue` may import neither the root nor a
sibling leaf.

## Exported surface

- `Blockage` — one conflict-domain-blocked queue row
- `CloneBlockages` — normalize and order blockage rows (biggest, oldest
  first), dropping blank-stage rows
- `RenderBlockageLines` — plain-text operator lines for blocked work
- `FailureSnapshot` — the newest queued-work failure metadata
- `CloneFailure` — defensive copy of a failure snapshot
- `FailureText` — one bounded operator line for a failure snapshot

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/status/shared` — `NonNegativeDuration`

## Telemetry

None. This package performs no I/O and emits no metrics, spans, or logs; it
normalizes and renders rows the status reader already gathered.

## Gotchas / invariants

- `FailureSnapshot` values render only in status text/JSON payloads. They
  must never be promoted to a metric label — `WorkItemID`, `ScopeID`, and
  `GenerationID` are unbounded-cardinality identifiers.
- `FailureText` truncates `FailureMessage` and `FailureDetails` independently
  at 240 characters each (`queueFailureTextLimit`), appending `...`. A
  caller that needs the untruncated value must read the `FailureSnapshot`
  fields directly, not the rendered text.
- `CloneBlockages` silently drops any row whose `Stage` is blank after
  trimming — an empty-stage row never reaches the rendered report.
- `CloneBlockages`'s sort order (blocked count descending, then oldest age
  descending, then stage/domain/conflict-key ascending) is the operator
  report's contract: the biggest, oldest blocker is always first. Do not
  re-sort downstream without a reason documented at the call site.
- `cloneQueueBlockages`, `renderQueueBlockageLines`, `cloneQueueFailure`, and
  `queueFailureText` are called directly from root `status.go` and are
  unexported today (`CloneBlockages`, `RenderBlockageLines`, `CloneFailure`,
  `FailureText` above are their post-move exported names). All four must be
  exported on the move or root's call sites will not compile.

## Evidence

No-Regression Evidence (#6949 batch 2, queue family): this change moves
the two `queue.*` compat entries' Go importers (`QueueBlockage` and
`QueueFailureSnapshot`, reached as both `status.*` and `statuspkg.*`)
off the transitional compat spellings onto `queue.Blockage` and
`queue.FailureSnapshot`, and deletes the emptied `compat_queue.go`. No
type shape, wire field, SQL text, or executable statement changes:
across 28 files, every production hunk requalifies an identifier or
import path only, every other hunk is a package-doc rewording, a
ledger row, or the compat file's own deletion, and the build resolves
with no dangling reference.
Measurement: identical before/after outcomes (ledger:6949-queue-batch2-before,
ledger:6949-queue-batch2-after). The command is `go test -count=1` over
the 7 affected package targets (`./internal/status/...`,
`./internal/query/`, `./internal/mcp/`, `./internal/cli/evbundle/`,
`./internal/cli/localsupervisor/`, `./internal/storage/postgres/`,
`./cmd/eshu/`) on baseline `b815f40364` vs measurement commit
`e16db3f067` (this Evidence section, the two ledger rows, and
content-identical rebases tracking main are the only later changes):
7 packages ok, 0 fail on both sides, with the ok-package set
byte-identical after timing strip. `go test -list` inventory is
identical on both sides (6703 tests). Backend/version: go1.26.9
linux/amd64, in-memory; no backend touched. Input shape: n/a (no
runtime input). Terminal queue/row counts: none — no queue, lease,
Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`, and
`verify-package-docs.sh`. The change is safe because it cannot alter
runtime behavior: the compiler resolves the same types through their
new paths, and the compat deletion is compile-enforced total — any
missed caller would fail the build.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
