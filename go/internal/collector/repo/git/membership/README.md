# membership

## Purpose

`go/internal/collector/repo/git/membership` records whether each known
repository scope of a GitHub org is still a member of the org listing the
collector selects from (#7625). The rows are repository selection
observations. A scope that drops out of the listing (transferred, renamed, or
deleted upstream) keeps its last projected graph today; these observations are
the evidence a later freshness verdict reads. The package records only. It
never deletes, hides, or retires a scope and never writes the graph.

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
          ^
    this package (githubOrg listing, shard 0 only)
```

`git.NativeRepositorySelector` calls `Observer.Observe` right after githubOrg
discovery, with the full pre-shard listing, on shard 0 only. Every shard lists
the same org, so a single writer avoids N-way races on the same rows. The
webhook selector never observes: a webhook batch is not a listing.

In explicit mode the selector calls `Observe` once per owner of the full
pre-shard configured list, also on shard 0 only, with a complete listing in
which every repository is `selected`. Each repository carries the scope ID and
repo slug a sync of it writes, and its owner is the slug's first segment. The
explicit selector (`NewExplicitSelector`, kind `explicit`) writes a `selected`
row only for a configured repository that already has a scope. It never writes
`not_listed` or excluded rows, so it skips the mass-miss guard, and it does not
sample the scope gauge. Filesystem mode and bootstrap-index never observe.

## Exported surface

- `NewGitHubOrgSelector`, `Selector`, `Rule`: the selector identity. The id
  hashes the source mode, the lowercased org, the sorted normalized rules, and
  the include-archived flag, so two configurations on one org never share rows.
- `Evaluate`, `Input`, `Result`, `Counts`, `ScopeCounts`: the pure evaluation.
- `Observer`, `Store`, `Request`: the per-cycle wrapper and its storage port.
  The Postgres implementation is
  `go/internal/storage/postgres/membership`.
- `Observation`, `Row`, `Batch`, `State`, `Confirmed`: the row contract.
  `State` aliases `selection.State` and `Confirmed` delegates to
  `selection.Confirmed` (`go/internal/scope/selection`), the definition the
  repository freshness `not_selected` verdict also uses.

## Evaluation rules

| Rule | Behavior |
| --- | --- |
| Complete listing only | `Listing.Complete == false` writes nothing; outcome `listing_truncated`. |
| Owner partition | Known scopes are the org's `repository` scopes, matched case-insensitively on the repo slug org. `repository_ref` scopes are excluded. |
| Mass-miss guard | More newly unlisted scopes than `max(10, ceil(0.10 * known))`, or an empty listing while known scopes exist, writes nothing; outcome `guard_tripped`. |
| Confirmation | `not_listed` is `Confirmed` only when `unlisted_cycle_count >= 2` and `evaluated_at - first_unlisted_at >= evaluation_interval`. |
| Relist | A listed scope resets `first_unlisted_at` and `unlisted_cycle_count`. |
| Replay | An upsert with an `evaluated_at` that is not newer than the stored row changes nothing. |
| Store errors | Never fail the cycle; logged and counted as `store_error`. |

The collector has no configured cycle interval (it loops back to back). The
evaluation interval is therefore `max(MinimumInterval, now - newest prior
evaluated_at for the selector)`, truncated to whole seconds, with
`DefaultMinimumInterval` of 5 minutes. Confirmation then needs two cycles and
at least the floor, and tracks the actual cadence when cycles are slow.

`project` mirrors the SQL counter math in the Postgres upsert so the state
gauge needs no read-back. The live store test asserts the two agree.

## Telemetry

- Counter `eshu_dp_collector_repository_selection_evaluations_total`
  `{collector_kind="git",outcome}`, outcomes `evaluated`, `listing_truncated`,
  `guard_tripped`, `store_error`.
- Gauge `eshu_dp_collector_repository_selection_scopes`
  `{collector_kind="git",state}`, states `selected`, `not_listed_pending`,
  `not_listed`, `archived_excluded`, `rule_excluded`. Sampled only on
  `evaluated` cycles.
- Logs: INFO `git_repository_selection_evaluated` every cycle, carrying the
  outcome and counts; plus WARN `git_repository_selection_guard_tripped`,
  `git_repository_selection_listing_truncated`, or
  `git_repository_selection_store_failed` (with `failure_class`) when the
  outcome is not `evaluated`.

## Evidence

Performance Evidence (#7625): the work runs once per githubOrg cycle on shard
0 only, after the paged GitHub listing (one HTTP request per 100 repositories).
On an Apple M1 Max, `BenchmarkEvaluateQAFixtureSteadyState` (802 known scopes,
779 listed, full prior projection) measured 181–183 µs/op, 432 KB/op,
19 allocs/op over three 2 s runs. `BenchmarkGitHubOrgSelectionRequest` in the
git package (800 listed repositories, one scope ID derivation each, no disk or
network I/O) measured 3.27–3.36 ms/op. The store adds two reads and one upsert
per cycle; see `go/internal/storage/postgres/membership/README.md`. Explicit
mode runs the same two reads and one upsert once per configured owner, over
only the configured repositories. Other shards and filesystem mode run none of
it.

Observability Evidence (#7625): every cycle increments
`eshu_dp_collector_repository_selection_evaluations_total` with its `outcome`
and logs the INFO `git_repository_selection_evaluated` summary with that
outcome; the three non-`evaluated` outcomes also log their WARN. A silent
shard 0 is visible as a missing counter series. `evaluated` cycles also
set `eshu_dp_collector_repository_selection_scopes` per `state`. Store
failures carry `failure_class` (`known_scopes_read`, `observations_read`,
`upsert`, `store_missing`).

## Verification

```bash
cd go && go test ./internal/collector/repo/git/... -count=1
cd go && go test -race ./internal/collector/repo/git/membership -count=1
```
