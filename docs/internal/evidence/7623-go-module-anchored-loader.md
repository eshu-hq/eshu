# Go Module-Anchored Symbol-Definition Loader (#7623)

`FactStore.LoadActiveCodeCallSymbolDefinitionFacts` in
`go/internal/storage/postgres` now anchors `scip-go gomod <import_path> ...`
keys on the scopes whose stored go.mod manifests declare that import path's
module. Only those scopes' active file facts are scanned. This answers the
issue's open question affirmatively: the module path in the key resolves the
producer scopes, and the anchored answer is exactly the corpus-wide answer on
consistent data.

## What changed

The loader splits the requested keys three ways
(`splitCodeCallPackageSymbolKeys`, then `splitCodeCallGoSymbolKeys`).

- **Go keys** first run `listActiveCodeCallGoModuleManifestsQuery`. That
  statement reads the stored go.mod manifests in `content_files` for
  repository scopes that have an active generation, through the same
  MATERIALIZED-CTE shape as the package.json manifest read. Go parses each
  manifest's `module` directive (`codeCallGoModuleManifestPath`), and the
  loader keeps the scopes whose module is a prefix of a requested import
  path at a path boundary (`codeCallGoImportPathInModule`,
  `listCodeCallGoModuleProducerScopeIDs`). Then the existing anchored scan
  runs with those scopes as `$5`.
- A Go key whose module no stored manifest declares falls back to the
  corpus-wide scan with the same keys, so the answer stays exactly the
  full-scan answer. This differs deliberately from the package-key path,
  which leaves producer-less keys unresolved: a missing go.mod is a data
  gap (stale projection, GOPATH-era history), not proof the definition is
  absent.
- **Package keys** are unchanged. **All other keys** (SCIP symbols,
  malformed Go keys) keep the corpus-wide scan, now sharing one statement
  with any fallback Go keys.

The manifest read excludes no path. Discovery already prunes `vendor/`
trees, and the reducer's Go module index honors every remaining module
root, so every stored go.mod is a candidate producer. A module declared by
several repositories returns every one of them, so the reducer sees each
candidate definition and keeps the key unresolved.

The change adds no DDL, no index, no reducer code, and no API, OpenAPI, or
MCP change.

## Edge cases

| Case | Handling | Proof |
| --- | --- | --- |
| Go key, producer declared | Manifest read, then the anchored scan with exactly the producer scopes | Live `AnchorsGoKeysOnModules` |
| Fork declares the same module | Both scopes load; the reducer's unique-or-unresolved rule applies | Live `AnchorsGoKeysOnModules` (`scope:gofork`) |
| Nested module (`sub/go.mod`) | Its own producer for keys under it; parent-module scopes are prefix candidates too | Live `AnchorsGoKeysOnModules` (`scope:gosub`), shim K2 differential |
| Lookalike module (`libext` vs `lib`) | No path boundary, not a producer | Unit `TestCodeCallGoImportPathInModule`, live `$5` assertion |
| No declaring module | Corpus-wide fallback scan, same keys | Unit `FallsBackToCorpusScanWithoutModuleProducer`, live `GoKeyFallbackMatchesCorpus` |
| Malformed key (`scip-go gomod`, no import path) | Not a Go key; corpus scan with the other keys | Unit `TestSplitCodeCallGoSymbolKeys` |
| Malformed go.mod (no `module` line, `module` with no path) | Skipped; the load never fails | Unit `TestCodeCallGoModuleManifestPath` |
| Manifest whose scope has no active generation | Kept out of the producer set by the manifest join | Same join as the package read, live `AnchorsPackageKeys` covers the join |
| Superseded generation | The anchored scan joins the active generation, as before | Live `AnchorsGoKeysOnModules` (`fact-golib-stale`) |
| Mixed Go, package, and SCIP keys | One anchored scan for Go keys, one corpus scan for the rest, package path unchanged | Unit sequence tests, live `SkipsScanWithoutProducer` |
| More than one page | Every 500-row page re-runs the scan with a new keyset cursor | Live `PagesScanPerPage` (measured below) |

### Named limit: manifests are not generation-tagged

`content_files` holds the latest projected content and has no generation
column, so a go.mod that runs ahead of the active generation can add or drop
a candidate scope. When the producer set is empty the fallback scan keeps the
answer exact. When some other scope still declares the module, a renamed
module in a pending generation could drop a scope whose active facts still
carry the old keys, and those definitions would be missed until that
generation activates or the next one succeeds. This is the same window the
package-key anchor documents; module renames are rare and the window lasts
one in-flight generation.

## Measurements

Shim: throwaway `postgres:18` database on this machine, tables per
migrations 001, 002, 003, 004, 099, and 134, 100 repository scopes with 300
active file facts each (30,000 files, about 10 KB of jsonb per file, 352 MB
in TOAST), one go.mod per scope in `content_files`, vacuumed and analyzed.
Query text extracted verbatim from the shipped Go constants. One producer
scope and one fork declare `github.com/acme/lib`; a third scope declares
`github.com/acme/lib/sub`.

| Case | Buffers | Time | Rows |
| --- | --- | --- | --- |
| Corpus-wide scan, K1 (warm) | 153,731 | 1,240 ms | 4, the seeded carriers |
| Anchored scan, K1, two producer scopes (warm) | 3,414 | 31 ms | 4, identical set |
| go.mod manifest read, 100 rows | about 10 | under 1 ms | 100 |

The corpus plan is the issue's shape: a nested loop over the scopes, 300
index probes each, and the EXISTS over the five definition arrays per file.
The anchored plan probes only the producer scopes. Row-set differentials
(EXCEPT both ways) are empty for K1 and for the nested-module K2 with its
three-scope prefix superset.

The paging claim in the issue ("inferred, not measured") is now measured:
`PagesScanPerPage` seeds 1,200 matching facts and one load issues the
manifest read plus three full corpus scans, one per 500-row page. The anchor
does not change paging; it shrinks each page's scan to the producer scopes.

Performance Evidence: the anchored scan touches about 45 times fewer buffers
than the corpus scan on the same shim and returns the identical row set, so
the QA corpus-wide loads (33 to 37 s before #7697, about 6 s after, per the
issue and the #7601 note) move to producer-sized scans plus one manifest
read shaped like the 15 to 21 ms package.json read.

## Correctness proof

- Live on throwaway `postgres:18` with the bootstrap DDL:
  `go test ./internal/storage/postgres/ -run
  'TestReducerContentionGateActiveCodeCallSymbolLoader' -count=1`.
  All eight loader tests pass, including the pre-existing package-key,
  vendored-manifest, and cross-repository proofs.
- The anchor test asserts the exact producer scope array and runs the
  corpus-wide scan over the same fixture as a differential: identical ids.
- Unit tests pin the key split, the import-path parse, the go.mod module
  parse, the boundary-aware prefix match, and both loader statement
  sequences (anchored and fallback).

Observability Evidence: no new signal. The existing `code call
materialization completed` log already carries
`load_symbol_definitions_duration_seconds`, `symbol_key_count`, and
`symbol_definition_fact_count`. A Go consumer's duration drops from seconds
to producer scale; the count stays exact because the row set is unchanged.

Concurrency: none needed. All three statements are plain reads. The change
touches no lease, claim, queue, lock, or write path.

NOT_CHECKED:

- Post-deploy loader times for the Go consumers in the reducer log.
- Cold-cache timing of the anchored scan and the go.mod manifest read.
- A Go consumer whose producers hold tens of thousands of files; the cost
  then follows the producer size as with the package-key anchor.
