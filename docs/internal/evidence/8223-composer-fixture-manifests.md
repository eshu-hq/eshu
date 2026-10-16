# Composer Fixture Manifests Are Not Producers (#8223)

A `composer.json` under a test, tests, fixtures, testdata, or examples directory can
carry a real package name and a psr-4 map. It must not publish that package.

## Rule

`composer.FixtureDirPattern` matches whole directory segments named `test`,
`tests`, `fixtures`, `testdata`, or `examples` on a repository-relative slash
path, in any ASCII letter case:

```
(^|/)([Tt][Ee][Ss][Tt][Ss]?|[Ff][Ii][Xx][Tt][Uu][Rr][Ee][Ss]|[Tt][Ee][Ss][Tt][Dd][Aa][Tt][Aa]|[Ee][Xx][Aa][Mm][Pp][Ll][Ee][Ss])/
```

`packages/testing` and `packages/attestation` do not match. The case folding is
written as ASCII bracket classes with no flag. Go's `(?i)` folds the long s
(U+017F) to `s` and PostgreSQL's `~*` does not, and `~*` also follows the
database locale. A flag-based pattern would let the two dialects disagree on a
path such as `a/teſts/composer.json`. The bracket form folds nothing and reads no
locale in either dialect (checked: `a/teſts/` and `a/TEſTS/` are false in both,
`TESTS/` and `Test/` are true in both).

Two places apply the one constant:

- `ComposerManifestsQuery` (loader anchor) filters with
  `relative_path !~ '<pattern>'`. The SQL is built from the constant.
- `composer.ProducerPackage` (parser stamp) stops the nearest-manifest walk at a
  fixture manifest and returns no key. It does not fall through to an outer
  manifest, so a fixture class never inherits the root package name.

Both sides must agree. If only the anchor excluded fixtures, a repository with a
real root manifest and a fixture manifest of the same name would still stamp
both classes with one key.

## Path set and limit

Admission criterion (arbiter ruling for #8223 acceptance item 1): a segment name
joins the set when it is a PHP-ecosystem scaffolding convention under which a
published `composer.json` is implausible and a copied real manifest is
plausible.

- `tests`, `fixtures`, `testdata` are the issue's own set.
- `examples` is kept: sample applications copy a library's manifest.
- `test` (singular) is added: Laminas and Zend components keep their PHPUnit
  trees under `test/`. One of the 2 fixture-style manifests measured on the QA
  replica sits under `test`, so the segment is also observed in the corpus.
- `spec`, `__tests__` and other names are not added. Each name is an unmeasured
  false-negative surface until a corpus census shows a manifest under it.

Limit, accepted: a real package whose `composer.json` sits under one of these
segments gets no `package_id`, so calls into it stay unresolved. It is a missed
edge, never a false one. The rule is on the manifest location, not the class
file: a root manifest whose `autoload.psr-4` maps `tests/support/` still keys
the classes there.

## Performance Evidence

Performance Evidence: baseline is the composer manifest read without the
fixture predicate; after is the same read with the final predicate (the ASCII
bracket pattern with `!~`). Backend is the PostgreSQL streaming replica of the
QA environment (version as deployed there). Input shape is `content_files` at
about 149,375 rows with 94 composer manifests (counts only). Terminal row counts
are 94 before and 92 after. The read is unchanged in plan class: one bitmap
scan, no new index, no queue, lease, or write path touched.

Read-only SELECT on the QA environment read replica, `content_files` at about
149,375 rows (`reltuples`), three warm runs each, `EXPLAIN (ANALYZE, BUFFERS)`
of the manifest read, before and after:

| Shape | Plan | Rows | Buffers | Execution |
| --- | --- | --- | --- | --- |
| before (`vendor/` only) | Bitmap Heap Scan over `content_files_relative_path_trgm_idx`, BitmapOr of two index scans | 94 | shared hit 1183 (warm) | 13.15-13.59 ms |
| after (`vendor/` and fixture, final pattern) | same plan, same two index scans | 92 | shared hit 1183 (warm) | 13.31-13.99 ms |

The fixture predicate is a heap Filter on the rows the index already returned
(`Rows Removed by Filter: 2`). The plan, the buffers, and the index scans are
unchanged, so no index is added. The first run after the pattern change read
468 pages cold (`read=468`) and the next two were warm; the buffer comparison
uses the warm runs. Of the 94 composer manifests at this measurement, 2 sit
under a fixture-style path: one under `test` and one under `tests`. The
`vendor/` count is 0, as discovery prunes it. An earlier run of the first
pattern (`~*`, without `test`) read 1777 buffers at 22.9 ms against 22.6 ms for
the baseline on a colder cache; the warm numbers above are the comparable pair.

Parity: the same pattern string evaluated by PostgreSQL `~` on the replica and
by Go gives the same result on 12 sample paths, including `a/teſts/` and
`a/TEſTS/` (both false) and `Test/`, `TESTS/`, `docs/Examples/` (true).

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
package.json rows sit under a fixture-style path. #8244 owns it.

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
test passes. Reverting only the parser predicate (the SQL and the fixture kept) leaves the fact count at 252 and moves the digest, so the pin isolates the parser change. In the fixture the root class carries `package_id` `acme/fixture-lib`
and the fixture class carries none.

## Tests

- `go/internal/parser/php/composer/fixture_test.go`: segment matching (case and
  substring cases), `ProducerPackage` across the directory table, and the
  no-fall-through walk with a real root manifest.
- `TestComposerManifestsQueryShape` pins that the SQL embeds the shared
  constant.

NOT_CHECKED: a live Postgres differential of the new predicate (needs the
Docker gate; the predicate was run read-only on the replica above).
