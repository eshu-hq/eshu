# AGENTS.md - internal/parser/scip guidance

## Read first

1. README.md - package boundary, extraction strategy, gotchas
2. doc.go - godoc contract for the SCIP package
3. parser.go - `IndexParser.Parse`, symbol-table build, definition/reference
   extraction
4. indexer.go - `Indexer.Run`/`IsAvailable`, language detection and grouping
5. bucket.go - the local `appendBucket` wrapper around `shared.AppendBucket`

## Invariants this package enforces

- Dependency direction stays one way: the parent `internal/parser` package and
  `internal/collector/repo/git` may import this package, but this package must
  not import `internal/parser`. Its only `internal/parser/*` dependency is
  `internal/parser/shared`, mirrored locally in `bucket.go`.
- `Parse` returns the same payload shape and field names
  (`function_calls_scip`, `scip_symbol`, etc.) the parent SCIP adapter emitted
  before the package split (issue #6772). Do not rename payload map keys here
  without also updating `internal/collector/repo/git` and the reducer's SCIP
  code-call extractor, plus `sdk/go/factschema`'s
  `DecodeParsedFileDataSCIPFunctionCalls` typed accessor.
- Exported identifiers do not repeat the package name (`ParseResult`, not
  `SCIPParseResult`; `Indexer`, not `SCIPIndexer`) per
  `docs/internal/naming.md` rule 4. Keep new exports to that convention; "SCIP"
  stays in doc comments and error strings (it names the protocol), never in a
  new identifier.
- `IndexParser` and `Indexer` are structural interface implementations for
  `internal/collector/repo/git`'s unexported `scipResultParser`/
  `scipProjectIndexer` interfaces. There is no compile-time assertion tying
  them together, so a signature change here that silently breaks the
  collector's interface satisfaction will only surface as a build failure in
  `internal/collector/repo/git`, not in this package's own tests. Build both
  packages after changing `Parse`, `Run`, or `IsAvailable`.

## Common changes and how to scope them

- **Add a new SCIP-capable language** (e.g. a new `scip-<lang>` indexer):
  1. Add an `extensionConfigs` entry (indexer.go) keyed by lowercased file
     extension, giving `Language`, `Binary` (the `scip-*` CLI name), and
     `InstallHint` (a one-line install command shown in the resolve-binary
     error).
  2. Add the language to `languagePriority` (indexer.go) in the position that
     reflects how it should be ranked when a repo mixes SCIP-capable
     languages — this order is covered by an ADR-guard note in the parent
     `internal/parser/AGENTS.md`, so changing it changes fact output for every
     repo with multiple SCIP-capable languages.
  3. Add a `case` to `buildCommand` (indexer.go) for the CLI invocation shape
     that language's `scip-*` binary expects (some take `index .`, some take a
     bare `--output`, cpp/c use `--index-output-path=`).
  4. Add a test following the existing names in `indexer_test.go`: a
     `TestDetectSCIPProjectLanguage*` case for detection and a
     `TestBuildSCIPCommand*` case for the new binary's invocation shape. Test
     function names keep the `SCIP` prefix even though the identifiers they
     exercise dropped it (#6772) -- match the siblings, do not de-stutter.
  5. Update `docs/public/languages/support-maturity.md` if the new language
     changes its documented support tier.
- **Change symbol/name extraction** (`parser.go`): `nameFromSymbol` and
  `parseSignature` are pinned by `regex_hoist_test.go`
  (`TestScipNameFromSymbolMatchesSeparatorSplit`,
  `TestScipParseSignatureMatchesArgsAndReturnType`); update those cases
  alongside any change to symbol-name or signature-argument parsing.
- **Change the call-edge shape** (`appendReference`, `function_calls_scip`):
  keep `parsed_file_data_contract_test.go`
  (`TestSCIPFunctionCallsRoundTripThroughTypedContract`) green — it proves the
  raw payload round-trips through `sdk/go/factschema`'s typed accessor, which
  is a separate module's contract this package's output feeds.

## Failure modes and how to debug

- Missing functions/classes/variables usually mean a `SymbolInformation_*`
  kind in `definitionKind` was not matched, or the occurrence's symbol started
  with `local ` and was filtered as a local binding (SCIP marks block-local
  bindings this way; they are intentionally excluded from both the symbol
  table and payload buckets).
- Missing `function_calls_scip` edges usually mean
  `findEnclosingDefinition` found no definition at or before the reference
  line (a reference before any definition, e.g. inside a package-level
  expression) — this is filtered on purpose, not a bug, unless the SCIP index
  itself lacks the expected definition occurrence.
- `Indexer.Run` failing with "unsupported SCIP language" means the language
  string reaching `buildCommand` does not match one of its `case` arms exactly
  (case-insensitive, trimmed) — confirm the caller passes the same
  `extensionConfigs[...].Language` value, not a raw file extension.
- A `go build` failure in `internal/collector/repo/git` after a signature
  change here usually means `scipProjectIndexer`/`scipResultParser` structural
  satisfaction broke; see the invariants section above.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse `appendBucket` or any other
  helper — copy it locally instead (see `bucket.go`).
- Adding a new SCIP language without a `languagePriority` entry — an
  extension-configured language absent from priority ranking is detected as
  present in `filesByLanguage` but never wins `DetectProjectLanguage`, and is
  skipped entirely by `DetectProjectLanguageGroups`.
- Reintroducing a per-call `regexp.MustCompile` for symbol-name or
  signature-argument parsing (see the hoisted-regex gotcha in README.md).

## What NOT to change without an ADR

- Do not change `ParseResult`/payload bucket keys, `function_calls_scip` edge
  field names, or `SymbolTable` entry field names without updating
  `internal/collector/repo/git`, the reducer's SCIP code-call extractor, and
  `sdk/go/factschema`'s `DecodeParsedFileDataSCIPFunctionCalls` in the same
  branch.
