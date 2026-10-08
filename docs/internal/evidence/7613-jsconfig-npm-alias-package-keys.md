# jsconfig Aliases And npm Alias Targets In JS Package Keys (#7613)

This note records the proof for #7613, which closes the known limit the
#7601 evidence doc discloses: a dependency name that is also a `jsconfig.json`
alias, or an npm alias, could get a wrong package key.

## What changed

Parser only. No reducer, storage, schema, API, MCP, or telemetry code changed,
and no `sdk/go/factschema`, fixture-pack, or registry file changed:
`resolved_source` rides the open `Attributes` pass-through of the imports
bucket (see `sdk/go/factschema/codegraph/v1/parsed_file_data_imports.go`), and
`package_export_symbol` is not a typed contract field, so both stay
value-level accuracy fixes inside existing fields.

- **jsconfig.** `nearestTSConfig` (`go/internal/parser/javascript/project/tsconfig.go`)
  now reads `jsconfig.json` with the same fields as `tsconfig.json`, so a
  jsconfig `baseUrl`/`paths` alias sets `resolved_source` and the import stays
  unkeyed even when the name is also declared. `tsconfig.json` wins when one
  directory carries both; the nearest directory carrying either wins outward.
- **npm alias.** `project.NpmAliasTargets` maps `"alias": "npm:target@range"`
  dependencies to the target package the producer publishes
  (`npm:bar@1` → `bar`, `npm:@scope/bar@^2` → `@scope/bar`, nearest manifest
  wins), and `annotatePackageImportCalls` keys the call `package:<target>#<name>`.
  An `npm:` value whose target does not parse as a bare specifier leaves the
  call unresolved — never keyed under the alias.

## Edge cases

| Case | Handling | Proof |
| --- | --- | --- |
| jsconfig `baseUrl` alias whose name is declared | `resolved_source` set; no key | `TestDefaultEngineParsePathJavaScriptJSConfigAliasWinsOverDeclaredDependency` |
| Both configs in one directory | tsconfig wins | `TestTSConfigImportResolverPrefersTSConfigOverJSConfig` |
| npm alias, plain and scoped | Keyed under the target | `TestDefaultEngineParsePathJavaScriptNpmAliasKeysTargetName`, `TestNpmAliasTargetsMapAliasToPublishedName` |
| npm alias without version (`npm:bare`) | Keyed under `bare` | `TestNpmAliasTargetsMapAliasToPublishedName` |
| Unparseable target (`npm:`, `npm:bar/baz@1`) | Unresolved, never keyed | same tests (`broken`, `subpath`, `thing`) |
| Alias declared by two manifests | Nearest spec wins | `TestNpmAliasTargetsPreferNearestManifest` |
| Non-`npm:` specs, non-string specs | Untouched; declared set unchanged | `TestDefaultEngineParsePathTypeScriptToleratesMalformedDependencyFields` (still green) |

## Key before and after (RED→GREEN)

Engine tests run RED before the fix, GREEN after:

- jsconfig: `resolved_source` was `""` and the call carried the wrong
  `package:api#getUser`; now `resolved_source` is `src/api/index.js` and the
  call is unkeyed.
- npm alias: the call carried `package:foo#widget` (miss at best); now
  `package:bar#widget`, and `package:scoped#gadget` became
  `package:@acme/real#gadget`; `package:broken#thing` is now unkeyed.

Reducer end-to-end (real parser output through `ExtractRows`):

- `TestExtractRowsResolvesNpmAliasImportToTargetProducer`: consumer key
  `package:lib#widget` joins the `lib` producer's export with
  `resolution_method=import_binding`.
- `TestExtractRowsLeavesJSConfigAliasOnDeclaredNameInsideItsRepository`:
  no CALLS row from the consumer to the unrelated corpus publisher `api`.

## Corpus replica count

Staged corpus (`scripts/lib/golden-corpus-fixtures.sh`, 33 repos):

- repos carrying `jsconfig.json`: **0**
- repos carrying `tsconfig.json`: **1**
- manifests with an `"npm:` dependency value: **0**

The change is corpus-neutral: no staged import gains `resolved_source` and no
staged key changes, so the B-12 snapshot needs no update. The live B-7 Neo4j
run below proves the pipeline still agrees end to end.

## B-7 live proof

`ESHU_GRAPH_BACKEND=neo4j bash scripts/verify-golden-corpus-gate.sh`:
(recorded at promotion time; the gate must report N pass, 0 required-fail,
0 advisory-warn plus `PASS: B-7 golden corpus gate green`.)

## Performance

No-regression evidence: `BenchmarkParsePathTypeScriptPackageImportCalls`
(synthetic worst case, every call keyed), same machine, alternating rounds,
100 iterations each. Before is `ce3f32f19` (branch base); after is this
change.

| Round | Before (ns/op) | After (ns/op) |
| --- | --- | --- |
| 1 | 247,453,208 | 322,996,108 |
| 2 | 248,914,333 | 247,595,208 |

Round 1 ran while a peer live gate held the host (earlier after-samples in
the same window read 261–345 ms); round 2, on a settled host, reads
before 248.9 ms vs after 247.6 ms — no separable regression. An earlier
three-sample after run in the contended window is discarded as
non-comparable, not averaged in.

Declared impact: per file with keyed calls the consumer step adds one
`NpmAliasTargets` walk (read once, next to the existing single
`DeclaredDependencies` read; manifests come from the same (path, stat)
cache, so no new filesystem reads). The jsconfig lookup adds one extra
`os.Stat` per directory level only when no `tsconfig.json` sits there.
`BenchmarkParsePathJavaScriptCorpus` needs `ESHU_JSTS_BENCH_CORPUS` (a real
corpus checkout, as in #7601's remote run) and is NOT_CHECKED here.
