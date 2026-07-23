# AGENTS.md - boundedset

## Ownership

This package owns only the generic `DedupeSortCap[T]` dedupe/sort/cap
mechanics. It must not gain SBOM-specific, reducer-specific, or
query-specific knowledge, and must not import any other `go/internal`
package — that would turn a leaf utility into a coupling point between the
packages that are supposed to share only this algorithm.

## Rules

- Keep `DedupeSortCap` generic (`[T any]`) and free of I/O, logging, or
  telemetry — callers own those concerns.
- Do not weaken the ordering/dedupe contract documented on `DedupeSortCap`
  (total order via `less`, adjacent-only `dedupeKey` checks, `maxRows <= 0`
  disables capping) without updating every call site and their tests.
- Any change to the sort/dedupe/cap mechanics must keep
  `TestDedupeSortCapIsOrderInvariantAcrossShuffles` green — it is the
  regression guard for the entire reason this package exists (a caller-level
  test pinned to one fixed input order would NOT catch a broken tiebreak).
- Add a table-driven or shuffled-property test before changing this file;
  every reducer/query caller depends on this package never silently
  regressing back to two independently-drifting implementations.
