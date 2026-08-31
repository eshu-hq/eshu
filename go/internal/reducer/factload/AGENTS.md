# Reducer fact-load package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/factload/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- Remain a leaf below `internal/reducer`. Never import the parent reducer
  package or a family subpackage. Budget: `internal/facts`,
  `internal/reducer/payloadcore`, and the standard library.
- `FactKindLoader` and `FactPayloadValueLoader` must stay OPTIONAL. A store that
  implements neither falls back to the full `FactLoader` contract and receives
  the whole scope generation, which the calling handler then filters. Making
  either mandatory breaks every store that cannot filter. Dropping the
  extensions is NOT free: this package applies no in-process filter, so every
  domain handler would start receiving the entire generation.
- `ClassifyFactLoadError` must keep marking only transport and availability
  failures retryable. Widening it turns a real error into an infinite retry;
  narrowing it dead-letters a scope generation on a transient outage.
- Loading only. Decoding a loaded payload belongs to `factdecode`, and the
  handler that interprets the facts belongs to its family.

## Common changes

Adding a fact kind: add the constant here and the root alias in
`scoped_fact_loader_compat.go` together, so root call sites keep compiling while
their families are still in the root.

## Failure modes

- Adding a method to `FactLoader` rather than a new optional extension interface
  silently excludes every store that does not implement it.
- Filtering in process where the store could have filtered turns a bounded query
  into a full scope-generation read. Push down when the extension is available.
