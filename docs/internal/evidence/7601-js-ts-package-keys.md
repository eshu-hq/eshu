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
| Positive | `TestExtractRowsResolvesParsedJavaScriptPackageImportAcrossRepositories` parses a producer and a consumer repository (the consumer manifest declares the producer) through `parser.DefaultEngine` and asserts `CALLS` rows to the producer's function and class with `resolution_method=import_binding` and the consumer's `repo_id`. Golden `package_import_resolves_across_repositories` pins the same with confidence 0.90 | Checked |
| Negative | Every row of the edge-case table above | Checked |
| Ambiguous | Two producers publishing one name give no cross-repository row (parser-backed test and golden). A mutation that let `uniqueCodeCallSymbolCandidates` keep two candidates made the parser-backed test fail | Checked |
| Query | The golden query shape `POST /api/v0/code/dead-code/cross-repo?golden_scope=format-kit` (see Golden corpus) | Checked live on Neo4j (below) |
| Graph | A direct Neo4j read of the cross-repository `CALLS` edge | NOT_CHECKED; the query shape reads the reachability rows built from that edge, not the edge itself |

RED: with the parser change disabled, the positive parser-backed reducer test
found no `renderPage -> formatPrice` row (only the consumer's own `round`).
After the declared-dependency rule landed, the same test went red again until
its consumer manifest declared `@acme/format`, which proves the rule is wired
through the reducer path.

### Known gap that predates this change (#7610)

When a key resolves to nothing (no producer in the corpus, or two producers),
the call still falls through to the repository-unique-name fallback. If the
consumer has its own function with that name in another file, the call links
there. The same call without a key resolves the same way on `origin/main`, so
this change does not make it worse, and a resolved key always wins first.
Golden `package_import_unresolved_falls_back_to_local_name` records it with
`falsePositiveGap` naming #7610.

### Golden corpus

The corpus gains a producer and consumer pair, staged from
`scripts/lib/golden-corpus-fixtures.sh` (31 to 33 repositories):

- `tests/fixtures/ecosystems/format-kit/`: `package.json` named
  `@acme/format-kit` with no entry fields, so no export is a package root.
  `src/index.js` exports `formatPrice` (called nowhere in the repository),
  `formatRounded`, a private `roundCents`, and classes `Money` and `Ledger`;
  `src/dates.js` exports `formatDate`.
- `tests/fixtures/ecosystems/storefront-web/`: `package.json` declares
  `@acme/format-kit` in `dependencies` and names `./dist/index.js` in
  `exports`, so `src/index.ts`'s exported `renderCheckout` is a package-export
  root (a function named `main` would have collided with other golden shapes
  that search for `main`). `renderCheckout` calls `formatPrice`
  (the keyed edge), its own `roundCents`, and `formatDate` through the
  `@acme/format-kit/dates` subpath (unkeyed). `Money` (value import) and
  `Ledger` (`import type`) appear only as type annotations.

Parser output for those files (checked with `parser.DefaultEngine`): the only
keyed consumer call is `formatPrice` with `package:@acme/format-kit#formatPrice`;
`Money` and `Ledger` are `typescript.type_reference` rows with no key;
`formatDate` and `roundCents` carry no key.

No `CALLS` `rc-NN` was added: the correlation schema filters only by labels,
relationship type, and `evidence_kinds`, so it would pass on any in-repository
call (`rc-11` already asserts that shape). Instead, query shape
`POST /api/v0/code/dead-code/cross-repo?golden_scope=format-kit` asks the
cross-repo dead-code route about `format-kit` with consumer `storefront-web`
and requires, closed on `(name, classification)`, that `formatPrice` is
`live_by_consumer`, plus a path for its `consumer_evidence[].citation`.
Without the cross-repository edge, `formatPrice` has no consumer evidence and
the route classifies it `dead`, so the object match fails.

Snapshot counts that move: `corpus_composition.git_repos` and the
`Repository` ceiling go from 31 to 33 (the static test keeps both in lockstep
with the fixture list). Every other node and edge tolerance is a wide range
that two small repositories cannot cross; `get_repository_stats` keeps its
floor of 31.

Live B-7 run, Neo4j, on the remote validation host, 2026-10-05, from a git
checkout of the committed branch (`ESHU_GRAPH_BACKEND=neo4j bash
scripts/verify-golden-corpus-gate.sh`). It was not run on the local machine.

- GREEN at `fb02b10cd`: `573 pass, 0 required-fail, 3 advisory-warn` and
  `PASS: B-7 golden corpus gate green`. The new shape passed, `Repository`
  counted 33, and `CALLS` counted 33 (29 before the fixtures). The three
  warnings are the advisory per-phase timing bands, which are calibrated to a
  different machine.
- RED: `f685bae44` (no parser keys) plus only the two fixture commits:
  `572 pass, 1 required-fail`. The one failure is this shape
  (`live_by_consumer[]` resolved no values), and `CALLS` counted 32: the
  cross-repository edge is the difference.
- A first GREEN attempt failed `mcp:resolve_entity` (`count` 3, want 2) because
  the consumer's entry function was named `main`, which that shape searches
  for. `fb02b10cd` renamed it and roots it through `exports` instead.

## Real-corpus check

Run on the remote validation host from a git bundle of the committed branch
(no uncommitted files), parsing 22 checked-out repositories with the same
directory exclusions as repository discovery (`node_modules`, `dist`, `build`,
and the rest). Producer definitions were joined to consumer keys the way the
loader and reducer do: a key resolves only when exactly one definition carries
it.

| Run | Keyed calls | Resolved | Ambiguous | Resolved by shape |
| --- | --- | --- | --- | --- |
| `0392ce9de` (before the declared-dependency rule) | 25,690 | 78 | 1 | 48 `new Local(`, 23 `local(`, 4 `ns.member(`, 3 `new Local(` in test files |
| `d16a06158` (declared-dependency rule) | 25,144 | 60 | 1 | 33 `new Local(`, 23 `local(`, 3 `ns.member(`, 1 `new Local(` in test files |

- All 78 earlier resolved calls were hand-checked against consumer and
  producer source: 78 true, 0 false. The 60 that still resolve are a subset of
  those 78 (no call resolves now that did not before).
- The 18 that dropped all import a package that no `package.json` on their
  path declares: 15 are codemod fixture inputs in a repository that does not
  depend on the logging package, 2 are test files importing a transitive
  dependency, and 1 is a `require` of a transitive dependency. They were true
  references and are now misses, which is the price of never keying an
  undeclared bare name.

Test-file hand-check (arbiter requirement): 40 keyed calls sampled from test
files of the three named consumers (16, 14, and 10) on `0392ce9de`, checked
against consumer source. 40 true, 0 false, 0 undecidable: each is a real call
of the imported binding. The declared-dependency rule only removes keys, so
the post-fix set is a subset. None of the 40 resolves, because their producers
are generated or outside the corpus. Under this change's shapes, the three
named producers publish no keyed export these consumers call: one consumer
calls only methods of a default-imported logger instance, one producer exports
`require` objects, and one is a CommonJS object. Those pairs stay misses, not
false links.

## Performance

Declared impact. Per file the consumer step is O(imports + calls) map work,
plus, only when a call is keyed, one walk up the directory chain reading cached
manifests for declared dependencies, one byte search per distinct bound name,
and an AST lookup per occurrence (`redeclaredImportNames`). The producer step
adds one nearest-manifest lookup per file (a stat walk to the repository root;
the manifest parse is cached) and O(1) parent lookups per declaration. Nothing
is O(calls x imports).

Performance Evidence: remote validation host (AWS EC2 r7a.4xlarge, 16 vCPU,
128 GiB, Linux amd64), 2026-10-05. Before is `f685bae44` plus the benchmark
commits; after is `d16a06158` for the corpus and `2dbb6a03d` for the synthetic
file (same parser code). Rounds alternate before and after.

| Benchmark | Before | After |
| --- | --- | --- |
| `BenchmarkParsePathJavaScriptCorpus` (22 repositories, one pass) | 46.50, 46.25, 46.30, 46.53 s (mean 46.40 s) | 46.99, 46.99, 47.09, 47.37 s (mean 47.11 s) |
| `BenchmarkParsePathTypeScriptPackageImportCalls` (worst case, every call keyed) | 230.0 to 239.8 ms (12 samples, median 232.3) | 243.1 to 248.2 ms (12 samples, median 245.7) |

- On the real corpus the change costs about +1.5% of parse wall time (0.71 s on
  a 46 s pass), slower in all four paired rounds. A CPU profile of one after
  pass attributes 0.31 s of 53.47 s sampled CPU (0.6%) to the new code:
  `annotatePackageImportCalls` 0.25 s (of which `redeclaredImportNames`
  0.15 s and `DeclaredDependencies` 0.08 s), `newPackageExportStamper` 0.05 s,
  and the stamping itself 0.01 s. Allocations rose 0.2%.
- The synthetic file is the adversarial case: 400 bound names and 2,200 keyed
  calls in one file, all declared. It costs about +5.8%, roughly 13 ms per parse.
- Earlier rounds on superseded commits: `0392ce9de` (before the dependency
  rule) measured +0.9% on the corpus and +6.5% on the synthetic file; a first
  version that walked the whole tree measured +5.6% and +3.5% on the corpus.
  The synthetic numbers for `d16a06158` itself are not comparable: its
  benchmark manifest declared nothing, so no call was keyed; `62ef19166`
  declares the packages.

The loader side was measured by #7605 with every bare named import of the worst
consumer (237 keys, third-party included). Keys now need a declared dependency,
so that is an upper bound; namespace-member keys come on top of it, and their
count on that consumer is NOT_CHECKED.

Observability Evidence: no new signal. The existing `code call materialization
completed` log carries `symbol_key_count`, `symbol_definition_fact_count`, and
`load_symbol_definitions_duration_seconds`, and each `CALLS` row carries
`resolution_method`.

## Carried forward from #7605 (#7609)

Stored manifests are not generation-tagged, so a stored manifest can be ahead
of the active generation. A manifest that adds a name is harmless: the
definition scan still matches each definition's own active `package_id`. A
manifest that drops a name is a miss for a sole producer. It over-resolves only
when one of two active producers of the same name drops its manifest in a
generation that wrote content but did not activate: the pair collapses to one
candidate scope and the key can resolve where the active truth is ambiguous.
The state heals at that repository's next successful generation. Tracked in
#7609.

## Other notes

- Review found that a file matched by a subpath `exports` pattern could claim
  the package's `#default` key. Default keys now come only from the `main` or
  `module` entry file. No resolved call in either corpus run used `#default`.
- A JSX call (`<Local />`) that resolves projects as a `REFERENCES` edge, not
  `CALLS`, through the existing code-call writer.

## Deferred

- ops-qa, after deploy: re-verify 40 resolved edges against source with 0
  false, on the post-fix candidate set (the 60 above or their ops-qa
  equivalents), and a before and after of
  `load_symbol_definitions_duration_seconds` on the same JS/TS scopes with a Go
  scope unchanged.
- A direct graph read of the cross-repository edge.
- The replica shape split of the investigation's 2,602 strict calls:
  NOT_CHECKED. The split above comes from parser output on 22 repositories.
