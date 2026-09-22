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
