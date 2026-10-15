# parser/php/composer

Composer manifest reads for the PHP cross-repository call keys (#7601).

## Surface

| Function | Side | Reads | Answers |
| --- | --- | --- | --- |
| `ProducerPackage(repoRoot, path, fqcn)` | producer | nearest `composer.json` | the package that publishes `fqcn` from this file, or `""` |
| `LocksFor(repoRoot, path)` | consumer | every `composer.lock` up to `repoRoot`, nearest first | the file's lock chain, read once per file |
| `Locks.Package(fqcn)` | consumer | the chain | the locked package that provides `fqcn`, or `""` |
| `ManifestName(content)` | loader | one `composer.json` | its name in the composer grammar, or `""` |
| `ValidPackageName(name)` | both | none | composer's `vendor/name` grammar |

## Rules

- Producer: nearest `composer.json` only, never under a `vendor/` directory.
  The name must match composer's grammar. Only `autoload.psr-4` counts
  (`autoload-dev`, `classmap`, `files`, `psr-0`, and the empty fallback prefix
  do not), and the declaring file must sit exactly at the mapped directory
  plus the namespace remainder plus `.php`.
- Consumer: the first lock (nearest first) with a covering prefix decides. The
  longest covering prefix wins; a tie between packages, a `psr-0` prefix, a
  `psr-4` prefix without the trailing `\`, or a name outside the grammar
  yields no key and stops the walk.
- Consumer: the consumer's own `composer.json` files (`autoload` or
  `autoload-dev`, `psr-4` or `psr-0`, file directory up to the repository root)
  can claim the class too. Composer tries the longest psr-4 prefix first, so an
  own prefix suppresses the key only when it is at least as long as the winning
  locked prefix: own `MyCorp\` yields to a locked `MyCorp\Logging\`, while own
  `App\` ties `laravel/pint`'s `App\` (the namespace of every Laravel
  application) and suppresses. The empty own fallback never suppresses.
- Every `""` is a miss, never a guess: the reducer resolves a key only when
  exactly one definition carries it.

## Performance

Both files go through `project.ScopeCache` keyed by `(path, mtime, size)`, so a
`composer.lock` is decoded once per content generation, not once per PHP file,
and the PHP adapter builds a file's lock chain once, at its first candidate
call. A lookup probes one map entry per namespace segment of the class.
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
