# SCIP Parser

## Purpose

`internal/parser/scip` owns decoding of `index.scip` protobuf files produced by
external `scip-*` indexers (scip-python, scip-go, scip-typescript,
scip-clang, scip-java, scip-rust) into Eshu's parser payload shape, and owns
running those external indexer binaries. It exists so SCIP-path extraction can
evolve as its own language-owned package without depending on the parent
parser dispatcher, matching the split already done for `sql`, `java`, and
`python`.

## Ownership boundary

This package is responsible for: (1) parsing one already-produced `index.scip`
file into file payload buckets and a symbol table (`IndexParser.Parse`), (2)
running the correct external `scip-*` binary for a language and returning the
`index.scip` path it wrote (`Indexer.Run`/`Indexer.IsAvailable`), and (3)
picking which SCIP-capable language(s) a candidate file set belongs to
(`DetectProjectLanguage`, `DetectProjectLanguageGroups`). It does not own
deciding *when* the SCIP path runs instead of the native tree-sitter parsers,
subtree/project-root selection, worker fan-out, or telemetry recording for the
snapshot attempt — those stay in
`internal/collector/repo/git/snapshot_scip*.go`, the package's only production
caller.

## Exported surface

The godoc contract is in `doc.go`. Current exports are `IndexParser`,
`ParseResult`, `Indexer`, `LanguageFileGroup`, `DetectProjectLanguage`, and
`DetectProjectLanguageGroups`.

## Dependencies

This package imports the Go standard library, the vendored
`github.com/scip-code/scip/bindings/go/scip` protobuf bindings
(`google.golang.org/protobuf/proto`), and `internal/parser/shared` for the
payload-bucket-append helper it mirrors locally in `bucket.go`. It must not
import the parent `internal/parser` package, collector packages, graph
storage, projector, query, or reducer code — the parent package imports this
one, never the reverse.

## Extraction strategy

`IndexParser.Parse` unmarshals the protobuf `Index`, builds a symbol table from
every non-`local` definition occurrence (enriched with display name,
documentation, and kind from `SymbolInformation` rows), then walks each
document's occurrences a second time: definitions become `functions`,
`classes`, or `variables` rows keyed by SCIP `SymbolInformation` kind; other
occurrences become `function_calls_scip` edges by resolving the occurrence's
enclosing definition (the innermost definition whose line is at or before the
reference line) as the caller. There is no tree-sitter grammar and no
statement-level AST here — a SCIP index already carries resolved symbols, so
this package projects protobuf into payload shape rather than parsing source.

`Indexer.Run` resolves the `scip-*` binary for a language via `LookPath`
(`exec.LookPath` unless overridden for tests), builds the language-specific
invocation (`buildCommand`), and runs it with a bounded timeout (default 5
minutes) via `RunCommand` (`exec.CommandContext` unless overridden). It fails
closed if the binary does not produce `index.scip` in the given output
directory.

`DetectProjectLanguage`/`DetectProjectLanguageGroups` group candidate paths by
SCIP-capable language (`extensionConfigs`, keyed by lowercased file extension)
restricted to an allowed-language set, then rank by `languagePriority` so a
mixed-language repository picks a deterministic dominant language rather than
depending on map iteration order.

## Telemetry

This package emits no metrics, spans, or logs itself. The collector caller
(`internal/collector/repo/git`) records SCIP snapshot attempt/result counters,
process-wait duration, and process-slot-acquired debug logs around calls into
this package; see that package's own docs for the emitted instrument and log
names.

## Gotchas / invariants

- `ParseResult.Files` is keyed by resolved absolute path
  (`filepath.Abs(filepath.Join(projectPath, relativePath))`), not the SCIP
  document's repo-relative path — callers that need the relative path use
  `shape.File.Path` set by the collector, not a key in this map.
- Function/method definitions always emit `cyclomatic_complexity: 0`. SCIP
  carries symbols and signatures but no statement-level AST, so a real McCabe
  count cannot be computed here; `0` is treated as "unknown" downstream and
  excluded from complexity rankings rather than surfacing a fabricated `1`
  (issue #3488).
- `nameFromSymbol` and `parseSignature` each use a hoisted package-level
  `*regexp.Regexp` (`trailingCallRe`/`separatorRe`/`signatureArgsRe`) instead
  of compiling per call; keep new signature/name parsing on this pattern
  rather than reintroducing a per-call `regexp.MustCompile` (issue #4874).
- `Indexer` and `IndexParser` are stateless value receivers with overridable
  function fields, not interfaces, so the collector's `scipProjectIndexer`/
  `scipResultParser` interfaces in `internal/collector/repo/git` are structural
  matches, not declared implementations. Changing either exported method's
  signature breaks that structural match without a compiler error at the type
  declaration site — grep collector callers before changing `IsAvailable`,
  `Run`, or `Parse`.
- `appendBucket` (`bucket.go`) is a local copy of `shared.AppendBucket`,
  matching the same per-package pattern used by `sql`, `java`, and `python`.
  Keep it a thin wrapper; do not import the parent `internal/parser` package
  to reuse its version, which would risk an import cycle.

## Related docs

- `docs/public/languages/support-maturity.md`
