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

Each job restores with `actions/cache/restore` and saves with
`actions/cache/save` as its last step, under
`github.event_name == 'push' && github.ref == 'refs/heads/main'`. The save
skips an exact-key hit and uses the restore step's `cache-primary-key`. A pull
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
second time, and a readiness shard downloaded both copies. These jobs set
`cache: false`, and `scripts/ci/go-mod-download-retry.sh` downloads the
modules. In go-core and go-race runs before this change, the pre-warm step
took 14 to 26 s when setup-go missed its cache. A warm `Set up Go` took 40 to
82 s, most of it downloading a 1.8 GB entry that held the build cache again.

## The pin

`go/internal/cigates/gocache_wiring_test.go` pins these rules, and
`gocache_wiring_mutations_test.go` breaks each one. Keep the restore step
named `Restore Go build cache`, with `Set up Go` directly after it. The
readiness weights refresher reads the shard's cache outcome from that step's
log section, up to the next step.
