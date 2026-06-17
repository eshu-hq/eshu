# AGENTS.md - internal/parser/dataflowemit guidance

## Read first

1. `README.md` - package boundary and the bucket schema
2. `doc.go` - godoc contract: the three buckets and the lang label
3. `emit.go` - the row renderers and deterministic sorting
4. The Go original this generalizes: `../golang/cfg_emit.go`,
   `../golang/cfg_interproc.go`
5. A caller: `../python/cfg_emit.go`

## Invariants this package enforces

- One row schema across all languages; rows differ only by the `lang` label and
  the facts. Changing a key here is a wire-contract change to every language's
  payload and to the reducer that consumes it.
- Optional fields (`class_context`, `sink_label`, `source_label`, `neutralized`,
  `cloud`) are omitted when empty. Do not emit empty/zero values for them.
- Output is deterministic only after `SortFunctionRows`/`SortFindingRows`; the
  caller must apply them.

## Common changes and how to scope them

- Add a row field: update the renderer here AND every adapter's tests plus the
  reducer that reads the bucket. Keep it optional-when-empty unless it is
  always present.
- Add a language: the new adapter walks and lowers its own functions, then calls
  these renderers with its `lang` label. Do not add language-specific logic here.

## Do not change without review

- The bucket key names or the schema (wire contract shared with the reducer and
  every language adapter). Keep changes in lockstep with `golang/cfg_emit.go`
  until that adapter is migrated onto this package.
