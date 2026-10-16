# Package.json Fixture Manifests Are Not Producers (#8244)

A `package.json` under a test, tests, fixtures, testdata, or examples directory
can carry a real package name. It must not publish that package. This is the
JavaScript twin of
[8223-composer-fixture-manifests.md](8223-composer-fixture-manifests.md); read
that note for the pattern's dialect rationale.

## Rule

`shared.FixtureDirPattern` (package `internal/parser/shared`) matches whole
directory segments named `test`, `tests`, `fixtures`, `testdata`, or `examples`
on a repository-relative slash path, in any ASCII letter case:

```
(^|/)([Tt][Ee][Ss][Tt][Ss]?|[Ff][Ii][Xx][Tt][Uu][Rr][Ee][Ss]|[Tt][Ee][Ss][Tt][Dd][Aa][Tt][Aa]|[Ee][Xx][Aa][Mm][Pp][Ll][Ee][Ss])/
```

The constant moved from the PHP composer package to the shared parser package
so one string serves both ecosystems. `composer.FixtureDirPattern` is now an
alias of it, so the composer anchor and the PHP parser are unchanged in
behavior. A lockstep test pins that the two constants are equal and that both
SQL anchors embed it.

Two places apply it for package.json:

- `PackageManifestsQuery` (loader anchor) filters with
  `relative_path !~ '<pattern>'`, built from the constant. The `node_modules`
  exclusion (`!~*`, segment prefix) is unchanged.
- `project.ProducerPackageName` (parser stamp, read for `package_id` and
  `export_name`) returns no name when the nearest manifest sits under a
  fixture segment, relative to the repository root. It does not fall through to
  an outer manifest, so a fixture class never inherits the root package name.

`node_package_name` is intentionally ungated. `project.NearestPackageName`
still returns the nearest manifest's name for a file under a fixture manifest.
That field records which package owns the file, not that the package is
published. Two consumers read it: the reducer's same-repository workspace
fallback (`nodePackageNamesByRepo`, #7610) and the `source_package` stamp on
`IMPORTS` edges for `file_import_cycles` (#7348). Both are placement questions
that are accurate for a fixture package, and neither is a producer claim, so
gating them would silently change same-repository fallback and cycle collapsing
for fixture files. The parser reads both names from one walk
(`project.PackageNames`) and uses the gated one only for `package_id` and
`export_name`.

Both sides must agree. If only the anchor excluded fixtures, a repository with a
real root manifest and a same-named fixture manifest would still stamp both
classes with one key.

## Path set and limit

Admission criterion: the segment set is the one #8223 admitted. A name stays in
the set when it is a scaffolding convention under which a published manifest is
implausible and a copied real manifest is plausible. No JavaScript-only name is
added: `__tests__`, `spec`, `e2e`, and similar names are not in the set, because
each is an unmeasured false-negative surface until a corpus census shows a
manifest under it.

Limit, accepted: a real package whose `package.json` sits under one of these
segments is no longer a producer, so calls into it stay unresolved. It is a
missed edge, never a false one. The rule is on the manifest location, not the
source file: a root manifest still keys a source file under a tests directory
when no nearer manifest exists. A consumer's declared dependencies are not
affected.

## Exposure measured

Read-only counts on the QA replica (counts only, no repository names or ids),
`content_files` at about 126,204 rows:

- 462 stored package.json manifests outside `node_modules*`.
- 5 of them sit under a fixture-style path, all 5 declare a name, and 1 of those
  5 names equals the name of a manifest outside the fixture set. The name test
  is a regular-expression read of the first `"name"` key, so the collision count
  is approximate; the 5 and 462 are exact.

These counts differ from the 4 of 470 recorded in #8223 and the issue because
the stored corpus changed between the two measurements (462 rows now against
470 then); the numbers are two readings of a moving corpus, not a discrepancy
in the filter.

So the exposure is real but small: one name could bind a consumer import to a
fixture manifest's repository (or make two repositories look like producers of
one name) before this change.

## Performance Evidence

Performance Evidence: baseline is the package.json manifest read without the
fixture predicate; after is the same read with `!~` and the shared pattern.
Backend is the PostgreSQL streaming replica of the QA environment (version as
deployed there). Input shape is `content_files` at about 126,204 rows with 462
package.json rows outside `node_modules*` (counts only). Terminal row counts are
462 before and 457 after. The read is unchanged in plan class: one Bitmap Heap
Scan over a BitmapOr of two index scans, no new index, no queue, lease, or
write path touched.

Read-only `EXPLAIN (ANALYZE, BUFFERS)` of the manifest read, three warm runs
each, before and after:

| Shape | Plan | Rows | Buffers | Execution |
| --- | --- | --- | --- | --- |
| before (`node_modules*` only) | Bitmap Heap Scan, BitmapOr of `content_files_repo_path_pattern_idx` and `content_files_relative_path_trgm_idx` | 462 | shared hit 2461 | 14.42-14.56 ms |
| after (`node_modules*` and fixture, final pattern) | same plan, same two index scans | 457 | shared hit 2461 | 14.68-14.86 ms |

The fixture predicate is a heap Filter on the rows the index already returned
(`Rows Removed by Filter` 7 before, 12 after). The plan, the index scans, and the
buffers are unchanged, so no index is added. The after run is about 0.3 ms
(2%) slower on a 14.5 ms read that runs once per package-keyed load and is
bounded by the same manifest count; the regex runs on 462 returned rows, not on
`content_files`. The manifest read stays a MATERIALIZED CTE.

Correctness proof on real PostgreSQL 18.6 (scratch container, removed
afterwards): the live loader test seeds a real manifest next to a same-named
fixture manifest, six scaffold-only repositories, and a `packages/testing`
look-alike. It fails on the unfixed predicate (the five fixture-only scopes
load) and passes with it. A second live test evaluates the one constant with
PostgreSQL `~` and with Go on 29 paths, including the long s (U+017F) cases
where a flag-based pattern disagrees, and the results are equal on every path.

No-Observability-Change: the loader still counts every row the read returns in
`eshu_dp_producer_manifest_tag_outcomes_total` by `kind=npm` and `outcome`. The
excluded row never reaches the read, so the counter sees one fewer row per
fixture manifest. No metric, span, log key, or status field is added or
renamed. An operator sees the same signals as before.

## Content version

`gitGenerationContentVersion` moves from `git-collector.v5` to
`git-collector.v6`. The parser now stamps no `package_id` on a class under a
fixture manifest, which changes stored output for unchanged JavaScript source,
and the store keeps an equal hint and commit as a duplicate. The pin fixture (the
`js-lib` directory of the content-version pin fixture in
`go/internal/collector/repo/git`) holds a real root manifest with a class and
a function, and a same-named manifest with a class and a function in a nested
`app` directory below a tests and fixtures path.

Seeded RED: with the fixture grown and the constant still at v5, the pin test
fails (266 facts against 252 pinned, "gitGenerationContentVersion is still
git-collector.v5"). After the bump and `-update-content-version-pin` it passes.
Reverting only the parser change in a throwaway worktree (fixture, SQL, and
version kept) leaves the fact count at 266 and moves the digest, so the pin
isolates the parser change.

## Tests

- `go/internal/parser/shared/fixture_dir_test.go`: segment, case, substring, and
  long-s table.
- `go/internal/parser/javascript/project/package_name_fixture_test.go`:
  `ProducerPackageName` across a directory table, the no-fall-through walk, the
  manifest-location limit, and that `NearestPackageName` stays ungated.
- `go/internal/parser/javascript/engine_package_fixture_manifest_test.go`: the
  real engine stamps no `package_id` or `export_name` under a fixture manifest
  and keeps `node_package_name`.
- `TestPackageManifestsQueryShape` and
  `TestFixtureDirPatternIsOneConstantAcrossEcosystems` (producers package): the
  SQL embeds the shared constant with `!~`, and the composer and package.json
  anchors use one constant.
- Live (`ESHU_POSTGRES_DSN`): `TestReducerContentionGateActiveCodeCallSymbolLoaderSkipsFixturePackageManifests`
  and `TestFixtureDirPatternAgreesBetweenPostgresAndGo`.
