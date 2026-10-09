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

- `NewGitHubOrgSelector`, `NewExplicitSelector`, `Selector`, `Rule`,
  `GitHubAppPrincipal`, `TokenPrincipal`: the selector identity. The id hashes
  the kind, source mode, lowercased owner, sorted normalized rules, and the
  include-archived flag, then appends the credential principal, so two
  configurations or credentials on one owner never share rows.
- `Evaluate`, `Input`, `Result`, `Counts`, `ScopeCounts`: the pure evaluation.
- `Observer`, `Store`, `Request`: the per-cycle wrapper and its storage port.
- `KnownScopeHost`: the remote host a selector kind's known scopes must carry
  (`github.com` for `github_org`, none for explicit).
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
| Owner partition | Known scopes are the org's `repository` scopes, matched case-insensitively on the repo slug org. A `github_org` selector also requires the stored `remote_url` host to be `github.com` (`KnownScopeHost`), so a gitlab.com or GitHub Enterprise scope with the same slug owner is never judged by a github.com listing. Explicit selectors use no host filter. `repository_ref` scopes are excluded. |
| Mass-miss guard | More newly unlisted scopes than `max(10, ceil(0.10 * known))`, or an empty listing while known scopes exist, writes nothing; outcome `guard_tripped`. |
| State tracking | Each row stores `state_since` and `state_cycle_count`. The same state as the stored row keeps `state_since` and adds one to the count; a different state, or a new row, sets `state_since` to this evaluation and the count to 1. |
| Confirmation | Any state other than `selected` (`not_listed`, `archived_excluded`, `rule_excluded`) is `Confirmed` only when `state_cycle_count >= 2` and `evaluated_at - state_since >= ConfirmationMinSpan` (5 minutes). A first evaluation never confirms anything. |
| Liveness | Every row stores `liveness_window_seconds` from `ESHU_REPO_SELECTION_LIVENESS_WINDOW` (default 48h, minimum 1h). A row counts while `evaluated_at + window >= now`. The window does not depend on the gap between cycles. |
| Replay | An upsert with an `evaluated_at` that is not newer than the stored row changes nothing. |
| Store errors | Never fail the cycle; logged and counted as `store_error`. |

The selector id is `<config hash>@<principal>`. The principal names the
credential that listed the repositories: `app:<app_id>:<installation_id>` for
a GitHub App, `token:` plus the first 16 hex characters of a salted SHA-256 of
the token, or `anonymous`. The token itself never appears in an id, a log, or
an error. A token rotation starts a new selector; the old one's rows expire
after the window, and any live `selected` row from either keeps the scope
selected during the overlap.

`project` mirrors the SQL state math in the Postgres upsert so the state
gauge needs no read-back. The live store test asserts the two agree.

Known limits: filesystem-mode collectors write no rows, so a repository only
they ingest is covered only by the freshness rule that no generation was
observed after the exclusion began. If a repository's other producer stops
without its selector's rows expiring first, the scope fails open to `unknown`
or keeps its last evidence label until the window passes. GitHub Enterprise
and other non-`github.com` orgs are not evaluated: their scopes get no
`github_org` rows and read `unknown`.

## When the guard trips every cycle

A tripped guard writes nothing, so the missing scopes are still "newly
unlisted" on the next cycle and the guard trips again. A small org reaches this
quickly: the threshold is `max(10, ceil(0.10 * known))`, so with 40 known
scopes eleven missing repositories hold every write.

What the operator sees each cycle: WARN `git_repository_selection_guard_tripped`
with `selector_id`, `listed_count`, `known_scope_count`,
`newly_unlisted_count`, and `guard_threshold`; the INFO
`git_repository_selection_evaluated` line with `outcome=guard_tripped` and up
to ten missing slugs in `not_listed_sample`; and the evaluation counter rising
under `outcome="guard_tripped"`. The scope gauge keeps its last `evaluated`
sample. Because the selector's rows are no longer refreshed, they expire after
the liveness window. Unless another selector still has live rows for them, the
org's scopes then read selection `unknown`, which leaves their freshness
verdict where it was before #7625. The expired-row sweep never deletes the
selector's `not_listed` rows, so the scopes it had already confirmed missing
still do not count as newly unlisted once access returns, however long the
guard tripped.

To confirm the cause, check the `not_listed_sample` slugs on GitHub:

- If the repositories still exist in the org, the listing lost access. Usually
  the GitHub App installation's repository access was narrowed or the token
  lost scope. Restore access. The next cycle lists them again and the guard
  clears. This is the false positive the guard exists to stop.
- If they really were transferred, renamed, or deleted, the departure is real.
  This phase has no override and no knob for it. The thresholds are the
  `guardMinimum` and `guardFraction` constants in `evaluate.go`, not
  configuration, and no supported operation removes a repository scope from
  `ingestion_scopes`. Repository removal is the later tombstone phase in
  `docs/public/reference/hosted-retention-deletion-policy.md`. Until then the
  guard keeps tripping and those scopes stay `unknown`. Narrowing the
  repository rules does not help. Rules change which listed repositories are
  selected, not which known scopes are missing from the listing, and a rule
  change starts a new selector with no prior rows.

## Telemetry

- Counter `eshu_dp_collector_repository_selection_evaluations_total`
  `{collector_kind="git",selector_kind,outcome}`, selector kinds `github_org`
  and `explicit`, outcomes `evaluated`, `listing_truncated`, `guard_tripped`,
  `store_error`. The `explicit` kind only reports `evaluated` and
  `store_error`.
- Gauge `eshu_dp_collector_repository_selection_scopes`
  `{collector_kind="git",state}`, states `selected`, `not_listed_pending`,
  `not_listed`, `archived_excluded`, `rule_excluded`. Sampled only on
  `evaluated` cycles.
- Logs: INFO `git_repository_selection_evaluated` every cycle, carrying the
  outcome and counts; plus WARN `git_repository_selection_guard_tripped`,
  `git_repository_selection_listing_truncated`, or
  `git_repository_selection_store_failed` (with `failure_class`) when the
  outcome is not `evaluated`. The INFO line also carries `selector_kind`,
  `evaluation_gap_seconds`, and `liveness_window_seconds`.
- WARN `git_repository_selection_liveness_lapsed` (`evaluation_gap_seconds`,
  `liveness_window_seconds`) when the gap since the selector's previous
  evaluation exceeds the window: its rows had expired and read `unknown`
  until this evaluation.
- Counter `eshu_dp_collector_repository_selection_observations_deleted_total`
  `{collector_kind="git"}` (#7774): expired rows the sweep deleted. The INFO
  line of the request that swept carries `expired_deleted_count`, zero on
  every other request; a failed sweep logs
  `git_repository_selection_store_failed` with `failure_class=expired_sweep`
  and keeps the cycle's outcome.

## Expired-row sweep

The collector marks exactly one request per cycle with `Request.SweepExpired`:
the githubOrg request, or the last owner's request in explicit mode. After that
request's `evaluated`, `guard_tripped`, or `listing_truncated` outcome, `Observer` calls
`Store.DeleteExpiredObservations(now, ExpiredObservationGrace)` once. That
deletes rows of every selector whose `evaluated_at` plus their own liveness
window plus `ExpiredObservationGrace` has passed (#7774), except `not_listed`
rows, which are never deleted. Rows orphaned by a credential rotation, rules
change, or owner change are the target. A failed store read or a failed upsert
on that request skips the sweep for the cycle. A truncated listing still
sweeps: it writes no rows, and the predicate is time-based and never reads the
listing, so an `ESHU_REPO_LIMIT` held below the org size cannot stop the drain.
While truncation lasts, the selector's own rows age out too and, past their
window plus the grace, are deleted like those of a paused selector (see
below); they already read `unknown` once expired.

`not_listed` rows stay because the mass-miss guard reads them: a scope whose
prior row is `not_listed` is not newly unlisted, and a relist is counted only
against one. Deleting that history could hold a recovered selector's guard
tripped forever. Deleting any other prior row changes no guard count, since a
missing prior and a prior in another state both count the scope as newly
unlisted. What a selector does lose past its window plus the grace:

- A returning `archived_excluded` or `rule_excluded` scope restarts at one
  cycle and reads pending until confirmed again. That errs toward no
  `not_selected` verdict, never a wrong one.
- `liveness_lapsed` may not fire on its first evaluation, because
  `PreviousEvaluatedAt` reads only the rows that remain.

The grace still matters for those: a selector that resumes inside it keeps
its full history and logs `liveness_lapsed`. Explicit selectors write no
`not_listed` rows, so the exception never applies to them. The cost is that
an abandoned selector's `not_listed` rows stay forever. They are bounded by
the selectors ever created times the scopes each had unlisted, freshness
reads only live rows, and `Evaluate` reads only its own selector.

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
`upsert`, `store_missing`, `expired_sweep`).

## Verification

```bash
cd go && go test ./internal/collector/repo/git/... -count=1
cd go && go test -race ./internal/collector/repo/git/membership -count=1
```
