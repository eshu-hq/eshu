# #8905: content-version fixtures must reach discovery

## Problem

`TestGenerationContentVersionPinsCollectorOutput` pins a digest of the facts the
git collector emits for `testdata/content_version`. Discovery prunes a directory
named `deps`, so the six dependency manifests under `deps/` never reached the
digest: `deps/gomod/go.mod`, `deps/dotnet/App.csproj`, `deps/node/package.json`,
`deps/node/yarn.lock`, `deps/python/pyproject.toml` and
`deps/python/requirements.txt`. A change to those parsers moved no digest, and
the pin test stayed green.

## Fix

- The fixtures moved to `testdata/content_version/manifests/`.
- `TestContentVersionFixtureFilesReachDiscovery` resolves the fixture with the
  snapshotter's own file-set resolution and fails for any file a registered
  parser handles that discovery skips. A seeded pair proves it: a `go.mod`
  under `deps/` is reported, the same file under `manifests/` is not.
- `gitGenerationContentVersion` moved from `git-collector.v9` to
  `git-collector.v10` and the pin was re-pinned.

## Evidence

- Performance Evidence: no hot path runs differently. The only runtime edit is
  the version string. The baseline is the pinned fixture stream at v9: 293
  facts, digest `4dafb339...`. After: 311 facts, digest `e24b8860...`. The 18
  added facts are the six manifests, parsed for the first time by the digest.
  The input is the checked-in fixture tree (59 tracked files); no queue or row counts
  apply because the change touches no database path.
- No-Regression Evidence: the fixture digest test runs in the package's race
  suite (`go test -race -count=1 ./internal/collector/repo/...`), which exits 0.
  Discovery, parsing and fact emission for real repositories are unchanged.
- No-Observability-Change: no metric, span or log key changed. The new version
  string appears in the existing freshness hint, which operators already read
  from the generation record.
- Why the bump: issue #8905 acceptance criterion 3 requires it. The pin test
  does not: with the constant and the pin both left at v9 and the new digest,
  `TestGenerationContentVersionPinsCollectorOutput` passes. Real-repository
  output does not change.
- Cost of the bump: the version is folded into every git freshness hint, so a
  repository stores one new full generation when its reconciliation sweep next
  comes due. The sweep forces it through `interval_elapsed` (default 24 hours)
  and caps it at `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` (default 10), as
  [Reconciliation sweep](../../public/reference/reconciliation-sweep.md)
  describes. The pace is prior evidence, not re-measured here: the #8222 note
  ([PHP import kind](8222-php-import-kind-content-version.md)) measured 7 to 10
  repositories per 10 minutes on a 69-repository reindex. NOT_CHECKED: whether
  `git-collector.v9` (landed in #8922) is deployed anywhere, so the number of
  repositories this roll adds beyond the v9 roll is unknown.
- Why it is bounded, not free: the bump cannot hide or drop a fact, and the
  extra generation per repository is rate-limited by the sweep cap. It still
  costs one full re-parse per repository, which is why a fixture-only change
  should not bump casually. The guard keeps a future fixture from falling into
  a pruned directory.
