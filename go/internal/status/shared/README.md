# Status Shared Primitives

## Purpose

`internal/status/shared` holds the small value types and rendering helpers that
every status section needs, so the family leaves (`collector`, `semantic`,
`tfstate`, `cloud`, `queue`, `generation`, `changedsince`) can use them without
importing the parent `internal/status` package.

## Ownership boundary

This package owns the name/count primitive, its wire shape, and the two scalar
normalizers status output depends on. It owns no section of the report and no
snapshot type. It deliberately depends on nothing else under `internal/status`.

The root package aggregates every leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves and
root may import `shared`; `shared` may import neither. Adding an import of a
sibling leaf here creates an import cycle and will not compile.

## Exported surface

- `NamedCount` — one status bucket and its count
- `CountMap` — fold named buckets into a total per name, dropping blank names
- `FormatTotals` — render a count map as operator text in lifecycle order
- `NamedCountJSON` / `NamedCountsJSON` — the bucket wire shape and its projection
- `NullableRFC3339Value` — UTC RFC3339, or empty string when unset
- `NonNegativeDuration` — clamp a negative status age to zero

See `doc.go` for the full godoc contract.

## Dependencies

Standard library only (`fmt`, `sort`, `strings`, `time`). This is a deliberate
constraint, not a coincidence — see the ownership boundary above.

## Telemetry

None. This package performs no I/O and emits no metrics, spans, or logs; it is
pure value transformation called from the sections that do.

## Gotchas / invariants

- `NamedCountJSON`'s field tags and `NullableRFC3339Value`'s formatting are
  operator-facing API. They are locked by the byte-for-byte wire goldens in
  `internal/status/testdata/`; changing them is an API change, not a refactor.
- `NamedCountsJSON` returns a non-nil empty slice on empty input so a section
  renders `[]` rather than `null`.
- `NamedCountsJSON` converts `NamedCount` to `NamedCountJSON` by direct struct
  conversion, so the two must keep identical field order and types.
- `CountMap` accumulates duplicate names rather than overwriting them.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package

## Evidence

No-Regression Evidence (#6949 batch 8, shared family): this change moves
every `NamedCount` importer off the root `internal/status` alias onto this
leaf and deletes the alias from `go/internal/status/status.go`. Scope was
re-measured by symbol, not qualifier: an initial `status.NamedCount` grep
undercounted at 11 files, and the build exposed the same-package bare
`NamedCount` uses (`coordinator.go`, `report.go`, `registry.go`, 3
`render_wire_fixture` tests) plus a second qualifier spelling
`statuspkg.NamedCount` in 14 more files, for 31 files and 104 replacements
in total. Two files shed a now-unused `statuspkg` import. No type shape,
wire field, SQL text, or executable statement changes: `status.NamedCount`
was a type alias (`=`) for `shared.NamedCount`, so every hunk requalifies
an identifier or edits an import path only. No `compat_*.go` remains under
`go/internal/status/`. `RepositoryFreshnessSelection` stays: it aliases the
external `selection.Summary`, not a nested status leaf.
Measurement: packages ok on both sides with identical sets (ledger:6949-shared-batch8-after).
Sibling before-row `6949-shared-batch8-before`; baseline `41e247aeaf`
vs measurement commit `0e8a895dc9` (the two ledger rows and this Evidence
section are the only later changes), with the ok-package set byte-identical
after timing strip (158/158).
`go test -list` inventory is identical on both sides (11212 tests).
Backend/version: go1.26.2 linux/amd64, in-memory; no backend touched. Input
shape: n/a (no runtime input). Terminal queue/row counts: none — no queue,
lease, Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`,
`verify-package-docs.sh`, and `verify-performance-evidence.sh`. The change
is safe because it cannot alter runtime behavior: the compiler resolves the
same type through its new path, and the alias deletion is
compile-enforced total — any missed caller would fail the build.
No-Observability-Change: no metric, span, log line, or status output changes;
telemetry evidence is the unchanged test outcomes above.
