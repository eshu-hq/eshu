# PHP Package Keys For Cross-Repository Calls (#7601)

This note records the proof for the PHP half of #7601. The JS/TS half is in
[7601-js-ts-package-keys.md](7601-js-ts-package-keys.md). The loader anchor it
extends is in
[7601-anchored-symbol-definition-loader.md](7601-anchored-symbol-definition-loader.md).

## What changed

The PHP parser now writes both halves of the
`package:<package_id>#<export_name>` key the reducer already resolves
(`codeCallDefinitionSymbolKeys` and `codeCallEdgeSymbolKeys` in
`go/internal/reducer/code/call/shared/symbol_index.go`). No reducer, schema,
API, OpenAPI, or MCP code changed.

- **Producer** (`go/internal/parser/php/package_keys.go`,
  `go/internal/parser/php/composer/manifest.go`). A class or interface gets
  `package_id`, the name of the nearest `composer.json` between the file and the
  repository root in composer's `vendor/name` grammar, and `export_name`, its
  fully qualified name. This needs an `autoload.psr-4` prefix of that manifest
  that covers the class with the file at exactly the mapped directory plus the
  namespace remainder plus `.php`. A public static class method gets
  `export_name` `<FQCN>::<method>`. Nothing is keyed under `vendor/` or under a tests, fixtures, testdata, or
  examples directory (#8223), for
  `autoload-dev` (composer loads it for the root package only), classmap,
  psr-0, or the empty fallback prefix, for traits, enums (the adapter emits no
  enum entities), or free functions, or in a file with more than one namespace
  declaration.
- **Consumer** (`package_keys.go`, `composer/lock.go`). `new X(` and
  `X::method(` get `package_export_symbol`
  `package:<locked name>#<FQCN>[::<method>]` when `X` is a `use`-bound name
  (alias and group use included) or a fully qualified literal. The name is
  bound from the raw class node and the `use` rows, never from the parser's
  last-segment alias map, and a clause with its own `function` or `const`
  keyword (`use A\{B, function c}`) binds nothing. The locked name comes from
  every `composer.lock` from the file's directory up to the repository root,
  nearest first. Tie rule: the first lock in that chain whose `packages` or
  `packages-dev` autoload a covering psr-4 prefix decides; inside that lock the
  longest covering prefix wins; two packages tied on that prefix give no key
  (pinned by `TestLockLookupLongestPrefixTiesAndBlockers` in
  `go/internal/parser/php/composer/lock_test.go`). A psr-0 prefix, a psr-4 prefix without the
  trailing `\`, or a name outside the grammar gives no key. The consumer's
  own `composer.json` files (autoload and autoload-dev, psr-4 and psr-0) can
  claim the class too. An own
  prefix suppresses the key only when it is at least as long as the winning
  locked prefix, because Composer tries the longest psr-4 prefix first: own
  `MyCorp\` yields to a locked `MyCorp\Logging\`, while own `App\` ties
  laravel/pint's `App\` and suppresses. The empty own fallback never
  suppresses (Composer searches it last).
- **Loader** (`go/internal/storage/postgres/code/producers/store.go`).
  `PackageScopeIDs` sends a composer name to the new `ComposerManifestsQuery`
  (`composer.json`, MATERIALIZED CTE, generation-tag rule, `vendor/`
  excluded). It sends every other name to the existing package.json read. The
  two name spaces are disjoint: npm names have no `/` or start with `@`. A
  request with both kinds runs both reads and unions the scopes.
  `ComposerManifestName` delegates to the parser's `composer.ManifestName`, and
  `TestComposerManifestNameMatchesThePackageIDTheParserStamps` pins it.

## Rulings

- Rule B (lock-based consumer) over rule A (corpus psr-4 map), arbiter ruling
  relayed by the coordinator. Rule A keys every `use` import under any corpus
  psr-4 prefix. That falsely links a consumer that uses its own class without
  a psr-4 declaration for it (classmap, or no `composer.json`). The QA corpus
  has 94 `composer.json`, 21 with psr-4 prefixes, against 69 PHP repositories
  with `use` imports.
- Own-namespace guard, added after the redraw (below): `monicahq/monica` locks
  `laravel/pint`, whose `composer.json` declares psr-4 `App\`. Without the
  guard, all 5,767 of monica's own `App\` calls were keyed to `laravel/pint`.
  Turning the guard off makes
  `TestExtractRowsKeepsLaravelStyleOwnClassOutOfAnotherRepository` resolve
  `index` into `uid:repo-pint:User`. Review narrowed it to the length rule
  above: the first version dropped every class under an own prefix, which also
  dropped `composer/composer`'s calls into `composer/pcre` (`Composer\Pcre\`
  is longer than its own `Composer\`).

## Measurements

Public substitute corpus: the QA corpus is not reachable from the development
host. These repositories were cloned with `git clone --depth 1` on 2026-10-15:
symfony/{console, process, finder, filesystem, string, event-dispatcher,
symfony, demo, polyfill-mbstring}, Seldaek/monolog, php-fig/{log,
http-message}, composer/composer, laravel/{framework, laravel},
illuminate/{support, collections}, guzzle/{guzzle, psr7, promises},
thephpleague/{flysystem, commonmark, oauth2-server}, sebastianbergmann/diff,
nikic/PHP-Parser, doctrine/inflector, koel/koel, monicahq/monica,
BookStackApp/BookStack, ramsey/{uuid, collection}, webmozart/assert,
vlucas/phpdotenv, brick/math, nette/schema, dragonmantank/cron-expression,
egulias/EmailValidator, lcobucci/jwt. That is 38 repositories and 24,773 PHP
files, parsed with this branch's `parser.DefaultEngine` (`vendor/` skipped).

Rule A, before any code (24 repositories, 18,142 files). Of 72,323 `use`
imports, 1,938 sit under a unique foreign psr-4 prefix and 719 under a shared
longest prefix. 1,336 strict calls resolve (1,144 `new`, 192 static). A
hand-read of 45 found 45 true. Two of the 1,336 failed an automatic text
check, and both are false-link patterns rule B avoids by binding from the raw
node:

- `new \InvalidArgumentException(` in a file that imports
  `Psr\Log\InvalidArgumentException`. The parser reduces the class to its last
  segment, so a name-based binder would link the global class to psr/log.
- `ResendEmail::from(` with `use Resend\Email as ResendEmail;` next to an
  import of another `Email`. The receiver normalizes to `Email`, the short
  name of the other import.

Also measured: 162 `new X\Y(` relative qualified names whose last segment
equals a different import (never keyed), 1,437 aliased imports, and 22
multi-namespace files.

Rule B, on the final code (38 repositories):

| Measure | Count |
| --- | --- |
| Producer stamps (classes, interfaces, public static methods) | 14,631 / 1,120 / 8,258 |
| Keyed consumer calls (`new` / static) | 1,089 / 6,938 |
| Resolved cross-repository (`new` / static) | 152 / 86 |
| Unresolved, ambiguous | 184 (symfony/console 137, process 28, finder 15, filesystem 4: each split repository and the monorepo stamp the same name) |
| Unresolved, no definition | 7,605 (producer not in the corpus, inherited or trait static methods, laravel/framework subpackage names) |

The automatic check passed all 238 resolved edges: the call is on the line
through the `use` binding or a fully qualified literal; the producer file
declares the class in that namespace, and for a static call the public static
method; the consumer's `composer.lock` lists the package with a covering psr-4
prefix; and the producer's nearest `composer.json` name equals the package. A
seeded sample of 45 (20 static, 25 `new`) was also hand-read: 45 true, 0 false,
0 undecidable. It covers 11 consumer/producer pairs, including the Laravel `App\`
consumers koel and monica and the symfony/demo to symfony/symfony split pair.
Scripts and outputs are in the session scratchpad, not committed.

## Edge cases

| Case | Handling | Proof |
| --- | --- | --- |
| Private, protected, or instance method | No producer key | `TestDefaultEngineParsePathPHPStampsProducerPackageKeysOnPSR4Classes` |
| Path not matching the class name, `autoload-dev`, classmap, empty fallback prefix, unnamed or malformed name, `vendor/`, two namespace blocks | No producer key | `TestDefaultEngineParsePathPHPSkipsProducerKeysComposerCannotAutoload` |
| `new \Global(` with an imported last segment, relative qualified name, alias sharing a short name, tied prefix, malformed locked name, own class, `$var::`, instance call, `use function`, `parent::`, `self::` | No consumer key | `TestDefaultEngineParsePathPHPSkipsUnprovableConsumerKeys` |
| No `composer.lock`; multi-namespace consumer | No consumer key | `TestDefaultEngineParsePathPHPSkipsConsumerKeysWithoutComposerLock`, `...InMultiNamespaceFile` |
| Nested lock that does not cover the class | Next lock up decides | `TestDefaultEngineParsePathPHPKeysConsumerFromNearestCoveringLock` |
| Locked dev tool declaring the consumer's own namespace | No consumer key | `TestDefaultEngineParsePathPHPSkipsConsumerKeysForOwnNamespaces` |
| Own prefix shorter than, equal to, or longer than the locked one; own empty fallback | Keyed only when the locked prefix is longer | `TestLocksOwnNamespaceYieldsOnlyToALongerLockedPrefix`, `TestLocksOwnFallbackPrefixNeverOutranksALockedPrefix` |
| Locked psr-4 prefix without the trailing `\` (`Acme` against `AcmeOther\Thing`) | Blocks, never keys | `TestLockPSR4PrefixWithoutTrailingBackslashOnlyBlocks` |
| `function` or `const` clause in a mixed group use | Binds no class | `TestDefaultEngineParsePathPHPSkipsUnprovableConsumerKeys` |
| Two repositories stamp the same composer name | No row | `TestExtractRowsLeavesParsedPHPComposerImportUnresolvedWhenTwoProducersShareTheName` |

## Correlation truth matrix

| Axis | Proof | Status |
| --- | --- | --- |
| Positive | `TestExtractRowsResolvesParsedPHPComposerImportAcrossRepositories`: `new Logger()` and `Logger::create()` give `CALLS` rows with `resolution_method=import_binding` into the producer | Checked |
| Negative | Edge-case table; `TestExtractRowsKeepsLaravelStyleOwnClassOutOfAnotherRepository` | Checked |
| Ambiguous | Two producers of one composer name give no row | Checked |
| Graph | `ESHU_GRAPH_BACKEND=neo4j bash scripts/verify-golden-corpus-gate.sh` exit 0 at head `0d865355767f8eff22891289ee26b93c4054cdf7` (the docs-only commit that follows changes no fixture or code): Repository node count 35, the `>= 1` checks pass, 0 FAIL; `bash scripts/test-verify-golden-corpus-gate.sh` exit 0 | Checked |
| Query | Same run: the new shape `POST /api/v0/code/dead-code/cross-repo?golden_scope=ledger-kit` (`formatAmount` must be `live_by_consumer`) PASS | Checked |
| RED | The same gate without the parser keys, to show the shape fails without the edge | NOT_CHECKED (not run) |

## Performance

Performance Evidence: `BenchmarkParsePathPHPWithComposerLock` (40 imports, 80
keyed calls, 120-package lock), interleaved base `88beb12d0` and head binaries,
10 rounds each, `-benchtime 200x -cpu 1`, on a shared 16-core host: median
10.14 ms to 10.51 ms (+3.6%), minimum 10.00 to 10.17 ms (+1.7%).
`BenchmarkParsePathPHPRouteHeavy` (no composer files): 30.83 ms to 31.49 ms
(+2.1%). Both are within the ruled 5% bound. Each lock is decoded once per
`(path, stat)`. Each file reads its lock chain once, at its first candidate
call, and looks a class up by one map probe per namespace segment.

`EXPLAIN (ANALYZE, BUFFERS)` on local PostgreSQL 18.6 with a seeded
`content_files` of 144,520 rows, 800 repository scopes, 100 composer.json, 20
vendored composer.json, and 400 package.json, with the production
`content_files_relative_path_trgm_idx`. Composer read: 1.23 to 1.32 ms, a
Bitmap Index Scan on the trigram index inside the MATERIALIZED CTE (100 rows,
20 vendored rows removed by filter). Package read on the same data: 2.02 to
2.05 ms with the same plan class. Without the trigram index, both statements
seq-scan: 68 ms and 81 ms. Replica EXPLAIN: NOT_CHECKED.

Observability Evidence: `eshu_dp_producer_manifest_tag_outcomes_total` gains a
third closed `kind` label, `composer`
(`TestScopeIDsRecordComposerTagOutcomesUnderTheirOwnKind`). The existing
`code call symbol definition load` log line already counts package keys and
producer scopes (composer keys count as package keys). Resolved edges carry
`resolution_method=import_binding`. There is no new metric, span, or log key.

## Content version

`gitGenerationContentVersion` moves from `git-collector.v2` to
`git-collector.v3`. The parser now emits new fields for unchanged PHP input
that has composer files, and without the bump `GenerationContentVersion()` is
byte-identical on base and head, so already-indexed PHP repositories would keep
keyless facts until their next commit. The pin fixture (the
`content_version` fixture tree beside `snapshot_content_version_gate_test.go`)
has no composer files, so the re-pin moves only
`content_version`; the digest and the 238-fact count are unchanged. A PHP
composer change is therefore not caught by the pin; the fixture was left alone
because #7978 owns it. Production discovery already prunes `vendor/`
(`go/internal/collector/repo/git/snapshot_discovery.go`); the parser and loader
`vendor/` guards stay as defense in depth.

## Known limits

- Split monorepos: symfony/symfony and laravel/framework stamp their nested
  manifest names. A key stays unresolved when the split repository is also
  indexed. A consumer that locks `laravel/framework` misses classes whose nearest
  manifest is a subpackage (`illuminate/support`).
- PHP class names are case-insensitive; the path and binding matches are
  case-sensitive, so a case mismatch is a miss, never a false link.
- Inherited and trait static methods (`Model::find()`, Carbon) are misses:
  the key names the called class, and only the declaring class is stamped.
- A consumer with no committed `composer.lock` (most libraries) gets no keys.
- An unresolved PHP key still falls through to the repository-unique name
  fallback, as an unkeyed call did before. The JS-family barrier (#7610) has no
  PHP equivalent yet.
- A class whose namespace's first segment is exactly `Function` or `Const` (any
  case), such as `use Function\Foo;`, is read by the vendored tree-sitter PHP
  grammar as a `use function` or `use const` clause: the first segment is
  dropped from the name and the `import_type` is wrong, so the class gets no
  Composer key. This is a grammar limit that pre-dates #7601 and is a missed
  key, never a false link. Namespaces that only begin with those words, such as
  `Functional\` or `Constraint\`, were misread by `phpImportKind` and are fixed
  by #8222.
- Nested test-fixture `composer.json` files: a fixture manifest that reuses a
  real package name and maps a class at the same path can capture an edge only
  when the real producer is not indexed; when both are indexed, the unique rule
  leaves the key unresolved. The JS leg shares this exposure (#8223).

## NOT_CHECKED

- QA corpus: `composer.lock` count and duplicate composer names (replica
  queries: `count(*)` of `content_files` where `relative_path` is or ends in
  `composer.lock`, and duplicate `name` values across stored `composer.json`).
- `packages-dev` autoload presence in real locks (packages-dev is read the same
  way as packages).
- The B-7 RED run (the gate without the parser keys), and the replica EXPLAIN
  of `ComposerManifestsQuery`.
