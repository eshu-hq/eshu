# Go Module-Anchored Symbol-Definition Loader (#7623)

`FactStore.LoadActiveCodeCallSymbolDefinitionFacts` in
`go/internal/storage/postgres` now anchors `scip-go gomod <import_path> ...`
keys on the scopes whose stored go.mod manifests declare that import path's
module path or a prefix of it. Only those scopes' active file facts are scanned. This answers the
issue's open question affirmatively: the module path in the key resolves the
producer scopes. On consistent data the anchored answer equals the corpus-wide
answer for every layout the parser produces; the named limits below are the
places it can be narrower.

## What changed

The loader splits the requested keys three ways with
`producerstore.Split` (package `go/internal/storage/postgres/code/producers`)
and runs one scan per kind.

- **Go keys** read the stored go.mod manifests in `content_files` for
  repository scopes that have an active generation
  (`producerstore.GoModuleManifestsQuery`, the same MATERIALIZED-CTE shape as
  the package.json read). `producerstore.GoModuleName` takes the module path
  with the parser's own rule, and the scopes whose module path equals the key's
  import path or one of its `/`-prefixes are the producers. Then the existing
  anchored scan runs with those scopes as `$5`.
- A Go key whose module no stored go.mod declares (the standard library, a
  dependency outside the corpus) issues **no definition scan** and stays
  unresolved, like a package key with no producer. An earlier revision fell back
  to the corpus-wide scan in that case; it was removed because the fallback is
  all-or-nothing per load, so a consumer with only standard library keys kept
  paying the full scan for an empty answer.
- A `scip-go gomod` key needs a symbol after the import path to count as a Go
  key. The parser always emits one, so a key without it keeps the corpus-wide
  scan with the other keys.
- **Package keys** are unchanged. **All other keys** (SCIP symbols from other
  indexers) keep the corpus-wide scan.
- The producer lookups moved out of the `postgres` package into
  `code/producers` so the package's non-test file count and the dirgate pin stay
  as they were.

The manifest read excludes no path. Discovery already prunes `vendor/` trees, and
the parser stamps the nearest go.mod of any package it parses, so every stored
go.mod is a candidate producer. A module declared by several repositories
returns every one of them, so the reducer sees each candidate definition and
keeps the key unresolved.

The change adds no DDL, no index, no reducer code, and no API, OpenAPI, or MCP
change.

## Why the rule is exact

The parser builds a definition's import path as its nearest go.mod module path
plus the package directory
(`go/internal/parser/go_package_module_import_path.go`), and emits the key from
it (`go/internal/parser/golang/scip_symbols.go`); it emits none without a go.mod.
So every native definition key is prefixed by the module directive of a go.mod
in the defining repository, whichever layout it has: nested modules, `/v2`
modules, a `v2/` directory, test packages, `internal/` packages and replace
directives all follow. The loader reads the module line the way the parser does
(`producerstore.GoModuleName` calls the parser's own line-ending normalization;
`TestGoModuleNameMatchesTheImportPathTheParserStamps` writes each of 17 go.mod
inputs next to a one-file Go package, takes the import path the parser's public
pre-scan stamps on it, and requires the loader's module to match, including
a bare CR, a stray CR beside LF, a trailing comment, a quoted path and a module
block).

## Edge cases

| Case | Handling | Proof |
| --- | --- | --- |
| Go key, producer declared | go.mod read, then the anchored scan with exactly the producer scopes | Live `GoAnchorEqualsCorpusScan` |
| Fork declares the same module | Both scopes load; the reducer's unique-or-unresolved rule applies | Same test (`scope:fork-a`, `scope:fork-b`) |
| Nested module (`sub/go.mod`) | Parent-module scopes are prefix candidates too | Same test (`scope:mono`) |
| `/v2` module and a `v2/` directory of the root module | Both are producers for keys under their paths | Same test (`scope:lib-v2`, `fact-lib-v2-dir`) |
| Copy under `third_party` with its own go.mod | A producer like any other module | Same test (`scope:third`) |
| Lookalike module (`libext` vs `lib`) | No path boundary, not a candidate | Unit `TestGoModuleCandidates`, `$5` assertion |
| Standard library or unknown module | go.mod read only, no definition scan | Live `GoAnchorStdlibKeysIssueNoDefinitionScan`, unit `...IssuesNoScanForGoKeyWithoutModuleProducer` |
| `scip-go gomod <path>` with no symbol | Not a Go key; corpus-wide scan | Unit `TestSplit`, `...KeepsNonPackageKeysUnanchored` |
| Malformed go.mod | Skipped; the load never fails | Unit `TestGoModuleName` |
| Scope whose generation is not active | Kept out of `$5` by the manifest join | Live `GoAnchorEqualsCorpusScan` (`scope:frozen`) |
| Superseded generation | The anchored scan joins the active generation | Same test (`fact-lib-stale`) |
| Package, Go and other keys together | Three scans; a fact matching two is returned once | Live `ThreeLegPartitionDedupes` |
| More than 500 rows | Each page re-runs only the producer-sized scan | Live `GoAnchorPagesPast500Rows` |

### Named limits

- **A definition whose go.mod is not stored is not found.** The anchor reads
  go.mod from `content_files`; the parser reads it from disk. An ignore rule
  (`.eshuignore`, `ignored_path_globs`, an ignored extension) can drop go.mod
  from the content store while the `.go` files next to it are still parsed.
  `content_files` also has no generation column, so it can run ahead of or
  behind the active generation: a renamed module in a pending generation can
  drop a scope whose active facts still carry the old keys until the generation
  activates. In both cases the anchor loads fewer definitions than the
  corpus-wide scan would have. Usually the key is then unresolved. If the
  missing scope was one of several declaring the same module, the remaining
  candidate can be the only one the reducer sees, and its unique-or-unresolved
  rule (`uniqueCodeCallSymbolCandidates`) then resolves the key to it, an edge the
  corpus-wide scan would have withheld. This is the window the package-key anchor
  already accepts. The standard library case is the same narrowing by design.
- **Keys from other indexers** keep the corpus-wide scan; none appears in the QA
  corpus today. Keys written by the real `scip-go` indexer, which carry the
  module and a version (`scip-go gomod <module> <version> <descriptor>`), are
  classified as Go keys and anchored on the module token. That token is a go.mod
  module path, so the declaring repository is still a candidate, but that rests
  on the indexer's output, not on the parser-parity argument above, and no
  fixture covers a version-bearing key beyond the classification test.

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

The paging claim in the issue ("inferred, not measured") is measured: before
this change, a 1,200-row load issued the manifest read plus three full corpus
scans, one per 500-row page. The anchor does not change paging; it shrinks each
page's scan to the producer scopes (`GoAnchorPagesPast500Rows`).

### The shared QA replica (read-only, 2026-10-09)

The shim above is synthetic. The same statements were run on the ops-qa read
replica (PostgreSQL 18.3), `EXPLAIN (ANALYZE, TIMING OFF)`, for every
repository in the QA corpus that has Go call keys (the corpus has 7 scopes with
Go files and 8 stored go.mod rows). The three loads that were slow in the
reducer log after #7697 (6.43, 6.45 and 6.87 s, every key a `scip-go gomod` key)
are consumers A to C.

| Consumer | Go keys | Corpus-wide scan | Producer scopes | Producer file facts | Anchored scan |
| --- | --- | --- | --- | --- | --- |
| A | 17 | 6.7 s | 1 | 7 | 4.5 to 4.9 ms |
| B | 27 | 6.8 s | 1 | 55 | 7.3 to 7.6 ms |
| C | 88 | 6.5 s | 1 | 53 | 10.0 to 10.4 ms |
| D | 115 | 6.6 s | 1 | 27 | 6.2 to 6.6 ms |
| E | 33 | 6.5 s | 1 | 10 | 4.8 to 5.0 ms |
| F | 41 | 6.5 s | 1 | 20 | 5.9 to 6.3 ms |
| G | 20 | 6.5 s, no rows | 0 | none | no statement issued |

In every case the scopes the corpus-wide statement returned were contained in
the go.mod-anchored producer scopes (six equal to the consumer's own repository,
one empty on both sides). The go.mod manifest read took 10.4 ms for 8 rows
through `content_files_relative_path_trgm_idx` (1,210 buffers; 13.9 ms on the
first run). No Go repository in the QA corpus imports another, so the
cross-repository case is proven by the fixtures, not by this data.

Performance Evidence: on the synthetic shim the anchored scan touches about 45
times fewer buffers than the corpus scan and returns the identical row set. On
the QA replica a Go load goes from 6.5 to 6.8 s to about 15 to 20 ms (the
manifest read plus the anchored scan). A consumer whose Go keys no stored go.mod
declares goes from the full scan to the manifest read alone.

## Correctness proof

- Live on throwaway `postgres:18` with the bootstrap DDL:
  `go test ./internal/storage/postgres -run
  'TestReducerContentionGateActiveCodeCallSymbol' -count=1`. The differential
  `GoAnchorEqualsCorpusScan` compares the anchored load with the shipped
  corpus-wide statement (`listActiveCodeCallSymbolDefinitionFactsQuery`, not a
  copy) over a module root, a library used from another repository, a fork, a
  nested `sub/go.mod`, a `/v2` module and a `v2/` directory, a `third_party`
  copy, a superseded generation, an inactive scope, a lookalike module, a
  standard library key and an unknown key, and asserts the exact `$5` scope array.
- `GoAnchorStdlibKeysIssueNoDefinitionScan` asserts the go.mod read and no
  definition scan, and that the corpus-wide scan would have found a definition
  the anchor does not look for. `ThreeLegPartitionDedupes` and
  `GoAnchorPagesPast500Rows` pin the partition and the paging.
- `TestGoModuleNameMatchesTheImportPathTheParserStamps` (in the producers
  package) drives the parser's public pre-scan with 17 go.mod inputs and
  requires the module it implies to equal the loader's `GoModuleName`. The review
  that found the stray-CR disagreement is why the loader now calls the parser's
  own line-ending normalization.
- Seeded violations, each turning a test red: put the corpus-wide fallback back;
  drop the `scope_generations` join from the go.mod read; drop
  `LIKE '%/go.mod'`; drop the `/` boundary from the candidates; accept a
  `scip-go gomod` key with no symbol as a Go key; always fold `\r` in
  `GoModuleName`.

Observability Evidence: the loader logs one `code call symbol definition load`
Info line per load, on failure too, with `outcome` (`ok` or `error`, plus the
`error` text), `other_key_count`, `package_key_count`, `go_key_count`,
`package_producer_scope_count`, `go_producer_scope_count`,
`other_scan_duration_seconds`, `package_leg_duration_seconds` and
`go_leg_duration_seconds`. It has no scope id, so correlate it with the
`code call materialization completed` line of the same consumer by adjacency.
A producer scope count of zero beside a non-zero key count means no stored
manifest names a producer, so that leg issued no definition scan and its keys
stay unresolved. The existing `load_symbol_definitions_duration_seconds` and
`symbol_definition_fact_count` still show the effect; the definition count can
be lower than before for a Go consumer inside the named limits above.

Concurrency: none needed. All three statements are plain reads. The change
touches no lease, claim, queue, lock, or write path.

NOT_CHECKED:

- Post-deploy loader times for the Go consumers in the reducer log.
- Cold-cache timing of the anchored scan and the go.mod manifest read.
- A Go consumer whose producers hold tens of thousands of files; the cost
  then follows the producer size as with the package-key anchor.
- The go.mod read cost at corpus scale. It reads every stored go.mod's content on
  every load that has Go keys, so it grows with the number of go.mod files; it was
  measured at 8 rows (replica) and 100 rows (shim), far below the corpus-wide
  scan it replaces.
