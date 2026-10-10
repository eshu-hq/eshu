# Anchored Symbol-Definition Loader (#7601)

This note records the proof for the first of the two #7601 pull requests: the
loader change. `FactStore.LoadActiveCodeCallSymbolDefinitionFacts` in
`go/internal/storage/postgres` now anchors `package:<package_id>#<export_name>`
keys on their producer scopes. Every other key keeps the corpus-wide scan.

`repo-A` and `repo-S` in this note are stable one-to-one placeholders for the
measured repository ids; the mapping is held outside the repository.

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
| Monorepo with nested workspace manifests | Every manifest counts, whatever its directory, except one under a path segment that begins with `node_modules` (see "Vendored manifests" below); each name maps to its repository. The nearest-manifest rule applies when the parser stamps `package_id`, not here | unit `KeepsDuplicateNameProducers`, live `AnchorsPackageKeys` (`packages/format/package.json`) |
| Manifest under a vendored `node_modules`-prefixed directory | Ignored; it is not a producer | live `IgnoresVendoredManifests`, `SkipsScanForVendoredOnlyPackage` |
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

Performance Evidence: measured 2026-10-04 on the QA read replica
(PostgreSQL 18.3), read-only, `statement_timeout` 10 s (5 s for the old
statement). Each statement was prepared from the shipped constant text. Plain
`EXPLAIN` ran before `EXPLAIN (ANALYZE, BUFFERS)`, with three timed runs per
case and a unique nonce in each run's key array or comment. Replay lag stayed
between 0.01 and 1.0 s, except 4.5 s right after the largest-producer runs.
`confl_snapshot` stayed at 2 throughout.

The consumer is `repository:repo-S`, the worst consumer by key count. Its
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
| Anchored scan, largest producer only (`repo-A`, 7,096 file facts) | 2,300.9, 2,109.0, 2,111.4 | Same index and condition |
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
  396 ms warm, with both plan modes inside that range. The one cold first run
  was 502 ms: 20.5 ms plus 481.9 ms, with 1,985 disk reads.
- **Old statement:** it cannot finish inside 5 s. The per-file cost measured
  here is 0.18 ms (warm, 2,101 files) to 0.30 ms (7,096 files). Extrapolated,
  not measured, over the about 145,810 active files the investigation reported,
  that is about 26 to 44 s per call.

Budget: the loader sub-step must stay at or under 500 ms on the worst consumer.

- It holds warm (388 to 396 ms).
- The single cold first touch came to 502 ms, 2 ms over.
- It does not hold for a consumer that depends on a producer the size of
  `repo-A`: about 2.1 s. That repository publishes WordPress theme and
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

## Vendored manifests

A repository can commit a backup of `node_modules` under a renamed directory,
such as `node_modules.bak/`. Discovery already prunes the exact name
`node_modules`, so the fleet has 470 stored `package.json` manifests and none
sits under an exact `node_modules/` segment. Seven sit under a segment that
begins with `node_modules`. One repository holds all seven: async, faketoe,
lodash, mysql, request, xml2js, and yargs.

The loader counted those as package publishers. Every consumer that imports one
of those names then anchored a scan of that repository's roughly 1,708 active
file facts, and a vendored ESM copy could have produced false edges. A vendored
copy is not the publisher of the package.

The fix changes one predicate in `listActiveCodeCallPackageManifestsQuery`. The
manifest read now ignores any manifest under a path segment that begins with
`node_modules`, case-insensitive, anchored at a segment boundary:

```sql
WHERE (relative_path = 'package.json' OR relative_path LIKE '%/package.json')
  AND relative_path !~* '(^|/)node_modules[^/]*/'
```

The `(^|/)` anchor keeps lookalikes such as `my_node_modules/x/package.json`
as producers. A real nested workspace manifest such as
`packages/format/package.json` is also still a producer. The change adds no
DDL, no index, and no API, OpenAPI, or MCP change. The Go and SCIP definition
statement `listActiveCodeCallSymbolDefinitionFactsQuery` is untouched.

Correctness proof (live, throwaway `postgres:18` container, run with
`ESHU_POSTGRES_DSN` set to it):
`go test ./internal/storage/postgres/ -run 'Vendored|^TestReducerContentionGateActiveCodeCallSymbolLoader' -count=1 -v`.

- Before the predicate, `IgnoresVendoredManifests` loaded `fact-backup-lodash`
  and `fact-backup-async` next to the two control facts, and
  `SkipsScanForVendoredOnlyPackage` loaded `fact-backup-lodash`.
- After the predicate, only the control facts load, the anchored scan's `$5`
  producer array is exactly `scope:control` and `scope:control-prefixed` (one
  scope per control manifest, so each manifest is tested alone), and a request
  whose only package is vendored runs the manifest read and no definition scan.
- The case variant `Node_Modules-old/async/package.json` is excluded, and
  `my_node_modules/x/package.json` stays a producer.

Performance Evidence: measured 2026-10-05 on the QA read replica
(PostgreSQL 18.3), read-only, three interleaved `EXPLAIN ANALYZE` pairs of the
shipped manifest CTE and the CTE with the predicate.

| Statement | Runs (ms) | Rows | Plan |
| --- | --- | --- | --- |
| Shipped manifest CTE | 24.5, 17.7, 17.7 | 470 | `BitmapOr` on `content_files_relative_path_trgm_idx`, bitmap heap, hash join to `ingestion_scopes`, `scope_generations_active_scope_idx` |
| With the predicate | 18.4, 18.2, 18.1 | 463 | Same plan class. The new line is `Filter: (relative_path !~* '(^|/)node_modules[^/]*/')`, with Rows Removed by Filter 7 |

The predicate costs nothing measurable: the manifest read stays near 18 ms and
the plan class does not change. The seven removed rows are the vendored
manifests.

The scan the change avoids is the anchored definition scan for the vendored
scope alone: 950.7 ms cold, then 246.1 ms and 225.0 ms warm. Every consumer of
one of those seven names paid that on each load.

Observability Evidence: no new signal. The existing `code call materialization
completed` log fields `load_symbol_definitions_duration_seconds` and
`symbol_definition_fact_count` show the effect: a consumer of a vendored name
stops paying the extra scan and loads fewer definition facts.

Concurrency: none needed. The statement is a plain read and touches no lease,
claim, queue, lock, or write path.

NOT_CHECKED:

- Post-deploy numbers on the consumers of the vendored names. Before this
  change, three consumers measured 0.35 to 0.56 s (worst 0.559 s against the
  0.5 s line) in the reducer's `code call materialization completed` log
  lines on the shared QA environment, build `sha-5d77d69`. The same lines are
  the proof after the change.
- Primary-side cache state. The replica numbers above are the only timings.

## Reading parsed_file_data once

Layer: after the anchor, what was left in the scan was CPU in the per-file
match, not I/O and not the plan. The match reads each file's
`parsed_file_data` ten times (a type check and a value read for each of five
definition arrays). The value is large and stored compressed out of line, so
each read detoasts it again. That cost about 0.27 ms per producer file fact. A
consumer whose producers hold 1,367 files took 366 ms.

The change extracts the value once in a `LATERAL` subquery in the shared query
head, and the match reads it from there. The match is otherwise unchanged, so
the returned rows are exactly the old ones and the key comparison keeps its
hashed `= ANY($1::text[])` lookup. `OFFSET 0` is the fence that keeps the
planner from folding the subquery back into its callers; without it the scan
took 801 and 858 ms against 140 and 121 ms.

Rejected: one `jsonb_path_exists` per file with the keys passed as a jsonb
document. On the replica it beat the old scan by 1.5 to 2.3 times on the
heaviest consumer and 4.7 times on the median, but it was 3.2 times slower than
this change on the heaviest consumer, 5.8 times on the synthetic 7,096-file
producer, and 1.1 times on the median. Its cost grows with the number of
requested keys: 151 keys took 391 ms and 1,000 keys took 1,960 ms on the same
files, where the old scan and this change stay flat (16 times slower than this
change at 1,000 keys). By how jsonpath compares values, it also cannot match a
key field that is not a string, which the old text comparison does, and it
matched package ids and export names as two sets, so it returned extra rows for
package keys. A review on a real Postgres reproduced both.

Performance Evidence: measured 2026-10-07 on the QA read replica
(PostgreSQL 18.3), read-only. Each statement was prepared with real parameters
and timed with `EXPLAIN (ANALYZE, TIMING OFF, BUFFERS)`, in interleaved rounds,
warm. "Old" is the statement on `main` at `1277f0429`.

| Consumer | Keys | Producer files | Old (ms) | This change (ms) | Rows |
| --- | --- | --- | --- | --- | --- |
| Heaviest real (151 package keys, 16 producer scopes) | 151 | 2,893 | 900, 622, 633, 866 | 122, 140, 121, 123 | 9, same set |
| Median | 29 | 119 | 35.4 | 6.8 | 11, same set |
| Most keys | 240 | 263 | 117, 104, 107 | 18, 25, 18 | 0 |
| Heaviest producers, 1,000 keys | 1,000 | 2,893 | 853, 625, 642 | 122, 139, 121 | 9, same set |
| Synthetic 7,096-file producer | 151 | 7,096 | 2,278, 2,129, 2,145 | 354, 410, 358 | 0 |

The generic plan (`= ANY ($1`) gave the same numbers, so the gain does not
depend on the plan the driver picks. Reading buffers fell from about 129,000 to
19,600 on the heaviest consumer. Just touching `payload->'parsed_file_data'`
costs 118 to 122 ms on those 2,893 files and 311 ms on the 7,096, so this
change sits at the detoast floor: matching adds 0 to 25 ms on the real heaviest
consumer.

The 92 anchored consumers have 1 to 240 package keys (median 24.5) and 6 to
2,893 producer files (median 112). No consumer names the 7,096-file producer
today, so that row is a labelled synthetic case. With the manifest read, the
heaviest real consumer loads its definitions in about 180 to 200 ms.

The corpus-wide scan for Go and SCIP keys (#7623) shares this match. One
consumer with 115 keys took 29.2 to 31.5 s before and 5.6 to 6.3 s after (two
single samples, three rows returned in each form). That is still far
from 500 ms: it needs a producer anchor, not a cheaper match.

Correctness proof: a real-Postgres differential against the frozen old
predicate over every key field, all five arrays, both package-pair spellings,
number-valued key fields and malformed payloads. A second real-Postgres test
reads the plan with `EXPLAIN (VERBOSE)` and fails when any of the five
`jsonb_array_elements` calls reads `fact.payload`. Four scratch mutants each
failed a test: dropping `OFFSET 0`, reading `fact.payload` directly for one
array, emptying the `classes` array, and dropping the `symbol` field.

Observability Evidence: no new signal. The existing `code call materialization
completed` log fields `load_symbol_definitions_duration_seconds` and
`symbol_definition_fact_count` show the effect. The count stays exact because
the row set is unchanged.

Concurrency: none needed. The statement is a plain read and touches no lease,
claim, queue, lock, or write path.

NOT_CHECKED:

- Post-deploy loader times in the reducer log. Capture them after the build is
  pinned on the QA environment.
- Cold-cache timing of this change. The statements ran in rotating order, and
  only the first round started cold, so every figure here is warm.
- The producer-manifest read costs 57 to 59 ms now (a nested loop of 802 passes
  over the scope join). A `LATERAL ... LIMIT 1` probe of `scope_generations`
  measured 18 to 20 ms with the same 464 rows. It is not part of this change.
- If a producer of about 7,000 files ever has a consumer, the next step below
  the detoast floor is `ALTER TABLE fact_records ALTER COLUMN payload SET
  COMPRESSION lz4`. Its gain is unmeasured.
