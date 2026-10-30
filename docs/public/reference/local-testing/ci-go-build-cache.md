# CI Go Build Cache

The slow Go lanes keep the compiler cache between runs in
`/tmp/eshu-ci/go-build` (`GOCACHE`). Three jobs use it: `go-core` and
`go-race` in `test.yml`, and the readiness shards in
`live-postgres-readiness.yml`.

| Job | Key | Restores | Saves |
|---|---|---|---|
| `go-core` | `<os>-<arch>-gobuild-<go.sum hash>` | every run | main push only |
| `go-race` (4 shards, one key) | `<os>-<arch>-gobuildrace-<go.sum hash>`, falls back to `gobuild-` | every run | main push only |
| readiness shards | falls back to go-core's `gobuild-` entry | every run | never |

## Why only main push saves

The repository cache store holds 10 GB and evicts entries by last access. A
cache that a pull request saves is visible only to re-runs of that pull
request, but it still counts against the 10 GB. Before #9598 each active pull
request wrote up to about 7 GB, and main's `gobuild-` entry was evicted every 15
to 60 minutes. Readiness shards that started in that gap compiled cold.

go-core and go-race restore with `actions/cache/restore` and save with
`actions/cache/save` as their last step, under
`github.event_name == 'push' && github.ref == 'refs/heads/main'`. The save
skips an exact-key hit and uses the restore step's `cache-primary-key`. The
readiness shards restore only and never save. A pull
request run restores main's entry, because a run can restore caches from the
default branch
([GitHub docs](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching)).
A pull request that changes `go.sum` falls back to main's entry for the old
hash through the `gobuild-` prefix. It never saves a cache of its own, so every
push to that pull request recompiles the packages the `go.sum` change touches,
until main saves the new key. This trades some pull-request compile time for
less pressure on the store.

## Why setup-go does not cache

`actions/setup-go` caches `go env GOMODCACHE` and `go env GOCACHE`. With
`GOCACHE` set to the persisted directory, its entry held the build cache a
second time, and a readiness shard downloaded both copies. Those lanes set
`cache: false`, and `scripts/ci/go-mod-download-retry.sh` downloads the
modules. In go-core and go-race runs before this change, the pre-warm step
took 14 to 26 s when setup-go missed its cache. A warm `Set up Go` took 40 to
82 s, most of it downloading a 1.8 GB entry that held the build cache again.

Since #9462 every other `setup-go` step in `test.yml` (`pr-plan`,
`merge-group-slim`, `verify-contracts`) sets `cache: false` too. They run on
the default `GOCACHE`, so they never shared the persisted directory — but each
PR-scoped copy still counts against the 10 GB store whose evictions were
emptying main's `gobuild-` entries. Each of those jobs already pre-warms its
modules, so the only cost is the 14 to 26 s download on a cold runner.

## The pin

`go/internal/cigates/gocache_wiring_test.go` pins these rules, and
`gocache_wiring_mutations_test.go` breaks each one — including
`TestSetupGoCacheDisabledInTestWorkflow`, which fails any `setup-go` step in
`test.yml` that re-enables the built-in cache. Keep the restore step
named `Restore Go build cache`, with `Set up Go` directly after it. That keeps
each shard log's cache outcome at a fixed landmark: the
`Cache restored from key:` or `Cache not found for input keys` line sits inside
that step's log group, which ends where `Set up Go` begins. With
`cache: false`, setup-go prints no cache lines of its own there. The weights
refresher in `scripts/lib/live_postgres_readiness_refresh_v2_pool.py` (#9604)
reads both: `cache_era` finds the step by name and counts a shard without it
as cold, and `restore_outcome` reads from its `actions/cache/restore` group to
the next step.

## The acceptance census

The #9598 acceptance is the restore hit rate over at least 10 main push
runs. Measure it with `scripts/dev/readiness-cache-census/census.py` over
fetch.sh run directories (same layout the weights refresher reads). The
census fails closed on any shard whose restore section shows neither a hit
nor a miss. The first post-fix census (4 runs: 2 all-hit, 2 all-miss) is the
evidence that the count waits on #9632.
