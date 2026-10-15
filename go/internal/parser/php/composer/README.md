# parser/php/composer

Composer manifest reads for the PHP cross-repository call keys (#7601).

## Surface

| Function | Side | Reads | Answers |
| --- | --- | --- | --- |
| `ProducerPackage(repoRoot, path, fqcn)` | producer | nearest `composer.json` | the package that publishes `fqcn` from this file, or `""` |
| `LockedPackage(repoRoot, path, fqcn)` | consumer | every `composer.lock` up to `repoRoot`, nearest first | the locked package that provides `fqcn`, or `""` |
| `HasLock(repoRoot, path)` | consumer | `composer.lock` stats | whether any lock can key this file |
| `ManifestName(content)` | loader | one `composer.json` | its name in the composer grammar, or `""` |
| `ValidPackageName(name)` | both | none | composer's `vendor/name` grammar |

## Rules

- Producer: nearest `composer.json` only, never under a `vendor/` directory.
  The name must match composer's grammar. Only `autoload.psr-4` counts
  (`autoload-dev`, `classmap`, `files`, `psr-0`, and the empty fallback prefix
  do not), and the declaring file must sit exactly at the mapped directory
  plus the namespace remainder plus `.php`.
- Consumer: the first lock (nearest first) with a covering prefix decides. The
  longest covering prefix wins; a tie between packages, a `psr-0` prefix, or a
  name outside the grammar yields no key and stops the walk.
- Every `""` is a miss, never a guess: the reducer resolves a key only when
  exactly one definition carries it.

## Performance

Both files go through `project.ScopeCache` keyed by `(path, mtime, size)`, so a
`composer.lock` is decoded once per content generation, not once per PHP file.
`BenchmarkParsePathPHPWithComposerLock` in the parent package measures the
per-file cost with a lock the size of a Symfony application's.

## Known limits

- Classes from a `classmap` or `files` autoload, `psr-0` producers, and enums
  (the PHP adapter emits no enum entities) are never keyed.
- A monorepo whose nested manifests reuse a split package's name (symfony,
  laravel) stamps the same key as the split repository; the key stays
  unresolved when both are indexed.
- PHP class names are case-insensitive; the path match is case-sensitive, so a
  case mismatch is a miss.
