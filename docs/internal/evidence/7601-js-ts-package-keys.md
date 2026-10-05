# JS/TS Package Keys For Cross-Repository Calls (#7601)

This note records the proof for the second #7601 pull request: the parser
change. The first pull request (#7605) anchored the symbol-definition loader on
producer scopes; see
[7601-anchored-symbol-definition-loader.md](7601-anchored-symbol-definition-loader.md).

## What changed

The JavaScript-family parser now writes both halves of the
`package:<package_id>#<export_name>` key that the reducer already reads
(`codeCallDefinitionSymbolKeys` and `codeCallEdgeSymbolKeys` in
`go/internal/reducer/code/call/shared/symbol_index.go`). No reducer, storage,
schema, API, MCP, or telemetry code changed.

- **Producer.** An exported top-level function, generator, class, abstract
  class, or `export const|let|var X = <function>` carries `package_id` (the
  trimmed `name` of the nearest `package.json`, from
  `project.NearestPackageName`) and `export_name`. The export must sit directly
  under the program, so methods, nested functions, and members of a TypeScript
  `namespace` or `declare module` block get no key. `export default` is keyed
  as `default`, and only in the file the manifest's `main` or `module` field
  names. An `exports` target is not enough: a subpath pattern such as
  `"./plugin/*"` marks files whose default belongs to `pkg/plugin/x`, not to
  `pkg`. A file whose nearest manifest has no string name gets no key.
- **Consumer.** After the declaration walk, `annotatePackageImportCalls`
  binds each `function_call`, `constructor_call`, and `jsx_component` to the
  file's `imports` rows. A call gets
  `package_export_symbol = package:<source>#<imported name>` only when the import is
  a bare package specifier (`pkg` or `@scope/pkg`) with no `resolved_source`,
  not type-only, not a re-export, and declared in the `dependencies`,
  `devDependencies`, `peerDependencies`, or `optionalDependencies` object of a
  `package.json` on the path from the file up to the repository root
  (`project.DeclaredDependencies`, a union over those manifests). Two shapes are keyed: `local(`,
  `new Local(`, `<Local />` for a named or default import, and `ns.member(` (also
  `new ns.Member(` and `<ns.Member />`) for a namespace import
  (`import * as ns`, `const ns = require("pkg")`, `import ns = require("pkg")`).

## Edge cases

| Case | Handling | Proof |
| --- | --- | --- |
| Type reference (`x: Logger`) | Still emitted as `typescript.type_reference`; no key | `TestDefaultEngineParsePathTSXStampsConsumerPackageExportSymbols` |
| `import type { X }`, `import { type X }` | Binding dropped; no key | same test |
| Relative, absolute, `#internal`, `node:` specifier | Not bare; no key | same test (`./rel`) and `isBarePackageSpecifier` |
| Subpath (`pkg/sub`, `@scope/pkg/sub`) | No key: a miss, never a false link | same test |
| In-repo alias with `resolved_source` (tsconfig `baseUrl` or `paths`) | No key, even when the name is also declared | same test (`shared`), `TestDefaultEngineParsePathTypeScriptKeysOnlyDeclaredDependencies` (`utils`) |
| jsconfig or bundler alias that looks bare (`api`), Node.js built-in (`fs`, `util`), undeclared package (`lodash`) | No key: no manifest declares it | `TestDefaultEngineParsePathJavaScriptSkipsUndeclaredBareImports` |
| `@/x`, `~/x`, `node:fs` | Not a package specifier; no key | `TestDefaultEngineParsePathTypeScriptKeysOnlyDeclaredDependencies` |
| Dependency declared only in the root manifest (hoisted), only in the nearest workspace manifest, or as a dev, peer, or optional dependency | Keyed | same test, plus the test-file `@acme/dev` case |
| Dependency field that is not an object (array, string) | Declares nothing; other fields still count | `TestDefaultEngineParsePathTypeScriptToleratesMalformedDependencyFields` |
| `a.b.c()` deep chain, `obj.member()` on a non-import | No key | same test |
| `D.member()` on a default import | No key. A default export is often an instance or a class, so the member is a method, not a named export (`import logger from "pkg"; logger.info()`) | same test (`render.member`) |
| Name declared again in the file (parameter, variable, destructuring, catch, function or class name) | No key for that name anywhere in the file | same test (`shadowed`, `dup`, `destructured`, `caught`) |
| Name in a comment or a string | Ignored; the real call is still keyed | same test (`formatPrice`) |
| Same local name bound twice to different targets | No key | `packageImportBindings` |
| Non-exported helper with an exported name elsewhere | Helper gets no key | `TestDefaultEngineParsePathTypeScriptStampsProducerPackageExportKeys` (`inner`) |
| Default export of an internal module | No key | `TestDefaultEngineParsePathTypeScriptKeepsDefaultExportKeysToPackageEntryFiles` |
| Default export of a file matched by a subpath `exports` pattern | No key; a package with only `exports` and no `main` or `module` gets no default key (a miss) | `TestDefaultEngineParsePathTypeScriptKeepsDefaultExportKeysOffSubpathExports` |
| No manifest, unnamed manifest, non-string name | No producer key | `TestDefaultEngineParsePathTypeScriptSkipsProducerKeysWithoutNamedManifest` |
| Call in a test file | Keyed like any other call (arbiter ruling) | `TestDefaultEngineParsePathTypeScriptKeysTestFileCalls` |
| Two repositories publish the same name | Two definitions carry the key; the reducer leaves it unresolved | `TestExtractRowsLeavesParsedJavaScriptPackageImportUnresolvedWhenTwoProducersShareTheName`, golden `package_import_published_twice_unresolved` |

Not keyed in this pull request, so these stay misses: static and instance
method calls (`Logger.create()`), members of named imports, export clauses
(`export { x }`), `export default ident`, CommonJS producers
(`exports.x = fn`, `module.exports = {...}`), and re-export chains.

## Correlation truth matrix

| Axis | Proof | Status |
| --- | --- | --- |
| Positive | `TestExtractRowsResolvesParsedJavaScriptPackageImportAcrossRepositories` parses a producer and a consumer repository through `parser.DefaultEngine` and asserts `CALLS` rows to the producer's function and class with `resolution_method=import_binding` and the consumer's `repo_id`. Golden `package_import_resolves_across_repositories` pins the same with confidence 0.90 | Checked |
| Negative | Every row of the edge-case table above | Checked |
| Ambiguous | Two producers publishing one name give no cross-repository row (parser-backed test and golden). A mutation that let `uniqueCodeCallSymbolCandidates` keep two candidates made the parser-backed test fail | Checked |
| Graph | A direct Neo4j read of the cross-repository `CALLS` edge | NOT_CHECKED; needs a live stack |
| Query | The cross-repository dead-code route showing consumer evidence | NOT_CHECKED; needs a live stack |

RED: with the parser change disabled, the positive parser-backed reducer test
found no `renderPage -> formatPrice` row (only the consumer's own `round`).

### Known gap that predates this change

When a key resolves to nothing (no producer in the corpus, or two producers),
the call still falls through to the repository-unique-name fallback. If the
consumer has its own function with that name in another file, the call links
there. The same call without a key resolves the same way on `origin/main`, so
this change does not cause it, and a resolved key always wins first. Golden
`package_import_unresolved_falls_back_to_local_name` records it with
`falsePositiveGap`. Blocking the fallback for keyed calls would also drop true
same-repository edges for workspace packages whose exports this parser does not
key yet, so it needs its own measurement and issue.

### Golden corpus

No fixture pair or `rc-NN` was added. The snapshot's correlation schema can
filter only by labels, relationship type, and `evidence_kinds`, and `CALLS`
edges carry no evidence kinds. A new `CALLS` Function to Function correlation
would therefore pass on any in-repository call (`rc-11` already asserts that
shape), which is a false green. A real cross-repository assertion needs a new
gate predicate (a Cypher read plus its performance evidence) and a live Neo4j
recalibration run. That is left for a follow-up with the graph and query rows
above. The `CALLS` tolerance (min 29, max 200000) cannot be crossed by this
change. Parsing every `tests/fixtures/ecosystems/` repository with this branch
gives 15 keyed calls and no producer key at all, so no new edge appears.

## Real-corpus check

Run on the remote validation host from a git bundle of the committed branch
(no uncommitted files), parsing 22 checked-out repositories with the same
directory exclusions as repository discovery (`node_modules`, `dist`, `build`,
and the rest). Producer definitions were joined to consumer keys the way the
loader and reducer do: a key resolves only when exactly one definition carries
it.

- 25,690 calls carried a key. Most name packages outside the corpus and can
  never resolve.
- 78 calls resolved, 1 key was ambiguous (three definitions), the rest had no
  producer definition. By shape: 48 `new Local(`, 23 `local(`, 4 `ns.member(`
  in non-test files, and 3 `new Local(` in test files.
- All 78 resolved calls were hand-checked against consumer and producer
  source: 78 true, 0 false. Each consumer line calls the binding imported from
  the package, and each target is that package's exported declaration.

Test-file hand-check (arbiter requirement): 40 keyed calls sampled from test
files of the three named consumers (16, 14, and 10), checked against consumer
source. 40 true, 0 false, 0 undecidable: each is a real call of the imported
binding (mock-client helpers, assertion and render helpers, a CommonJS
property import, a whole-module `require` member). None of the 40 resolves,
because their producers are generated or outside the corpus, so none can
create an edge. Under this change's shapes, the three named producers publish
no keyed export these consumers call: one consumer calls only methods of a
default-imported logger instance, one producer exports `require` objects, and
one is a CommonJS object. Those pairs stay misses, not false links.

## Performance

Declared impact. Per file the consumer step is O(imports + calls) map work,
plus, only when a call is keyed, one byte search per distinct bound name and an
AST lookup per occurrence (`redeclaredImportNames`). The producer step adds one
nearest-manifest lookup per file (a stat walk to the repository root; the
manifest parse is cached) and O(1) parent lookups per declaration. Nothing is
O(calls x imports).

Performance Evidence: remote validation host (AWS EC2 r7a.4xlarge, 16 vCPU,
128 GiB, Linux amd64), 2026-10-05. Before is `f685bae44` plus the benchmark
commit; after is this branch. Rounds alternate before and after.

| Benchmark | Before | After |
| --- | --- | --- |
| `BenchmarkParsePathJavaScriptCorpus` (22 repositories, one pass) | 46.71, 46.41, 46.24, 46.44 s (mean 46.45 s) | 46.64, 46.91, 46.92, 47.06 s (mean 46.88 s) |
| `BenchmarkParsePathTypeScriptPackageImportCalls` (worst case, every call keyed) | 228.0 to 235.4 ms (12 samples) | 243.9 to 250.4 ms (12 samples) |

- On the real corpus the change costs about +0.9% of parse time (0.43 s on a
  46 s pass); after was slower in each of the four paired rounds, by 0.07 to
  0.68 s. Allocations rose 0.18% and bytes 0.22%. Which of the two steps
  carries this cost was not profiled: NOT_CHECKED.
- The synthetic file is the adversarial case: 400 bound names and 2,200 keyed
  calls in one file. It costs about 6.5%, roughly 15 ms per parse.
- A first version walked every named node of every file with a keyed call. It
  cost +5.6% and +3.5% on the corpus (48.63 s and 48.51 s against 46.04 s and
  46.86 s). The occurrence lookup replaced it and gives the same results: every
  shadowing test passes, and a mutation that ignores re-declarations fails them.

The loader side was measured by #7605 with every bare named import of the worst
consumer (237 keys, third-party included). Namespace-member keys come on top of
that; their count on that consumer is NOT_CHECKED.

Observability Evidence: no new signal. The existing `code call materialization
completed` log carries `symbol_key_count`, `symbol_definition_fact_count`, and
`load_symbol_definitions_duration_seconds`, and each `CALLS` row carries
`resolution_method`.

## Carried forward from #7605

Stored manifests are not generation-tagged. If one of two repositories that
publish the same name renames its package in a pending generation, the stored
manifest already shows the new name while its active facts still carry the old
one. The pair then collapses to one producer, and the key can resolve where the
active truth is ambiguous, until that generation activates or the next one
succeeds. Now that the parser emits keys, this case is reachable. It needs an
arbiter decision before merge.

## Deferred

- ops-qa, after deploy: re-verify 40 resolved edges against source with 0
  false, and a before and after of `load_symbol_definitions_duration_seconds`
  on the same JS/TS scopes with a Go scope unchanged.
- Live graph and query proof, and the golden-corpus predicate described above.
- The replica shape split of the investigation's 2,602 strict calls:
  NOT_CHECKED. The split above comes from parser output on 22 repositories.

## Changes after the measurements

Review found that a file matched by a subpath `exports` pattern could claim the
package's `#default` key. The fix (default keys only for the `main` or `module`
entry file) landed after the corpus check and the benchmarks, which ran on
`0392ce9de`. It only removes one membership test from the producer step, and
none of the 78 resolved calls, and no producer definition in that run, used a
`#default` key, so those results still hold.

A JSX call (`<Local />`) that resolves projects as a `REFERENCES` edge, not
`CALLS`, through the existing code-call writer.

A bare-looking alias with no `resolved_source` (`import { x } from "api"`
through a jsconfig `baseUrl` or a bundler alias) and a Node.js built-in no
longer get a key: a consumer key now needs a declared dependency (see the
edge-case table). Removing that check makes the three dependency tests fail.
