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

## Why Docker builds do not use the Actions cache

`docker-publish.yml` used to export its BuildKit cache to the same store
(`cache-to: type=gha,mode=max`). Each main push wrote about 8.8 GB of layer
blobs, and each pull-request Docker run about 4 GB, so the Go entries were
evicted within minutes of their save (#9632). The Docker builds now keep their
cache in the GHCR package `ghcr.io/eshu-hq/eshu-buildcache`, one fixed tag per
target (`runtime`, `scanner-image`, `scanner-sbom`, `scanner-osruntime`):

- Only a `push` event (main or tag) writes the cache. A pull request, including
  test.yml's `workflow_call`, and `workflow_dispatch` get an empty `cache-to`
  and only read, so no pull request can write a cache that main reads.
- A missing cache is a plain miss, so the first build after the change and a
  build that cannot reach GHCR still succeed.
- Pull-request builds do not log in to GHCR, so they read the cache
  anonymously, which works only if the package is public. A workflow that
  creates a package with `GITHUB_TOKEN` is documented to give it the
  repository's visibility, but nothing in this repository verifies that for
  `eshu-buildcache`. If the package is private, pull-request builds miss the
  cache without any error and run cold; main pushes still write and read it,
  and the daily prune still works because its token comes from the linked
  repository. After the first main push that runs the Docker lanes, check
  `gh api orgs/eshu-hq/packages/container/eshu-buildcache --jq
  '{visibility,repository:.repository.full_name}'` (expected `public` and
  `eshu-hq/eshu`), and confirm that a pull-request Docker lane logs
  `importing cache manifest from ghcr.io/eshu-hq/eshu-buildcache:<target>`.
- `ghcr-buildcache-prune.yml` deletes untagged versions older than 7 days, in
  that package only, and always keeps the 4 newest untagged versions so a tag
  that a concurrent main push just moved is never pulled from under a running
  build. The script takes no package argument. A 404 from the package listing
  fails the run, because GitHub answers 404 both for a package that does not
  exist and for one the token cannot see; a manual dispatch can set
  `allow_missing` for the first run, or `dry_run` to list what would be
  deleted. The image packages are never pruned: their untagged versions are
  the per-platform manifests of live multi-arch images.
- `go/internal/cigates/dockercache_wiring_test.go` pins, for the two
  `docker/build-push-action` steps in `docker-publish.yml`: the event gate, the
  registry package, and exactly one tag per target. Each `cache-to` and each
  `cache-from` line must hold exactly one `ref=` at that package, with the
  target's own tag compared for equality (`:runtime`, or `:scanner-<component>`;
  a scanner may also read `:runtime`), so a prefix, a suffix, a shared tag, a
  second ref or an extra attribute fails. The same two steps may not pass a
  `secrets`, `secret-files`, `secret-envs` or `ssh` input or a `secrets.`
  build-arg: `mode=max` exports every layer to a package that anonymous PR
  readers must be able to pull, so it is treated as public.
  `dockercache_wiring_mutations_test.go` breaks each rule.
  `buildcache_prune_workflow_test.go` pins the prune workflow's triggers,
  permissions, and dry-run switches.
- The cache-export scan (`dockercache_scripts_test.go`) is a text scan of
  `docker-publish.yml` and of repo-local files, and it is not a proof that no
  other step can export a cache. It refuses a `docker/bake-action` step, a
  local action (`uses: ./...`) or local reusable workflow, and any run step
  whose text, or whose referenced files, holds `cache-to`, `cache_to`,
  `buildx bake`, `docker bake` or `docker/bake-action`. A referenced file is a
  path token ending in `.sh`, `.bash`, `.py`, `.yml` or `.yaml`, a path after
  `bash`, `sh`, `zsh`, `source`, `exec` or `.` (so a script without an
  extension is found), or a `./path`, and it must exist in the repository,
  outside `.github/workflows/`. The scan follows the files each referenced file
  names in turn, to four levels, and ends cycles.
- The scan does not cover: `eval`, `bash -c` or other built commands; `make`
  targets; a flag or path built from variables or split across quotes; a
  script fetched at run time; an action from another repository; a chain deeper
  than four files; a file that is not in the repository; a script in another
  language, such as `.rb`, `.js`, `.go` or `.ps1`; a list of files read by
  `xargs`; a bare extensionless path that a step runs directly, with no
  interpreter before it; a flag split across lines with a backslash; any
  workflow other than `docker-publish.yml`; or any build outside the two
  pinned steps that does not appear in text the scan reads.

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

Since #9648 the rule holds repo-wide: every `setup-go` step in every
workflow sets `cache: false`, except `macos.yml`. That workflow is nightly +
`workflow_dispatch` only, never on pull requests, so it writes no PR-scoped
copies; its key is OS-isolated (one 808 MB macOS arm64 entry on main) and
the 45-minute job envelope was measured with a warm cache.

The same issue converted `static-contract-gates.yml`'s 724 MB
`Linux-X64-go-` entry to the main-push-only pattern: the gate job restores
with `actions/cache/restore` (plus the pre-existing restore-only
`taggedbuilds` arm) and saves once, as its last step, only when a main push
misses the exact key. Pull requests restore main's entry through the
default-branch fallback and save nothing. Matrix legs upload identical
content for one key on a main push; the first finisher wins.

## The pin

`go/internal/cigates/gocache_wiring_test.go` pins these rules, and
`gocache_wiring_mutations_test.go` breaks each one — including
`TestSetupGoCacheDisabledInTestWorkflow`, which fails any `setup-go` step in
`test.yml` that re-enables the built-in cache,
`TestSetupGoCacheDisabledAllWorkflows`, which fails any re-enabled step in
any other workflow outside the documented exceptions, and
`TestStaticContractCacheWiring`, which fails a bare `actions/cache` step or
an unguarded save in `static-contract-gates.yml`. Keep the restore step
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

## Pre-fix eviction evidence gap (#9706)

Whether LRU eviction alone removed the pre-#9632 `gobuild-` entry cannot be
proven from the exposed APIs, and this is accepted as a permanent gap. The
audit established: the entry disappeared inside an ~11.5-minute window
(parent go-core success 01:33:44Z to restore miss 01:45:15Z on the dec23a17
push, same `go.sum`); the store read over the 10 GB limit on both sides
(24.53 GB the day before, 10.55 GB active hours after); a concurrent PR-9544
docker blob flood (1.29 GB at 01:45:15.8Z) hit the same over-limit store at
the miss second; key-mismatch, branch-scope, and 7-day-expiry are ruled out.
What the REST API cannot supply: timestamped eviction/deletion records and
historical store totals — only a 1.60 GB survivor lower bound is derivable,
and a manual delete inside the window is unfalsifiable. LRU eviction is the
sole surviving automatic mechanism, but that is elimination, not observation.
