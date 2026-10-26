# AGENTS.md — internal/eol guidance

## Read First

1. `README.md` — purpose and the status table.
2. `resolve.go` — `Resolve` is the authority on status boundaries and cycle
   matching; `snapshot.go` — normalization and the embed seam.
3. `docs/internal/evidence/8820-eol-snapshot-research.md` — the source,
   slug mapping, and licence the snapshot vendors.
4. `go/internal/collector/repo/runtime/declaration/AGENTS.md` — the sibling
   producer package; this package must not grow imports toward it.

## Invariants

- Keep this package dependency-free: standard library only, no collector,
  reducer, or query imports in production code. The air-gap contract rests
  on the join doing no I/O at all.
- Missing data is never read as supported. A new slug, a new product field,
  or a version too vague to pick a cycle resolves `unknown`; widening that
  set toward `supported` is a contract change, not a bug fix.
- The runtime-to-slug map is closed by evidence, not by enumeration: `java`
  has no plain slug and `swift` has none, so both stay `unknown` until the
  research note says otherwise.
- `snapshot_date` is a UTC day (`YYYY-MM-DD`); the refresh script writes it
  on every fetch. Never hand-edit a vendored product file: re-run the
  refresh script so the date matches the data.
- Dates are inclusive last days (README table). Changing a boundary is a
  contract change and needs a regression row on each side of it.
- Every new `.go` file needs the MIT SPDX header (`scripts/add-license-header.sh`).

## Common Changes

- A new mapped runtime: add the slug to `runtimeSlug`, the product file to
  the snapshot, the slug to the refresh script and the embedded-load test,
  and rows for each status the new data can produce.
- A refresh-window change: update `RefreshWindowDays` and both staleness
  rows in `eol_test.go`; the window is a freshness signal, not a correctness
  gate, so no query behavior may hinge on it.
- A per-product schema change (a new `eol` or `support` shape): extend
  `dayOrEmpty` and add a normalization row to
  `TestParseSnapshotNormalizesSchemaDrift`; the join must still read the
  unrecognized shape as `unknown`, never as supported.
