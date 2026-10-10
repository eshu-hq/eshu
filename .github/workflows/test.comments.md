# Build Test workflow rationale

These YAML comments were moved verbatim from `test.yml`.
Comments inside executable scalar blocks remain in the workflow.

Original workflow SHA-256: `da8c3cbd76f2f5118b4b1a5afeb2573c81d426aae36c4e30acee2d1400a79ce5`.

## Comment group 1

Original lines 22–27.

```yaml
  # Decide whether this event changed any non-docs path. A docs-only PR (docs
  # tree, root markdown, mkdocs config) skips the heavy Go lanes below; the
  # always-on docs-helm-hygiene job still builds the docs. Non-PR events (push
  # to main, nightly schedule, manual dispatch) always run the heavy lanes — the
  # per-job `if` short-circuits on `github.event_name != 'pull_request'` — so a
  # docs commit that lands on main is never left unverified.
```

## Comment group 2

Original lines 40–55.

```yaml
      # merge_group (#5814): every downstream heavy-lane `if:` already runs
      # unconditionally on any non-pull_request event (`github.event_name !=
      # 'pull_request'`, see verify-contracts/go-core/go-race below), so the
      # value of `code` is never actually consulted for a merge-queue entry.
      # What DOES matter is that this job always SUCCEEDS on merge_group:
      # go-core-complete and go-race-complete both require
      # `needs.changes.result == 'success'`, and a `changes` job that fails
      # (rather than cleanly skipping) marks its dependents `skipped`, which
      # the umbrella's "skipped is pass" rule would then wrongly accept as a
      # legitimate docs-only skip while nothing actually built or raced.
      # dorny/paths-filter@v3 does document merge_group support (it seeds
      # base/head from the event), but this job's shallow `fetch-depth: 2`
      # checkout has never been proven against merge_group's synthetic
      # merge-base lookup, so we do not depend on it: skip the diff outright
      # on merge_group and report code=true directly, which costs nothing
      # since it is already unused downstream.
```

## Comment group 3

Original lines 66–88.

```yaml
          # Without this line the whole filter below is decorative. dorny
          # compiles each pattern separately and, by default
          # (predicate-quantifier: some), includes a file the moment ONE pattern
          # matches it. `**` matches everything, so it short-circuits first and
          # the five `!` negations can never subtract: the filter is exactly
          # equivalent to `code: ['**']`, and every job it gates runs on every
          # PR including a docs-only one. Confirmed against
          # dorny/paths-filter@v3's own src/filter.ts, which defines precisely
          # PredicateQuantifier { EVERY = 'every', SOME = 'some' } and falls
          # through to `patterns.some(...)`; v3 has no SOME_WITH_EXCLUDES case
          # (that arrived later on master), so 'every' is the only value that
          # makes a negation mean anything here. #5896.
          #
          # This is also what #5818 actually needed. That fix added the
          # `!.agents/**` negation below and it has been inert ever since —
          # under `some` no negation subtracts, however it is anchored, so the
          # root-anchoring of `!*.md` was never the operative cause of those
          # ~118 wasted runner-minutes.
          #
          # Scoped deliberately to the three filters that HAVE negations. Do not
          # copy this line to static-contract-gates.yml: its filters are all
          # positive patterns, and 'every' would require a file to match every
          # one of them at once, selecting almost nothing.
```

## Comment group 4

Original lines 90–102.

```yaml
          # `code` is true when ANY changed file is outside the docs set. The
          # bare `*.md` negation is ROOT-ANCHORED (no `/` in the pattern), so it
          # only strips root-level markdown (README.md, CLAUDE.md, AGENTS.md,
          # etc.) — nested markdown under go/**/*.md, sdk/**/*.md,
          # tests/**/*.md, and skill-fragments/**/*.md still counts as code
          # (deliberate: those trees are code-adjacent and tied to the Go
          # build/test/security surface these three workflows gate). The one
          # exception is `.agents/**`, negated explicitly: it holds
          # agent-instruction skills/SKILL.md/reference markdown with no
          # Go/build/lint/security consumer in this workflow — it is verified
          # only by verify-agent-canon.sh (verify-agent-hygiene.yml) and
          # verify-skill-roundtrip.sh (static-contract-gates.yml), neither of
          # which this filter gates. See #5818.
```

## Comment group 5

Original lines 112–112.

```yaml
  # Docs-only changes skip the Go lanes; their Markdown cap must still run.
```

## Comment group 6

Original lines 135–136.

```yaml
      # #7886: true only when both Docker Hub secrets exist (fork and Dependabot
      # PRs get none). Expose the boolean, not the secrets, so no step sees them.
```

## Comment group 7

Original lines 145–146.

```yaml
      # #7886: authenticated pulls avoid the shared anonymous Docker Hub limit
      # on hosted runners. Skipped, not failed, when the secrets are absent.
```

## Comment group 8

Original lines 163–166.

```yaml
      # Pre-warm Go modules with retries so a dropped proxy stream fails
      # HERE, named for what it is, instead of surfacing as one of the
      # contract verifiers below failing with no module ever compiled
      # (#6615 follow-up to #6075).
```

## Comment group 9

Original lines 175–176.

```yaml
      # The trusted required-gates publisher reads main's registry. Retain the
      # previous owner while this PR introduces the dedicated cap job above.
```

## Comment group 10

Original lines 187–189.

```yaml
          # Pin the base used to build this PR merge commit. A newer main tip
          # fetched at depth 1 can have no visible merge base and make the
          # verifier's two-dot fallback attribute main's changes to the PR.
```

## Comment group 11

Original lines 267–284.

```yaml
      # install-apt-packages.sh has no separate verify mode -- the test
      # mirror is its only enforcement (bounded curl transfer, fail-closed
      # checksum verify, arch-support guard). It is hermetic with respect to
      # the INSTALLER under test: fixtures are file:// URLs and every case
      # isolates PATH and bin_dir, so it neither reinstalls ripgrep nor
      # disturbs the step above.
      #
      # It is NOT independent of that step, though, and the order matters:
      # the mirror's own assertions shell out to `rg` dozens of times, and
      # `rg` is exactly what "Install ripgrep" provides -- the runners log
      # "Setting up ripgrep" on every job, so it is not preinstalled. Keep
      # this step AFTER that one. Reordering fails loudly rather than
      # silently (a missing rg exits 127, the `if rg -q` guards take their
      # record_fail branch, and the gate goes red), but it fails for a
      # confusing reason, so do not rely on that.
      #
      # See specs/ci-gates.v1.yaml's ci-install-apt-packages entry, which
      # declares this job as its `ci.job`.
```

## Comment group 12

Original lines 289–297.

```yaml
      # go-mod-download-retry.sh has no separate verify mode either, for the
      # same reason install-apt-packages.sh above does not: the test mirror
      # IS this gate's verification, hermetic and off the network (a
      # fake `go` on PATH). Without this step the mirror only ever ran when a
      # human happened to invoke it -- #6615 review found it unwired into CI,
      # the registry, or any hook, so a regression in the wrapper every other
      # job in this workflow now depends on could land silently. See
      # specs/ci-gates.v1.yaml's ci-go-mod-download-retry entry, which
      # declares this job as its `ci.job`.
```

## Comment group 13

Original lines 302–306.

```yaml
      # Same shape for go-install-retry.sh, the retry wrapper every CI tool
      # install (golangci-lint below, benchstat, govulncheck, gosec, nancy)
      # goes through. Its mirror also fails if any workflow reintroduces a
      # bare `go install`. See specs/ci-gates.v1.yaml's ci-go-install-retry
      # entry, which declares this job as its `ci.job`.
```

## Comment group 14

Original lines 311–317.

```yaml
      # Pre-warm Go modules with retries so a dropped proxy stream fails
      # HERE, named for what it is, instead of surfacing as a build/lint
      # failure that never compiled anything -- this job runs the whole-module
      # `go build ./...` and `golangci-lint run ./...` that are the
      # authoritative "does the merge result still compile" check, so a
      # network flake here is the highest-cost place for this gap to have
      # gone unfixed (#6615, the follow-up #6075 should have covered).
```

## Comment group 15

Original lines 326–327.

```yaml
      # tools/golangci-lint-filelength is its own module (own go.mod), so the
      # go/ pre-warm above does not touch its cache -- warm it separately.
```

## Comment group 16

Original lines 337–341.

```yaml
      # skip_parity_test.go compares this plugin's skip() against the bash
      # mirror in scripts/dev/precommit-go.sh. It is the regression guard for
      # #6104, where the two drifted and the local hook rejected long _test.go
      # files CI accepted. Building the plugin does not run it, so without this
      # step the guard fires only under `make pre-pr` and never in CI.
```

## Comment group 17

Original lines 347–347.

```yaml
      # tools/golangci-lint-dirgate is also its own module -- warm it too.
```

## Comment group 18

Original lines 357–370.

```yaml
      # The dirgate Go plugin above is loaded by "Lint Go" below, but
      # golangci-lint's OWN nolint processor suppresses a finding purely on
      # `//nolint:dirgate` marker PRESENCE before the plugin's own
      # nolintJustification check (nolint.go) ever sees it -- so a BARE,
      # unjustified marker silently passes "Lint Go" even though the
      # dirgate gate requires a justification (see
      # tools/golangci-lint-dirgate/README.md's "Escape hatch" section).
      # scripts/lib/dirgate-core.sh (sourced by verify-dirgate.sh) is the
      # only path that enforces that rule, plus TSV/generated-Go lockstep
      # and the naming-exempt ledger's stale-row hard fail -- none of which
      # "Lint Go" alone can catch. Mirrors go-file-cap's split between the
      # golangci-lint plugin path and its own local/CI bash enforcement (see
      # specs/ci-gates.v1.yaml's go-dir-gate entry, which declares this job
      # as its `ci.job`).
```

## Comment group 19

Original lines 375–381.

```yaml
      # The plugin's own unit tests. "Lint Go" runs the plugin against the real
      # tree, but that tree is green, so none of the negative-case logic ever
      # fires there -- a plugin-only regression in the naming word-boundary
      # rule, nolint parsing, or swap/shrink detection would merge green, and
      # the bash mirror's separate tests cannot catch a Go-side bug. This
      # module is outside go/, so `go test ./...` in go/ does not reach it
      # (#6054 review finding).
```

## Comment group 20

Original lines 398–401.

```yaml
        # gofumpt is configured under `formatters` in v2, so it is
        # NOT enforced by `golangci-lint run`. Run the formatter
        # check separately so a non-gofumpt-clean tree fails CI
        # (codex P2 review on PR #3847).
```

## Comment group 21

Original lines 411–440.

```yaml
    # Umbrella gate (#5814): a single stable status name that is green only
    # when `go-core` passed. Point any future required-status-check config at
    # THIS job, never at `go-core` directly — `go-core` depends on the `changes`
    # job plus the `if` guard above, so it is SKIPPED on a docs-only PR and
    # never reports; requiring it directly would strand every docs-only PR
    # forever.
    # This is the same forward-safe aggregator shape #5757 established for
    # `go-race-complete` below — see that job's comment for the full rationale.
    #
    # `skipped` is a pass ONLY when the `changes` gate deliberately skipped
    # `go-core` on a docs-only PR. Treating that as green keeps this a single
    # always-reported check whose name is safe to mark required — it never
    # strands a docs-only PR.
    #
    # But `skipped` alone is not enough to conclude that. GitHub implicitly
    # prepends `success()` to any job `if:` that does not itself name a status
    # function, so `go-core`'s guard is really
    # `success() && (event_name != 'pull_request' || changes.outputs.code == 'true')`.
    # If `changes` itself FAILS or is CANCELLED (checkout hiccup, paths-filter
    # action error, runner flake), `go-core` is marked `skipped` too — not
    # `failure`. Keying only on `needs.go-core.result` would then report GREEN
    # having compiled nothing, which is the exact false-green class this gate
    # exists to close, just moved one hop upstream. So `changes` is an explicit
    # dependency and its own failure fails this gate.
    #
    # `go-core` runs a whole-module `go build ./...` (plus lint/fmt), so this is
    # the authoritative "does the merge result still compile" required check —
    # the cheapest possible gate against the #5802/#5814 failure class, where
    # two individually-green PRs merged into a `main` that did not build because
    # each side only compiled against its own base.
```

## Comment group 22

Original lines 462–475.

```yaml
    # Sharded authoritative race gate. Each shard runs `go test -race` on a
    # disjoint slice of `go list ./...`; the union across shards is every
    # package, so coverage is identical to the old single `go test ./... -race`.
    #
    # Why sharding does NOT weaken race coverage: `go test -race` instruments the
    # ENTIRE dependency closure of each test binary. A race in package A that is
    # triggered by package B's test is caught when B runs, because A is compiled
    # with -race as B's dependency. Sharding by TEST package only changes which
    # runner runs which test binary — every test binary still instruments its
    # full closure — so no race that was detectable before becomes undetectable.
    #
    # N is `strategy.job-total` (the matrix length), so the round-robin partition
    # `NR % N == matrix.shard % N` self-adjusts when shards are added/removed.
    # The matrix values MUST stay contiguous 1..N or a package slice is dropped.
```

## Comment group 23

Original lines 483–501.

```yaml
    # #6615: 25m had no headroom, unlike the per-binary 900s budget below,
    # which the same job's own comment documents as sized with deliberate
    # margin. Measured 587 completed go-race shard runs across ~150 recent
    # test.yml runs on main and PRs (gh api .../actions/runs/<id>/jobs,
    # 2026-09-17): overall max 24.2m, p99 21.9m, p50 17.6m; per-shard max
    # ranged 20.8m-24.2m. The round-robin partition (NR % N) shifts which
    # packages land in which shard as the module changes, so the shard
    # closest to budget moves too -- shard 4 measured the widest spread here
    # (p50 19.5m, max 23.0m), not shard 3 as in the original report. None of
    # the 19 cancelled shard-jobs in the sample were a genuine timeout: every
    # one completed well short of the old 25m budget, the shape of a
    # superseded-push cancel-in-progress kill, not a wall-clock cutoff (an
    # independent spot-check sample saw cancels as short as 2.1m; this
    # sample's shortest was 5.3m -- both far from a 25m timeout, so the exact
    # range is sample-dependent and not restated here). 35m gives ~45%
    # headroom over the observed max (24.2m) and ~60% over p99 -- enough
    # that ordinary runner variance does not false-positive as a hang, while
    # a genuine hang still
    # fails in under 35 minutes rather than running unbounded.
```

## Comment group 24

Original lines 519–522.

```yaml
          # Pinned: setup-helm installs latest by default, and a newer helm
          # changed values-schema error text (JSON-pointer paths became
          # dot-separated), spontaneously reddening the go/internal/runtime
          # Helm contract tests with no code change. See eshu-hq/eshu#5566.
```

## Comment group 25

Original lines 525–527.

```yaml
      # Pre-warm modules with retries so a dropped proxy stream fails HERE,
      # named for what it is, instead of surfacing as a race-detector red that
      # never ran a test (#6075 follow-up).
```

## Comment group 26

Original lines 531–536.

```yaml
      # go/internal/replay/recorder and go/cmd/collector-aws-cloud run the
      # committed private-data gate library (scripts/lib/
      # cassette_private_data_pattern.sh) in-process, and it shells out to rg.
      # The library reports a missing rg as a broken scan (exit 127) and the
      # tests fail on it rather than skip (#6987 review), so this lane needs
      # ripgrep exactly as go-core does.
```

## Comment group 27

Original lines 570–588.

```yaml
    # Umbrella gate: a single stable status name that is green only when EVERY
    # go-race shard passed. `needs.go-race.result` aggregates the matrix — it is
    # `success` only if all shards succeeded. Point any future required-status-
    # check config at THIS job, not the per-shard legs (whose names carry the
    # shard index and change when N changes).
    #
    # `skipped` is also a pass: on a docs-only PR the `changes` gate skips the
    # go-race matrix, so `needs.go-race.result` is `skipped`. Treating that as
    # green keeps this a single always-reported check whose name is safe to mark
    # required later — it never strands a docs-only PR. Only `failure`/`cancelled`
    # (a shard that actually ran and did not pass) fails the gate.
    #
    # `changes` is an explicit dependency for the same reason `go-core-complete`
    # carries it (#5814): GitHub implicitly prepends `success()` to a job `if:`
    # that does not name a status function, so a `changes` job that FAILS or is
    # CANCELLED marks every downstream shard `skipped` rather than `failure`.
    # Keying only on `needs.go-race.result` would then report GREEN having run
    # no race tests at all. A skipped matrix is only trustworthy as a docs-only
    # skip when `changes` itself succeeded.
```

## Comment group 28

Original lines 610–612.

```yaml
    # Always runs (no changes gate) — this is the docs build that a docs-only PR
    # needs, and it hosts the static guard that the docs-only carve-out wiring
    # above stays intact even on the docs-only PRs it governs.
```

## Comment group 29

Original lines 647–650.

```yaml
          # Pinned: setup-helm installs latest by default, and a newer helm
          # changed values-schema error text (JSON-pointer paths became
          # dot-separated), spontaneously reddening the go/internal/runtime
          # Helm contract tests with no code change. See eshu-hq/eshu#5566.
```
