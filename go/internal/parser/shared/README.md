# Shared Parser Helpers

## Purpose

`internal/parser/shared` holds the small contracts language-owned parser
packages need without importing the parent `internal/parser` dispatcher. It
contains common payload bucket helpers, source reads, tree-sitter node helpers,
string utilities, integer coercion, and parser options shared by adapter
packages. Parser options include the `EmitDataflow` gate and the stable
repository identity used by value-flow FunctionIDs. The shared Go semantic-root
options also carry the empty-method-list convention used when an imported
package can reach exported methods through an escaped interface value and the
qualified roots for imported receiver method calls.

## Dependency boundary

```mermaid
flowchart LR
    Parent["internal/parser dispatcher"]
    Shared["internal/parser/shared"]
    Child["language-owned parser packages"]
    Payload["common payload buckets and options"]
    Collector["collector materialization"]

    Parent --> Child
    Shared --> Child
    Shared --> Payload
    Child --> Payload
    Payload --> Collector
```

Child parser packages may depend on shared helpers. Shared helpers must stay
language-neutral and must not depend on the parent dispatcher.

## Ownership boundary

This package owns dependency-safe helper contracts for child parser packages.
It does not own registry dispatch, language selection, content metadata
inference, parser runtime caching, or language-specific semantics.

## Exported surface

The godoc contract is in `doc.go` and `shared.go`. Current exports are
`Options`, `GoImportedInterfaceParamMethods`, `GoDirectMethodCallRoots`,
`GoPackageSemanticRoots`, `GoPackageSemanticRootOptions`, `BasePayload`, `ReadSource`,
`NormalizeLineEndings`, `PrimeSource`, `ClearSource`, `SetReadSourceHookForTest`,
`WalkNamed`, `NodeText`, `NodeLine`, `NodeEndLine`, `CloneNode`,
`AppendBucket`, `SortNamedBucket`, `SortNamedMaps`, `CollectBucketNames`,
`IntValue`, `LastPathSegment`, `DedupeNonEmptyStrings`, `BranchNodeSet`,
`NewBranchNodeSet`, and `CyclomaticComplexity`.

`NormalizeLineEndings` rewrites a bare carriage return -- a `\r` with no `\n`
after it -- to `\n`, and returns an LF or CRLF source untouched as the
caller's own slice. `ReadSource` applies it to every buffer it returns, on the
cached path as well as the disk path, so no language parser in this tree ever
sees a classic-Mac line ending (#6306). Without it tree-sitter reports row 0
for every node in such a file and each hand-rolled `\n` scanner treats the
whole file as one line, silently. The rewrite is length-preserving, which is
what keeps downstream byte offsets (the JSONC offset translator, SQL entity
spans, `IndexSource` snippets) valid against the file on disk, and it never
mutates its input, which is what keeps the git collector's content digest
stable. A parser that reads a SECOND file with its own `os.ReadFile` -- a
C/C++ header, a sibling module, a tsconfig -- does not go through `ReadSource`
and must call `NormalizeLineEndings` itself.

`PrimeSource`/`ClearSource` back the single-physical-read cache
`Engine.ParsePath` uses so the language parser and the engine's
content-metadata inference share one disk read; see the parent package
README's "Single physical read per `ParsePath` call" section for the full
contract. `SetReadSourceHookForTest` is test-only instrumentation for counting
physical `ReadSource` reads without changing its signature; production code
never calls it.

`CyclomaticComplexity` is the shared McCabe walker. Each tree-sitter language
adapter passes a `BranchNodeSet` (built with `NewBranchNodeSet`) that names the
node kinds and boolean operator tokens counted as decision points, so adding
complexity for a language is a data table, not new traversal code. The
`defaultCaseKinds` argument names branch kinds that double as a switch `default`
or bare wildcard `_` arm; the walker excludes those catch-all arms because the
implicit else is not a decision point under McCabe.

## Dependencies

This package imports the Go standard library and
`github.com/tree-sitter/go-tree-sitter` for node helper signatures. It must not
import the parent `internal/parser` package or any collector, query, storage,
projector, or reducer package.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path.

## Performance and observability evidence

- No-Regression Evidence: `Options.RepositoryID` is an immutable string copied
  through existing parser option structs; it adds no traversal, graph write,
  queue operation, database query, lock, goroutine, or worker coordination.
  Focused verification is `go test ./internal/parser ./internal/collector
  ./internal/storage/postgres -run
  'Test(SnapshotParserOptionsThreadsRepositoryID|GoInterprocFunctionIDsIncludeRepositoryID|JSInterprocFunctionIDsIncludeRepositoryID|PythonInterprocFunctionIDsIncludeRepositoryID|FunctionSummary|BootstrapDefinitionsIncludeFunctionSummaries)'
  -count=1`.
- No-Observability-Change: this package remains telemetry-free; parser timing,
  parse failures, and collector stage metrics stay on the existing collector
  runtime path, unchanged by the repository identity field.
- No-Regression Evidence (issue #3488): `CyclomaticComplexity` is a pure
  function of one already-parsed function subtree. It performs one bounded
  extra named+anonymous walk per function and adds no graph write, queue
  operation, Cypher, lock, goroutine, or worker coordination. Focused
  verification is `go test ./internal/parser/shared -run
  TestCyclomaticComplexity -count=1` plus
  `go test ./internal/parser -run TestCyclomaticComplexityPerLanguage -count=1`.
- No-Observability-Change (issue #3488): the walker writes only the existing
  `cyclomatic_complexity` function-entity field; it adds no metric, span, log,
  status field, env var, or graph query.
- Benchmark Evidence (issue #6306): `NormalizeLineEndings` adds one scan to
  every `ReadSource` return, so it sits on the per-file parse hot path.
  Measured with `go test ./internal/parser/shared -run '^$' -bench
  BenchmarkNormalizeLineEndings -benchtime 2000x -count=5` on a 64 KiB body
  (large for a parser input; the median repository source file is well under
  8 KiB), darwin/arm64 Apple M4 Pro, go1.26.6, 12 usable CPUs, worktree
  GOCACHE, best of five: LF
  666ns/op and 0 allocs (one `bytes.IndexByte` that finds nothing, ~98 GB/s);
  CRLF 7.74us/op and 0 allocs (hops CR to CR, allocates nothing because a CRLF
  pair is never rewritten); bare CR 27.0us/op and 1 alloc of 73728 B, the only
  case that copies. Compare against the parse it precedes, from `go test
  ./internal/parser -run '^$' -bench 'BenchmarkParsePathPHPRouteHeavy|
  BenchmarkParsePathGoIdentifierHeavy' -benchtime 30x -count=5` on the same
  machine: 10.0ms/op for the PHP route-heavy file and 249ms/op for the Go
  identifier-heavy one. `ReadSource` is called about twice per `ParsePath`, so
  the added cost is roughly 1.3us against a >=10ms parse (about 0.013%) for
  the LF files that are essentially all real input, and about 54us (about
  0.5%) in the bare-CR case that previously produced wrong data. No new graph
  write, queue operation, Cypher, lock, goroutine, or worker coordination.
- No-Observability-Change (issue #6306): this package remains telemetry-free.
  Normalization adds no metric, span, structured log, status field, env var,
  or runtime knob; it changes only the bytes a language parser is handed, and
  an LF or CRLF source is returned as the caller's own slice, so operator-
  visible parse timing and failure signals stay on the existing collector
  path.
- Benchmark/No-Observability-Change evidence for the `ReadSource` single-read
  cache (`PrimeSource`/`ClearSource`) is recorded in the parent package
  README's "Single physical read per `ParsePath` call" section; this package
  only adds the cache lookup and test-hook plumbing `ReadSource` and
  `SetReadSourceHookForTest` use.

## Gotchas / invariants

Child parser packages depend on this package to avoid import cycles. Keep it
small and language-neutral; a helper that only one adapter needs belongs in
that adapter package.

`BasePayload`, bucket sorting, and name collection are fact-input contracts.
Changing their shape or ordering changes downstream materialization behavior.

`GoImportedInterfaceParamMethods` uses an empty method list intentionally. It
means the concrete value crossed into another package through an interface
parameter without a known method set, so exported methods on that concrete type
may be valid runtime hooks. Same-repository package contracts should carry
explicit method names from package interface declarations.

`GoDirectMethodCallRoots` uses lower-case qualified import-path receiver keys.
The parent parser decides which package directory receives those roots; child
packages should only carry the typed option.

`Options.EmitDataflow` controls opt-in `dataflow_functions`, `taint_findings`,
`interproc_findings`, and durable `dataflow_summaries` buckets. The gate is off
by default so normal parser payloads remain byte-identical.

`Options.RepositoryID` must be generation-independent and must not contain local
checkout paths, hostnames, IPs, credentials, or commit SHAs. Value-flow-capable
adapters use it with stable language package identity when `EmitDataflow` is
enabled; durable `dataflow_summaries` must be omitted when either identity
component is absent.

`SortNamedMaps` sorts by `line_number` first and `name` second. That preserves
the parent parser ordering contract used before language packages were split.

Utility helpers such as `IntValue`, `LastPathSegment`, and
`DedupeNonEmptyStrings` are intentionally small. Keep language-specific parsing
rules out of this package so shared does not become a second parser package.

`WalkNamed` uses one tree-sitter cursor per traversal and visits only named
direct children recursively. That preserves source-order parser behavior while
avoiding per-node `NamedChildren` slice allocation in repo-scale Go pre-scans.

## Related docs

- `docs/public/languages/support-maturity.md`
