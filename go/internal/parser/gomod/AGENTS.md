# AGENTS.md - internal/parser/gomod

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for parent parser callers.
3. `parser.go` - `Parse` entrypoint, go.mod dispatch, row construction.
4. `gosum.go` - go.sum line scanner and checksum row construction.
5. Parent wrapper in `../gomod_language.go`.
6. Coverage matrix in `../json/dependency_coverage.go` and the parent-level
   engine test `../dependency_coverage_engine_test.go` (this package owns
   the `go.mod` covered entry and the `go.sum` gap entry's "checksum-only"
   ambiguity rule).

## Invariants this package enforces

- Do not invent installed-version evidence. `go.mod` require entries carry
  the source-truth version verbatim; `replace` targets surface on a
  `resolved_module_path`/`resolved_version` pair plus a standalone replace
  row so the original intent is auditable.
- `replace` and `exclude` rows MUST use `config_kind=dependency_replace`
  and `config_kind=dependency_exclude` respectively so the consumption
  reducer never admits them as repository consumption.
- `go.sum` rows MUST be emitted as `config_kind=dependency_checksum` with
  `ambiguous=true`. The reducer must treat these as missing evidence
  until paired with a `go.mod` require, because `go.sum` records every
  version any tool has verified, not the currently selected version.
- A malformed `go.mod` MUST return a payload with zero dependency rows
  and a `gomod_state.parse_error` envelope; the parser never panics and
  never falls back to inventing rows from partial parses.
- Module identity is the full module path (for example
  `golang.org/x/text`); the parser does not normalize, lowercase, or
  rewrite identity. Reducers handle ecosystem-aware normalization.

## Common changes

- New go.mod directive (for example a future first-class `tool` block):
  add a per-directive row builder next to the existing helpers in
  `parser.go`, pick a `config_kind` that the consumption reducer will not
  admit unless the new directive is meant to be admitted, and add a
  focused test next to the existing per-directive tests.
- Replacement-resolution changes: keep `matchReplace` deterministic
  (version-specific match wins over a path-wide match) and update the
  replace-directive resolution tests in `parser_test.go` in the same
  commit so both the source-truth require row and the local-path
  replace fallback stay protected.
- go.sum row shape changes: keep the verbatim `checksum` field and the
  `module`/`gomod` `checksum_kind` value; reducers and operators read both.

## Failure modes

- Missing require rows usually means `modfile.Parse` rejected an invalid
  module path or version. Check `gomod_state.parse_error`.
- Missing replacement metadata on a require row usually means the
  `replace` directive used a version filter that did not match the
  require's version (`replace foo v1.2.3 => ...` only matches that exact
  version).
- A go.sum row showing up in consumption decisions means the row leaked
  a `config_kind=dependency` value somewhere — that is a bug; checksum
  rows must always be `dependency_checksum`.

## What not to change without an ADR

- Do not make this package read repository state beyond the single file
  passed to `Parse` (no scanning sibling `go.mod`, no module proxy
  lookups, no `go list` execution).
- Do not add graph, collector, storage, query, projector, or reducer
  dependencies.
- Do not change the contract that `go.sum` is parsed but emits no
  `config_kind=dependency` rows. Promoting `go.sum` to "covered" would
  require proving exact selected versions, which the file alone cannot
  do.
