# AGENTS.md — internal/parser guidance for LLM assistants

## Read first

1. `go/internal/parser/README.md` — pipeline position, registered languages,
   SCIP path, exported surface, and invariants
2. `go/internal/parser/registry.go` — `Registry`, `Definition`,
   `NewRegistry`, `DefaultRegistry`, `defaultDefinitions`, `LookupByPath`
3. `go/internal/parser/engine.go` — `Engine`, `DefaultEngine`, `ParsePath`,
   `PreScanRepositoryPathsWithWorkers`, and the `parseDefinition` dispatch
4. `go/internal/parser/runtime.go` — `Runtime`; tree-sitter grammar caching
5. `go/internal/parser/java/README.md` — Java-owned helper package boundary
   for metadata evidence that does not need parent parser internals
6. `go/internal/parser/javascript/README.md` — JavaScript-owned helper package
   boundary for tsconfig evidence that does not need parent parser internals
7. `go/internal/parser/python/README.md` — Python-owned helper package boundary
   for notebook source extraction that does not need parent parser internals
8. `go/internal/parser/golang/README.md` — Go-owned helper package boundary for
   embedded SQL evidence that does not need parent parser internals
9. `go/internal/parser/shared/README.md` — dependency-safe helper contracts for
   child parser packages
10. `go/internal/parser/groovy/README.md` — Groovy-owned helper package boundary
   for Jenkins delivery metadata that does not need parent parser internals
11. `go/internal/parser/dockerfile/README.md` — Dockerfile-owned helper package
    boundary for runtime metadata that does not need parent parser internals
12. `go/internal/parser/cloudformation/README.md` — shared CloudFormation/SAM
    parser package used by JSON and YAML adapters
13. Language-owned adapter READMEs for extracted parsers before touching their
    parent wrappers: `c`, `cpp`, `rust`, `csharp`, `scala`, `elixir`, `swift`,
    `dart`, `ruby`, `perl`, `haskell`, `sql`, and `hcl`
14. `go/internal/parser/scip_support.go` — `SCIPIndexer`,
   `DetectSCIPProjectLanguage`, SCIP binary map
15. `go/internal/parser/doc.go` — the package contract, especially the
   determinism invariant
16. `go/internal/telemetry/instruments.go` — `telemetry.FileParseDuration` before
   adding parse-time metrics

## Invariants this package enforces

- **Determinism** — `doc.go` states parsers must be deterministic given the
  same source bytes. Retry and repair runs must converge on the same output.
  Do not introduce non-deterministic behavior (random map iteration, timestamps,
  process-local state) into any language adapter.

- **Fact truth preservation** — when a language adapter starts emitting a new
  entity key, relationship key, or metadata field, the corresponding `internal/facts`
  contracts, test fixtures, and `internal/content/shape` must be updated in the
  same branch. Emitting keys that `shape.Materialize` does not consume silently
  discards data.

- **Registry immutability** — `Registry` is built once via `NewRegistry` and
  never mutated. `LookupByPath`, `LookupByExtension`, and `LookupByParserKey`
  return cloned `Definition` values. Do not add mutable state to `Registry`.

- **No duplicate keys or extensions** — `NewRegistry` rejects duplicate
  `ParserKey`, extension, and exact filename entries with an error.
  `DefaultRegistry` panics on construction failure because a duplicate in
  `defaultDefinitions` is a programming error that must surface immediately.

- **Shared Runtime** — `NewRuntime()` should be called once and shared across
  all `Engine` instances and all parse calls. `Runtime.Language(name)` is
  mutex-protected for concurrent use. Do not allocate a new `Runtime` per file
  or per goroutine.

- **Absolute paths in Engine.ParsePath** — `ParsePath` calls `filepath.Abs`
  on both `repoRoot` and `path`. Callers may pass relative paths but the
  payload's `repo_path` field will contain the absolute form.

## Permanent parser exceptions (no tree-sitter migration)

Several registered parsers intentionally do **not** use tree-sitter and never
will. The epic-wide "no regex" / "migrate to AST" work targets *programming
language* adapters whose grammar tree-sitter models. The parsers below read
declarative data, configuration, dependency manifests, lockfiles, possibly
invalid templated text, or a precomputed index. A general-purpose code grammar
is the wrong tool for them: a dedicated structured decoder (or, for templated
and lineage text, a bounded scanner) is the canonical, more accurate choice.

Do not "migrate" these to tree-sitter or flag their non-tree-sitter technique as
debt. When the rule says no regex, it means no regex *in the language-grammar
adapters* — it does not mean replacing a structured-format decoder with a code
grammar.

| Parser / format | Technique used instead | Source | Why not tree-sitter |
| --- | --- | --- | --- |
| YAML | `gopkg.in/yaml.v3` document decode | `yaml/language.go` | YAML is a declarative data format; the canonical Go YAML library yields the exact document tree. A code grammar cannot model YAML's anchors, merges, or block scalars. |
| JSON | `encoding/json` (+ ordered-object decode) | `json/language.go`, `json/ordered_object.go` | JSON is declarative data; the standard library decoder is the canonical, lossless parser. |
| HCL / Terraform / Terragrunt | `github.com/hashicorp/hcl/v2` (`hclparse`, `hclsyntax`) | `hcl/parser.go`, `hcl/values.go`, `hcl/include_chain.go` | HCL has an official AST library from HashiCorp. Its native parser is the authoritative grammar for `.tf`/`.tfvars`/Terragrunt; reimplementing it via tree-sitter would diverge from upstream semantics. |
| Go modules (`gomod`) | `golang.org/x/mod/modfile` | `gomod/parser.go` | `go.mod`/`go.sum` have a canonical Go module-file parser maintained alongside the toolchain. It is a manifest, not source code. |
| Node lockfiles (`node_lockfile`) | custom yarn.lock scanner; pnpm-lock via `gopkg.in/yaml.v3` | `nodelockfile/parser.go`, `nodelockfile/pnpm.go`, `nodelockfile/yarn_*.go` | Lockfiles are generated manifests. yarn.lock is a bespoke format; pnpm-lock is YAML. Neither is a programming language tree-sitter can parse. |
| Python deps (`python_requirements`, `python_toml`) | `bufio` line scanner / minimal TOML scanner | `pythondep/requirements.go`, `pythondep/toml.go`, `pythondep/pyproject.go` | pip requirements and `pyproject.toml`/`Pipfile`/`poetry.lock` are manifests, not Python source. A bounded scanner reads exactly the dependency-table shapes these manifests use. |
| Maven | `encoding/xml` | `maven/parser.go` | `pom.xml` is XML; the standard XML decoder is canonical. |
| NuGet project | `encoding/xml` | `nuget_project_language.go` | `.csproj` is an MSBuild XML manifest, not C# source. |
| Gradle dependency DSL | bounded regex/string scanner over Groovy/Kotlin DSL (no execution) | `gradle/scanner.go`, `gradle/parser.go` | The parser extracts proven dependency coordinates from build-script text without executing Gradle or resolving source sets; unresolved interpolations surface as partial/unresolved evidence rather than fabricated versions. A code grammar would not evaluate the DSL anyway. |
| CloudFormation / SAM | bounded structural evaluation over the already-decoded YAML/JSON document | `cloudformation/parser.go` (invoked from `yaml/language.go`, `json/language.go`) | CloudFormation is declarative data; it is decoded by the YAML/JSON libraries above, then evaluated for bounded resource/parameter/output/condition buckets. |
| dbt SQL (`dbtsql`) | bounded regex SQL-lineage scanner over compiled dbt SQL | `dbtsql/lineage.go`, `dbtsql/expressions.go` | Extracts bounded column lineage (select projections, CTEs, relation aliases) from compiled model SQL; unsupported shapes go on the unresolved path. This is lineage extraction, not source-grammar parsing. |
| `templated_detection.go` | regex content classification | `templated_detection.go` | Classifies possibly-invalid-grammar templated text (Go template, Jinja, GitHub Actions, Terraform interpolation/directive). The input is frequently not valid in any single grammar, so tree-sitter cannot parse it; this is detection, not extraction. |
| Raw text (`raw_text`) | filename/extension classification, no grammar | `raw_text_engine.go` | The fallback adapter for `.cnf`/`.cfg`/`.conf`/`.j2`/`.tpl` and similar. It carries content-metadata detection only and has no grammar to migrate. |
| SCIP (`scip`) | `google.golang.org/protobuf/proto` index decode | `scip_parser.go` | `index.scip` is a precomputed protobuf index produced by an external indexer. It is ingested, not parsed from source; SCIP supplements native tree-sitter output, it does not replace a grammar. |
| Dockerfile (`__dockerfile__`) | `bufio` instruction scanner | `dockerfile_language.go`, `dockerfile/metadata.go`, `dockerfile/tokens.go` | A Dockerfile is a build manifest of `FROM`/`COPY`/`ARG`/`ENV`/`LABEL` instructions, not a programming language. The scanner extracts bounded runtime evidence (stages, ports, args, envs, labels); there is no source grammar to migrate. |
| Java metadata (`java_metadata`) | declarative file scanner | `java_metadata_files.go`, `java/metadata.go`, `java/parser_metadata.go` | `META-INF/services/*`, `AutoConfiguration.imports`, and `spring.factories` are declarative service-registration manifests, not Java source. They are scanned for registered class names; the `.java` grammar work lives in the `java` adapter row above. |

The README cross-references this section from its tree-sitter support table.

## Common changes and how to scope them

- **Add a new language adapter** →
  1. Add a `Definition` entry to `defaultDefinitions()` in `registry.go` with a
     unique `ParserKey`, `Language`, `Extensions` and/or `ExactNames`.
  2. Prefer a `LanguageProvider` on the definition so parse/pre-scan behavior
     enters through the provider contract instead of shared engine switches.
  3. Create `<language>_language.go` or a language-owned package for the parse
     and optional pre-scan behavior.
  4. Add capability flags that truthfully describe tree-sitter, SCIP, and
     pre-scan support; register reducer code-call resolver depth separately.
  5. Add fixtures in the parser fixture corpus and run
     `go test ./internal/parser -count=1`.
  6. Update `internal/content/shape` if the new language emits entity keys that
     `shape.Materialize` must handle.
  7. Document the new language in the `README.md` language table.

- **Add a new entity key to an existing adapter** →
  1. Add the key to the adapter's output `map[string]any`.
  2. Add the key to the `snapshotEntityBuckets` table in
     `go/internal/collector/git_snapshot_native.go` if it is an entity type that
     the collector materializes into a content entity snapshot.
  3. Update `shape.Materialize` in `internal/content/shape`.
  4. Add a fixture test that asserts the new key appears in output for a known
     input.
  5. Update `entityTypeLabelMap` in `internal/projector/canonical.go` if the new
     entity type needs a graph node label.

- **Add SCIP support for a new language** →
  1. Add the extension-to-`scipLanguageConfig` entry in `scip_support.go`.
  2. Add the language to `scipLanguagePriority` at the appropriate priority
     position.
  3. Verify the external binary name matches what `SCIPIndexer.LookPath` would
     find.
  4. Add a test in `scip_parser_test.go` with a known SCIP index fixture.

- **Change pre-scan behavior for a language** →
  1. Edit the `preScan<Language>` function.
  2. Add a test case in `engine_<language>_*_test.go` or a new test file.
  3. Verify output is still deterministic — sort results before returning.

## Failure modes and how to debug

- Symptom: `eshu_dp_file_parse_duration_seconds` elevated for a language →
  likely cause: expensive tree-sitter query or large file → check per-language
  parse complexity in `engine_<language>_semantics_test.go` benchmarks; consider
  adding a file-size guard in the adapter.

- Symptom: entity counts drop for a language after a registry change →
  likely cause: new `Definition` duplicate rejected by `NewRegistry`, so
  `DefaultRegistry()` panics at startup → check process startup logs for
  `default parser registry is invalid`; verify the new `ParserKey` and
  extensions are unique.

- Symptom: `no parser registered` error for a file extension →
  likely cause: the extension is not in `defaultDefinitions()` or the
  file was excluded earlier in the discovery pass →
  add the extension to the correct `Definition.Extensions` list.

- Symptom: SCIP path produces no `SCIPParseResult` →
  likely cause: `scip-*` binary not on PATH, or `DetectSCIPProjectLanguage`
  returned `""` because no allowed language files exist → check the
  SCIP_LANGUAGES env var; verify binary availability with `which scip-go`
  (or equivalent).

- Symptom: import map is non-deterministic across runs →
  likely cause: `preScanOnePath` returns unordered names, or a language adapter
  iterates a map without sorting → sort names before returning from every
  `preScan<Language>` function; verify `sortPreScanResults` is called.

## Anti-patterns specific to this package

- **Calling `NewRuntime()` per file or per goroutine** — tree-sitter grammar
  loading is expensive. One shared `Runtime` is the correct model.

- **Importing internal/collector, internal/projector, or internal/storage** —
  the parser package is a leaf that `internal/collector` and `internal/query`
  depend on. Reverse or lateral imports create cycles or break the ownership
  boundary.

- **Letting child parser packages import the parent parser package** — language
  helper packages such as `internal/parser/java`,
  `internal/parser/javascript`, `internal/parser/python`,
  `internal/parser/golang`, `internal/parser/groovy`,
  `internal/parser/dockerfile`, and the extracted first-wave adapters exist to
  remove parent-package sprawl. Keep their APIs typed and parent-independent.

- **Emitting new entity keys without updating shape.Materialize** — keys not
  consumed by `shape.Materialize` are silently discarded. The fixture tests will
  not catch this unless a test asserts on the content entity output.

- **Non-deterministic map iteration in a language adapter** — Go map iteration
  order is randomized. Always collect map entries into a slice, sort, then
  process. Any randomness in parse output breaks fact idempotency.

- **Returning partial output on a parse error instead of an error value** — if
  a language adapter encounters a parse error, it should return an error, not a
  partial `map[string]any`. Partial output produces incomplete facts that are
  hard to distinguish from correct empty-entity files.

## What NOT to change without an ADR

- `defaultDefinitions()` extension assignments once a language has production
  fixture coverage — reassigning an extension (e.g. moving `.ts` from
  `typescript` to a new key) changes which parser runs on existing indexed
  files and breaks fact idempotency for those repos.
- SCIP language priority in `scipLanguagePriority` — the priority order
  determines which language wins in mixed-language repos; changing it alters
  SCIP-path fact output for all repos with multiple SCIP-capable languages.
- `Registry` mutability contract — the registry is used concurrently by the
  pre-scan worker pool; any mutable state addition requires proof of
  thread-safety and a test.

## Evidence notes

No-Regression Evidence: `go test ./internal/parser -run 'TestEngine(DispatchesRegisteredLanguageProvider|SkipsProviderPreScanWithoutCapability)' -count=1` failed before provider dispatch and pre-scan capability gating existed, then passed after custom registry providers could parse and opt into pre-scan without shared engine switch edits. `go test ./internal/reducer -run TestResolveGenericCalleeUsesLanguageResolverBeforeRepoUniqueName -count=1` failed before language-specific code-call resolvers were registered outside the generic resolver, then passed after the Go resolver branches moved behind phase-ordered registration while preserving the previous branch order.

No-Observability-Change: provider dispatch and code-call resolver registration add no graph query, queue, worker, lease, batch, runtime knob, metric instrument, metric label, route, or status field. Operators still diagnose parser behavior through existing collector parse-stage logs and `eshu_dp_file_parse_duration_seconds`, and code-call materialization through existing durable intent rows plus the existing completion log.

### Single physical read per `ParsePath` call (issue #4515, front-half throughput)

No-Regression Evidence: `go test ./internal/parser -run 'TestParsePathReadsSourceExactlyOnce|TestParsePathReadsRawTextSourceExactlyOnce' -count=1` failed before the single-read cache existed (`ParsePath` read a Go and a raw_text fixture's bytes twice: once inside the dispatched language parser via `shared.ReadSource`/`readSource`, once again for `inferContentMetadata`), then passed after `ParsePath` primed a call-scoped cache (`shared.PrimeSource`/`shared.ClearSource`, keyed by absolute path) that both `shared.ReadSource` and the engine-local `readSource` consult before touching disk. Full `go test ./internal/parser/... -count=1` (every language sub-package) stayed green with no fixture or golden-gate change, which is the byte-identity proof: no sub-package `Parse` signature changed, so every language's payload is built from the identical bytes it would have read itself. See the parent README's "Single physical read per `ParsePath` call" section for the `BenchmarkParsePath` before/after numbers.

No-Observability-Change: this adds no metric, span, structured log, status field, queue, graph write, worker, lease, batch, or runtime knob. Operators still diagnose parse behavior through the existing collector `telemetry.FileParseDuration` instrument and `collector snapshot stage completed` logs.

### Cyclomatic complexity per language (issue #3488)

No-Regression Evidence: `go test ./internal/parser -run TestCyclomaticComplexityPerLanguage -count=1`
and `go test ./internal/parser/shared -run TestCyclomaticComplexity -count=1` failed
before the shared McCabe walker existed (C, C++, Java, C#, Rust, Scala emitted no
`cyclomatic_complexity` field; Go and Python omitted short-circuit boolean
operators), then passed after `shared.CyclomaticComplexity` drove every
tree-sitter adapter from per-language `shared.BranchNodeSet` tables. Backend:
tree-sitter grammars vendored in `go/go.mod` (`go-tree-sitter v0.25.0`). Input
shape: single-file fixtures with hand-counted decision points (straight-line = 1,
branchy = 1 + each if/elif/loop/case/match-arm/catch/ternary/`&&`/`||`). The walk
adds one bounded extra named+anonymous traversal of each already-parsed function
subtree at parse time; complexity is a pure function of the function node, so it
adds no queue, lease, worker, batch, Cypher, or graph-write work and stays within
the existing per-file parse budget measured by
`eshu_dp_file_parse_duration_seconds`. Full `go test ./internal/parser/... -count=1`
stayed green, so no language regressed.

No-Observability-Change: complexity is written to the existing
`cyclomatic_complexity` function-entity field that `content/shape` already
forwards to the graph node property read by `find_most_complex_functions` and
`calculate_cyclomatic_complexity`. No new metric instrument, metric label, span,
log line, status field, env var, queue, worker, lease, batch, runtime knob, or
graph query is added. SCIP definitions now emit `0` (unknown) instead of a
fabricated `1`; rankings exclude `0` via the existing
`WHERE coalesce(e.cyclomatic_complexity, 0) > 0` filter, so operators see fewer
misleading rows, not a changed observability surface.

No-Regression Evidence (PR #3523 review follow-up): `go test ./internal/parser
-run TestCyclomaticComplexityCatchAndDefaultArms -count=1` locks in two McCabe
edge cases. Exception handlers each add one decision point (verified C++
try/catch via a compiled grammar probe: the vendored tree-sitter-cpp grammar
does emit a named `catch_clause`, so C++ catch increments and is not silently
zero; Java/C#/Scala catch and Python except confirmed the same). The switch
`default` arm and the bare Rust/Scala/Python wildcard `_` arm are the implicit
else and are excluded, so a switch or match whose only arm is the catch-all
stays complexity 1; this fixed a real over-count where Java `switch_label`, C#
`switch_section`, C/C++ `case_statement`, Rust `match_arm`, and Scala/Python
`case_clause` previously counted the catch-all as a decision. A guarded wildcard
(`_ if cond`) still counts because the guard is a decision. Go was already
correct because its grammar emits a distinct `default_case` node left out of the
branch kinds. This adds no runtime, queue, or graph-write cost; the walk shape is
unchanged aside from a bounded direct-child check on switch/match arm nodes.

No-Observability-Change (PR #3523 review follow-up): the catch/default/wildcard
correctness fix touches only the computed `cyclomatic_complexity` value; no
metric, span, log, status field, env var, queue, worker, lease, batch, runtime
knob, or graph query changes.

### Kotlin/Swift symbol extraction parity (issue #3486)

Context: issue #3486 (part of the #3480 capability regression audit) claimed
Kotlin produced zero symbol extraction and Swift was partial regex. A diagnostic
probe of the shipped `origin/main` adapters disproved the zero-extraction claim:
both already emitted classes, functions, variables, imports, and calls. The real
open items were the acceptance criteria — golden-fixture gates and capability
state matching reality — plus two measured accuracy gaps: Swift `extension`
members were orphaned (no `class_context`) and Kotlin dropped unqualified bare
calls. This change fixes both, adds golden gates, and corrects the README
capability state from "parser rewrite pending" to the real hybrid line-scan +
tree-sitter-syntax design.

No-Regression Evidence:
`go test ./internal/parser -run 'TestDefaultEngineParsePathSwiftExtensionMethodsCarryClassContext|TestDefaultEngineParsePathKotlinExtractsBareCalls|TestKotlinComprehensiveSymbolExtractionGate|TestSwiftComprehensiveSymbolExtractionGate' -count=1`
failed before the fix (Swift extension methods carried no `class_context`; Kotlin
bare calls like `info(...)`/`println(...)` were absent from `function_calls`) and
passes after. Full `go test ./internal/parser/... -count=1` (1118 tests) plus
`./internal/mcp ./internal/query ./internal/content/shape` (4972 total) and
`./internal/reducer/... ./internal/collector/... ./internal/projector/...` stayed
green, so the added Swift `extension` scope and Kotlin bare-call rows did not
regress any existing parser, reducer call-materialization, or content-shape
contract. Backend: tree-sitter grammars vendored in `go/go.mod`
(`go-tree-sitter v0.25.0`, `tree-sitter-kotlin v1.1.0`, Swift grammar v0.21.0).
Input shape: single-file Kotlin/Swift fixtures plus the comprehensive
`tests/fixtures/ecosystems/{kotlin,swift}_comprehensive` corpora with
hand-checked symbol sets. The extension scope reuses the existing per-line
brace-depth stack and the bare-call scan reuses the existing per-line
`seenLineCalls` dedup, so neither adds queue, lease, worker, batch, Cypher, or
graph-write work; both stay within the existing per-file parse budget measured by
`eshu_dp_file_parse_duration_seconds`.

No-Observability-Change: symbols flow through the existing `functions`,
`classes`, `function_calls`, and `class_context` payload keys that `content/shape`
already forwards; the Swift `extension` scope emits no new type entity and the
Kotlin bare-call path emits the existing `function_calls` row shape. No new metric
instrument, metric label, span, log line, status field, env var, queue, worker,
lease, batch, runtime knob, or graph query is added. Operators still diagnose
Kotlin/Swift parsing through the existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.
