# boundedset

## Purpose

`boundedset` holds one generic dedupe/sort/cap algorithm
(`DedupeSortCap[T]`) for bounding a declared-evidence row set: sort a copy of
the items, drop an item its neighbor's `dedupeKey` calls a duplicate, cap the
result at `maxRows`, and report the full distinct count computed before the
cap.

## Why this exists

Several SBOM attestation attachment evidence kinds
(`sbom.component`, `sbom.dependency_relationship`, `sbom.external_reference`)
need the SAME bounding discipline applied twice: once at reducer write time
(`go/internal/reducer/sbom_attestation_attachment_evidence_bounds.go`, capping
what gets persisted) and once defensively at query read time
(`go/internal/query`, re-capping whatever was actually persisted — including
a legacy, pre-cap fact written before the write-time cap existed). Two
independent implementations of "sort, dedupe, cap, count" is exactly the kind
of pair that drifts silently when one side's tiebreak or cap changes and the
other does not. This package is the one implementation both the reducer and
query call sites use, each supplying their own type's ordering and identity
comparators.

## Ownership boundary

Pure, generic, no I/O. It has no SBOM-specific, reducer-specific, or
query-specific knowledge — it never imports another `go/internal` package.
Callers own their own type, cap constant, and comparators; this package only
owns the mechanics.

## Contract

- `less` MUST impose a total order (every tie fully broken, typically ending
  in a stable identity tiebreak such as `fact_id`) for the result to be
  independent of the input slice's original order — that invariant is
  covered by `TestDedupeSortCapIsOrderInvariantAcrossShuffles`.
- `dedupeKey` is only ever checked between sort-adjacent items, so it must
  agree with `less`'s ordering (anything `dedupeKey` calls a duplicate must
  sort next to its match).
- `maxRows <= 0` disables capping.

## Verification

```bash
cd go && go test ./internal/boundedset -count=1
```
