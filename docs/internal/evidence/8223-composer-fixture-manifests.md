# Composer Fixture Manifests Are Not Producers (#8223)

A `composer.json` under a tests, fixtures, testdata, or examples directory can
carry a real package name and a psr-4 map. It must not publish that package.

## Rule

`composer.FixtureDirPattern` is `(^|/)(tests|fixtures|testdata|examples)/`. It
matches whole directory segments in any letter case, on a repository-relative
slash path. `packages/testing` and `packages/attestation` do not match.

Two places apply the one constant:

- `ComposerManifestsQuery` (loader anchor) filters with
  `relative_path !~* '<pattern>'`. The SQL is built from the constant.
- `composer.ProducerPackage` (parser stamp) stops the nearest-manifest walk at a
  fixture manifest and returns no key. It does not fall through to an outer
  manifest, so a fixture class never inherits the root package name.

Both sides must agree. If only the anchor excluded fixtures, a repository with a
real root manifest and a fixture manifest of the same name would still stamp
both classes with one key.

## Performance Evidence

Performance Evidence: baseline is the composer manifest read without the
fixture predicate; after is the same read with it. Backend is the PostgreSQL
streaming replica of the QA environment (version as deployed there). Input
shape is `content_files` at about 149,375 rows with 94 composer manifests
(counts only). Terminal row counts are 94 before and 93 after. The read is
unchanged in plan class: one bitmap scan, no new index, no queue, lease, or
write path touched.

Read-only SELECT on the QA environment read replica, `content_files` at about 149,375 rows
(`reltuples`). The manifest read, before and after, `EXPLAIN (ANALYZE,
BUFFERS)`:

| Shape | Plan | Rows | Buffers | Execution |
| --- | --- | --- | --- | --- |
| before (`vendor/` only) | Bitmap Heap Scan over `content_files_relative_path_trgm_idx`, BitmapOr of two index scans | 94 | shared hit 1777 | 22.573 ms |
| after (`vendor/` and fixture) | same plan, same two index scans | 93 | shared hit 1777 | 22.861 ms |

The fixture predicate is a heap Filter on the 94 rows the index already
returned (`Rows Removed by Filter: 1`). The plan, the buffers, and the index
scans are unchanged, so no index is added. Of the 94 composer manifests, 1
sits under a fixture-style path. The `vendor/` count is 0, as discovery prunes
it.

No-Observability-Change: the loader still counts every row the read returns in
`eshu_dp_producer_manifest_tag_outcomes_total` by `kind=composer` and
`outcome`. The excluded row never reaches the read, so the counter sees one
fewer row per fixture manifest. No metric, span, log key, or status field is
added or renamed. An operator sees the same signals as before.

## Decision: package.json

Not changed here. The package.json leg has a different stamp rule (the
JavaScript project scope, nearest `package.json`) and a different prune
(`node_modules*`), so the same exclusion needs its own parser change and proof.
Measured exposure on the same replica: 4 of 470 non-`node_modules`
package.json rows sit under a fixture-style path. A follow-up issue owns it.

## Content version

`gitGenerationContentVersion` moves from `git-collector.v4` to
`git-collector.v5`. The parser now stamps no `package_id` on a class under a
fixture manifest, which changes the stored output for unchanged PHP source, and
the store keeps an equal hint and commit as a duplicate. The pin fixture
(`testdata/content_version/php-lib/`) holds a real root manifest with one class
and a same-named manifest with one class under `tests/fixtures/app/`. Seeded
RED: with the fixture grown and the constant still at v4, the pin test fails
("252 facts ... pinned 238 facts ... gitGenerationContentVersion is still
git-collector.v4"). After the bump and `-update-content-version-pin`, the pin
test passes. In the fixture the root class carries `package_id` `acme/fixture-lib`
and the fixture class carries none.

## Tests

- `go/internal/parser/php/composer/fixture_test.go`: segment matching (case and
  substring cases), `ProducerPackage` across the directory table, and the
  no-fall-through walk with a real root manifest.
- `TestComposerManifestsQueryShape` pins that the SQL embeds the shared
  constant.

NOT_CHECKED: a live Postgres differential of the new predicate (needs the
Docker gate; the predicate was run read-only on the replica above).
