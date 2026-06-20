# Value-Flow Payload Emission

## Purpose

`dataflowemit` renders language-neutral value-flow facts into the parser payload
buckets the reducer consumes. It is the shared rendering layer behind the opt-in
`Options.EmitDataflow` gate, so each value-flow-capable language emits the same
schema for the buckets it supports.

## Ownership boundary

This package owns only the row rendering and deterministic ordering of the
value-flow buckets. It does NOT own the analysis (that is `internal/parser/cfg`,
`taint`, `valueflow`, `interproc`, and `summary`), the per-language function
walking and lowering (that is each adapter, e.g. `python` over
`python/pydataflow`), or the gate plumbing (`parser.Options`/`shared.Options`).

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `DataflowFunctionRow(lang, name, line, classContext, fn) map[string]any` — one
  `dataflow_functions` row (CFG blocks + def->use edges).
- `CatalogVersionRow(lang, catalog, version) map[string]any` — one
  `dataflow_catalog_versions` row used by the collector freshness hint.
- `TaintFindingRow(lang, name, line, classContext, finding) map[string]any` — one
  `taint_findings` row.
- `InterprocFindingRow(lang, finding) map[string]any` — one `interproc_findings`
  row.
- `DataflowSummaryRow(lang, id, effects) map[string]any` — one
  `dataflow_summaries` row for a durable `summary.FunctionID`.
- `SortFunctionRows`, `SortFindingRows`, `SortSummaryRows` — deterministic
  ordering for byte-stable buckets.

## Dependencies

- `internal/parser/cfg`, `internal/parser/taint`, `internal/parser/interproc`,
  `internal/parser/summary` (the language-neutral fact types).

## Telemetry

None. Pure rendering functions; the reducer that consumes the buckets owns
telemetry.

## Gotchas / invariants

- **One schema per bucket across languages.** Rows differ only by the `lang`
  label and the facts; the keys are identical so the reducer parses every
  emitting language uniformly.
- **Catalog versions are freshness metadata.** `dataflow_catalog_versions` rows
  do not become facts; the collector folds them into the snapshot hint so
  catalog-only matching changes re-run value-flow analysis on unchanged files.
- **Optional fields are omitted when empty** (`class_context`, `sink_label`,
  `source_label`, `guard_reason`, `neutralized`, `cloud`, `overflow`) to keep
  rows minimal and byte-stable.
- **Overflow shape is shared.** When present on `dataflow_functions`, `overflow`
  carries counted `blocks`, `stmts`, `def_use_edges`, `control_dependencies`,
  and `access_paths` values from `cfg.Function.Overflow`.
- **Ordering lives here, not in the analysis.** `SortFunctionRows`,
  `SortFindingRows`, and `SortSummaryRows` must be applied by the caller after
  collecting rows so the bucket is deterministic.

## Related docs

- Epic #2705, issue #2826. Callers: `internal/parser/golang`,
  `internal/parser/python`, and `internal/parser/javascript` (all `cfg_emit.go`)
  render the buckets through this package.
