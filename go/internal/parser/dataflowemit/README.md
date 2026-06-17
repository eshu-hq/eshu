# Value-Flow Payload Emission

## Purpose

`dataflowemit` renders language-neutral value-flow facts into the parser payload
buckets the reducer consumes. It is the shared rendering layer behind the opt-in
`Options.EmitDataflow` gate, so the Go, TypeScript/JavaScript, and Python parser
adapters all emit one identical bucket schema.

## Ownership boundary

This package owns only the row rendering and deterministic ordering of the three
value-flow buckets. It does NOT own the analysis (that is `internal/parser/cfg`,
`taint`, `valueflow`, `interproc`), the per-language function walking and
lowering (that is each adapter, e.g. `python` over `python/pydataflow`), or the
gate plumbing (`parser.Options`/`shared.Options`).

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `DataflowFunctionRow(lang, name, line, classContext, fn) map[string]any` — one
  `dataflow_functions` row (CFG blocks + def->use edges).
- `TaintFindingRow(lang, name, line, classContext, finding) map[string]any` — one
  `taint_findings` row.
- `InterprocFindingRow(lang, finding) map[string]any` — one `interproc_findings`
  row.
- `SortFunctionRows`, `SortFindingRows` — deterministic ordering for byte-stable
  buckets.

## Dependencies

- `internal/parser/cfg`, `internal/parser/taint`, `internal/parser/interproc`
  (the language-neutral fact types).

## Telemetry

None. Pure rendering functions; the reducer that consumes the buckets owns
telemetry.

## Gotchas / invariants

- **One schema across languages.** Rows differ only by the `lang` label and the
  facts; the keys are identical so the reducer parses every language uniformly.
- **Optional fields are omitted when empty** (`class_context`, `sink_label`,
  `source_label`, `neutralized`, `cloud`) to keep rows minimal and byte-stable.
- **Ordering lives here, not in the analysis.** `SortFunctionRows`/
  `SortFindingRows` must be applied by the caller after collecting rows so the
  bucket is deterministic.
- The Go adapter (`internal/parser/golang`) still carries its own copy of these
  renderers; migrating it onto this package is a tracked follow-up.

## Related docs

- Epic #2705, issue #2826. Callers: `internal/parser/python` (`cfg_emit.go`),
  and `internal/parser/golang` (to be migrated).
