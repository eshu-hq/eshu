# Anchored Symbol-Definition Loader (#7601)

This note records the proof for the first of the two #7601 pull requests: the
loader change. `FactStore.LoadActiveCodeCallSymbolDefinitionFacts` in
`go/internal/storage/postgres` now anchors `package:<package_id>#<export_name>`
keys on their producer scopes. Every other key keeps the corpus-wide scan.

## What changed

The loader splits the requested keys (`splitCodeCallPackageSymbolKeys`).

- **Other keys** (Go `stable_symbol_key`, SCIP symbols, anything without the
  `package:` prefix) run `listActiveCodeCallSymbolDefinitionFactsQuery`. Its
  text is byte-identical to the statement before this change, because it is
  built from the same shared fragments. It runs only when such keys exist.
- **Package keys** first run `listActiveCodeCallPackageManifestsQuery`. That
  statement reads the stored `package.json` manifests in `content_files` for
  repository scopes that have an active generation. Go parses each manifest
  name (`codeCallPackageManifestName`), and the loader keeps the scopes whose
  manifest names a requested package (`listCodeCallPackageProducerScopeIDs`).
  Then `listAnchoredActiveCodeCallSymbolDefinitionFactsQuery` runs. It is the
  same scan plus `AND fact.scope_id = ANY($5::text[])`, paged with the same
  `(observed_at, fact_id)` keyset and the same page size of 500.
- The manifest read is a `MATERIALIZED` CTE. Inlined, the planner probes
  `content_files` once per repository scope, which measured about 8 times
  slower warm and 37 times slower cold (see Measurements).
- When no producer manifest matches, the definition scan does not run and the
  key stays unresolved.
- The two result sets are merged with duplicates removed by `fact_id`
  (`appendUniqueFactEnvelopes`).

Nothing changes yet for existing keys. Go and SCIP keys take the unchanged
statement. No parser emits `package:` call keys yet, so JavaScript and
TypeScript resolution is also unchanged until the parser pull request (#7601
part 2) lands. The change adds no DDL, no index, no reducer code, and no API,
OpenAPI, or MCP change.

## Edge cases

| Case | Handling | Proof |
| --- | --- | --- |
| Empty or blank key list | Returns nil and issues no query, as before | `TestFactStoreLoadActiveCodeCallSymbolDefinitionFactsSkipsEmptySymbols` |
| Only non-package keys | One unanchored scan, four args, no manifest read | `TestLoadActiveCodeCallSymbolDefinitionFactsKeepsNonPackageKeysUnanchored`, `TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository` |
| Mixed keys | The unanchored scan gets only the non-package keys; the anchored scan gets only the package keys | `TestLoadActiveCodeCallSymbolDefinitionFactsAnchorsPackageKeysOnProducerScopes` |
| No producer manifest | Manifest read only; no definition scan | `TestLoadActiveCodeCallSymbolDefinitionFactsIssuesNoScanWithoutProducer`, `TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsScanWithoutProducer` |
| Malformed key (`package:#X`, `package:foo`) | Every key that starts with `package:` takes the anchored path. One with no `#`, or an empty name on either side, names no package, so it resolves no producer, issues no scan, and stays unresolved. Today the corpus-wide scan would still look it up by literal value. Nothing emits such keys, so this is a latent coupling: a future emitter of `package:` values in another shape must also change `codeCallPackageSymbolKeyPackageName` | `TestLoadActiveCodeCallSymbolDefinitionFactsIssuesNoScanWithoutProducer` |
| Two repositories publish one name | Both scopes load; the reducer's unique-or-unresolved rule keeps the key unresolved | `TestLoadActiveCodeCallSymbolDefinitionFactsKeepsDuplicateNameProducers`, live `AnchorsPackageKeys` |
| Monorepo with nested workspace manifests | Every manifest counts, whatever its directory; each name maps to its repository. The nearest-manifest rule applies when the parser stamps `package_id`, not here | unit `KeepsDuplicateNameProducers`, live `AnchorsPackageKeys` (`packages/format/package.json`) |
| Manifest with no name, a blank name, or a non-string name | Skipped | `TestLoadActiveCodeCallSymbolDefinitionFactsSkipsUnusableManifests` |
| Invalid JSON, or a non-object document | Skipped; the load never fails | unit `SkipsUnusableManifests`, live `AnchorsPackageKeys` (`broken/package.json`) |
| `\u0000` escape in a manifest | Parsed by Go, not cast in SQL, so it cannot abort the statement | unit `SkipsUnusableManifests` |
| Superseded generation | The anchored scan joins the active generation, as before | live `AnchorsPackageKeys` (`fact-logging-stale`) |
| Manifest whose scope has no active generation | Kept out of the producer set by the manifest join | live `AnchorsPackageKeys`: asserts the anchored scan's `$5` array excludes `scope:never-active` (never activated) and `scope:pending-generation` (active pointer on a pending generation), each seeded with a manifest and a fact. With the join removed, `$5` gains both scopes and the test fails. The loaded fact ids stay the same either way, because the definition scan also requires an active generation |
| Non-producer scope carrying the same derived key | Excluded | live `AnchorsPackageKeys` (`fact-vendored`) |
| More than one page | Page 2 carries the cursor, the same limit, and the same `$5` scopes | `TestLoadActiveCodeCallSymbolDefinitionFactsPagesAnchoredScan` |
| A file fact returned by both scans | Kept once | `TestLoadActiveCodeCallSymbolDefinitionFactsDeduplicatesAcrossScans` |

### Named limit: manifests are not generation-tagged

`content_files` holds the latest projected content of each repository and has
no generation column. The loader can enforce two things:

- the manifest's scope has an active generation;
- the definition scan reads only active-generation file facts, and it still
  matches each definition's own `package_id`.

So a manifest that runs ahead of or behind the active generation can only add or
drop a candidate scope. It cannot invent a definition. One case could still
mislead. Take a duplicate-name pair where one repository renamed its package in
a pending or failed generation. Its stored manifest already shows the new name,
but its active facts still carry the old one. The pair then collapses to one
producer, and the key could resolve where the active truth is ambiguous. The
window lasts until that generation activates or the next one succeeds.

This PR emits no package keys, so the case cannot happen yet. It is recorded
for the parser PR and its arbiter review. Reading the manifest from the
active-generation file fact instead would bring back the corpus-wide scan.
`fact_records_file_key_idx` indexes `(payload->>'repo_id',
payload->>'relative_path')` for file facts, so a lookup for one known
repository and path is cheap. The loader does not know the producer
repositories in advance, though, and that index cannot serve a cross-repository
`LIKE '%/package.json'` match. `content_files_relative_path_trgm_idx` can.

## Measurements

No-Regression Evidence: run 2026-10-04 on this branch.

- The unanchored statement is byte-identical to origin/main. The check
  extracted `listActiveCodeCallSymbolDefinitionFactsQuery` from
  `git show origin/main:go/internal/storage/postgres/facts_active_code_call_symbols.go`,
  wrote the branch constant out through a throwaway test, and compared the two
  with `cmp` (exit 0). Go and SCIP keys therefore run the same SQL as before.
- `TestReducerContentionGateActiveCodeCallSymbolLoaderCrossRepository` (a Go
  `scip_symbol` key) is unchanged and passes.
- Live correctness ran on a throwaway `postgres:18` container against the
  bootstrap `ingestion_scopes`, `scope_generations`, `fact_records`, and
  `content_store` DDL:
  `ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/ -run
  '^TestReducerContentionGateActiveCodeCallSymbolLoader' -count=1 -v`.
  - Before the change, simulated by sending every key to the unanchored scan,
    the anchored proof loaded `fact-vendored` and the no-producer proof loaded
    `fact-unpublished`.
  - After the change, both pass with the exact expected fact ids. The recorded
    statements are the manifest read, then the anchored scan. The anchored
    scan's `$5` producer array is exactly the three producer scopes.
  - Mutation: with the manifest statement's `scope_generations` join removed,
    `AnchorsPackageKeys` fails because `$5` also holds `scope:never-active` and
    `scope:pending-generation`. The join was restored after the run.

Performance Evidence: measured 2026-10-04 on the ops-qa read replica
(PostgreSQL 18.3), read-only, `statement_timeout` 10 s (5 s for the old
statement). Each statement was prepared from the shipped constant text. Plain
`EXPLAIN` ran before `EXPLAIN (ANALYZE, BUFFERS)`, with three timed runs per
case and a unique nonce in each run's key array or comment. Replay lag stayed
between 0.01 and 1.0 s, except 4.5 s right after the largest-producer runs.
`confl_snapshot` stayed at 2 throughout.

The consumer is `repository:r_e3c0ae60`, the worst consumer by key count. Its
real keys come from the active-generation `imports` rows: every named import of
a bare, non-subpath specifier, which gives 237 `package:` keys over 106
packages. Those packages resolve to 10 producer scopes with 2,101 active file
facts. That is larger than the 9 scopes and 492 files the investigation sized,
because default and namespace imports are included here.

| Case | Runs (ms) | Plan class |
| --- | --- | --- |
| Manifest read, first draft (inline join) | 757.7 cold (25,401 reads), 125.0, 128.7 | Nested loop over 799 scopes, one `content_files_repo_path_idx` probe each; scope join misestimated at 1 row; 155k buffers |
| Manifest read, shipped (MATERIALIZED CTE) | 20.5, 15.4, 15.1; generic plan 15.0 | One bitmap read on `content_files_relative_path_trgm_idx`, hash join to scopes; 469 rows, 3,497 buffers |
| Anchored definition scan, custom plan | 481.9 (1,985 reads), 380.2, 373.7 | Index scan on `fact_records_scope_generation_idx`, index condition `scope_id = ANY($5)`; 2,101 file facts examined |
| Anchored definition scan, `force_generic_plan` | 373.3, 375.8, 378.1 | Same index and condition |
| Anchored scan, largest producer only (`r_957cd853`, 7,096 file facts) | 2,300.9, 2,109.0, 2,111.4 | Same index and condition |
| Old unanchored statement, same 237 keys | canceled at 5,002.5 | Per-scope loop over every active file fact |

- **The first draft missed the budget.** The manifest read began as a plain
  join, and the planner chose the per-scope probe. Warm, the manifest read and
  the anchored scan totaled about 500 to 510 ms; cold, about 1.24 s.
- **The fix was measured before it shipped.** As a `MATERIALIZED` CTE, the same
  469 rows come back in 15 to 21 ms. Row sets match: shipped minus CTE, with
  `EXCEPT ALL`, is 0 rows, and both return 469. Without `MATERIALIZED`, the
  planner inlines the CTE back into the slow plan, so the marker is
  load-bearing. A unit test pins it.
- **Go parsing is small.** Reading names from 469 synthetic manifests of about
  2.4 KB each took 1.27 ms per pass in a local microbenchmark (Apple M-series,
  three runs of 50 iterations). The replica population is 469 manifests,
  859,635 bytes in total, 14,353 bytes at most.
- **Worst consumer, end to end:** manifest read plus anchored scan is 388 to
  403 ms warm, with both plan modes inside that range. The one cold first run
  was 502 ms: 20.5 ms plus 481.9 ms, with 1,985 disk reads.
- **Old statement:** it cannot finish inside 5 s. The per-file cost measured
  here is 0.18 ms (warm, 2,101 files) to 0.30 ms (7,096 files). Extrapolated,
  not measured, over the about 145,810 active files the investigation reported,
  that is about 26 to 44 s per call.

Budget: the loader sub-step must stay at or under 500 ms on the worst consumer.

- It holds warm (388 to 403 ms).
- The single cold first touch came to 502 ms, 2 ms over.
- It does not hold for a consumer that depends on a producer the size of
  `r_957cd853`: about 2.1 s. That repository publishes WordPress theme and
  plugin package names. Whether any consumer imports them is NOT_CHECKED,
  because answering it needs a corpus-wide `imports` scan.

Observability Evidence: no new signal. The existing `code call materialization
completed` log already carries `load_symbol_definitions_duration_seconds`,
`symbol_key_count`, and `symbol_definition_fact_count`
(`logCodeCallMaterializationCompleted` in
`go/internal/reducer/code/call/materialization`). Those fields show the sub-step
cost at 3 AM, and the loader signature is unchanged.

Concurrency: none needed. Both statements are plain reads. The change touches
no lease, claim, queue, lock, or write path.
