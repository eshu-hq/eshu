# git

## Purpose

`go/internal/collector/repo/git` is the git repository collector. It answers
"which repositories should we look at, what is in them right now, and what facts
does that produce" — the `sync -> discover -> parse -> emit facts` span of the
pipeline for git sources.

The root `collector` package keeps only the seam every collector kind shares:
`Service`, `Source`, `Committer`, `CollectedGeneration`, and the
`claimed_service*` machinery that roughly fifteen other collector kinds use.
Everything git-specific lives here.

## Layout

```
git/                     selection + snapshot + source (the tangled core)
  model/                 types and the fact-stream writer everything below shares
  docs/                  documentation extraction (markdown, OOXML, diagrams, ...)
  observability/         observability route and metric facts
  codeowners/            CODEOWNERS ownership facts (git-collector hook)
  submodule/             .gitmodules facts and pinned-SHA resolution (hook)
  service/catalog/       service catalog manifest facts (hook)
  tfstate/               Terraform backend-expression warnings
  workflow/image/        CI workflow container image evidence
  membership/            githubOrg repository selection observations (#7625)
  deadline/              per-command git deadlines for the sync loop (#8052)
  resolve/          per-host curloptResolve address cache (#8092)
```

Imports run one way: `git -> leaf -> model`. No leaf imports `git`.

## Why the core is one package

`snapshot_*`, `selection_*` and `source*` reference each other in
production code in all three directions. Splitting them into separate packages
does not compile without first inverting those dependencies, which is a
behaviour-changing refactor rather than a file move. They stay together until
that refactor is done and measured.

Three leaf names intentionally repeat a sibling parser package —
`codeowners`, `submodule`, and the `catalog` under `service/` — because each
is the git-collector hook over that parser: the parser understands the file
format, the hook decides when to run it and how the facts reach the stream.
The hook lives under `git/` and the parser stays outside it
(`collector/repo/codeowners`, `collector/repo/submodule`,
`collector/servicecatalog`), so the path still reads as a sentence. Files
that need both import the hook by its full path.

## Follow-up entity keys

The `shared_followup` facts that enqueue reducer domains carry an `entity_key`
of `<prefix>:<repository ID>`, built by one helper (`followupEntityKey` in
`followup_facts.go`) from `repo.ID`, the same canonical id the repository fact
publishes (`repository:r_<8-hex>`). It is never derived from the checkout path
or the display name: names are not unique across a run and can end in a colon
the alias normalizer cannot match, so name-keyed keys never select. The
reducer's candidate filter and retract matcher compare ids only (#7384); a
legacy name-keyed work item matches nothing and heals on the next full
generation, which re-emits id keys.

## Symlinked checkout roots

File fact ids (`<repository ID>:<relative path>`) never depend on how the
checkout directory was spelled. `model.RepositoryRelativePath` absolutizes
the repository root and the file path and resolves symlinks on both before
`filepath.Rel` (#7998): a checkout reached through a symlinked parent (macOS
`/var` to `/private/var`) emits the same `graph_id` values as the direct
path instead of a run of `../` segments.
`TestFileGraphIDStableThroughSymlinkedCheckoutRoot` snapshots one repository
through both spellings and requires identical file `graph_id` values.

A symlink inside the checkout follows one admission rule in both sync modes
(`symlinkAdmission`, #8809); the rule and its limits are in `AGENTS.md`.

No-Regression Evidence (#7998): the helper runs once per emitted file fact
plus a few documentation and commit-index facts per sync. A throwaway
`go test -bench` (not committed) on linux/amd64 (AMD EPYC 9R14), Go 1.26.9,
`-count=5`, over a real temp checkout measured:

| Build | ns/op (median) | B/op | allocs/op |
| --- | --- | --- | --- |
| Rel only | 106 | 0 | 0 |
| Dual resolve | 9317 | 1256 | 18 |

About 9.3 µs per file, so a 10,000-file sync pays about 0.1 s next to
per-file parse work, and output is byte-identical on plain paths: the
content-version pin digest did not move. No query, queue, graph-write, or
worker path changes.

No-Observability-Change: the change alters only the relative-path string
inside emitted facts. No metric, span, log key, or status field changes.

## Per-command git deadlines

The sync loop runs one repository at a time, so one git command stuck on name
resolution used to pause the whole fleet sync until libcurl gave up after about
300 s (#8052). Every git command that can wait on the network now goes through
`deadline.Run` under a deadline derived from the loop context: `ls-remote` ref
listing (`ls_remote`) and the default-branch lookup (`resolve`) get
`ESHU_GIT_RESOLVE_TIMEOUT_SECONDS` (default 60), `fetch` gets
`ESHU_GIT_FETCH_TIMEOUT_SECONDS` (default 90), and the initial `clone` gets
`ESHU_GIT_CLONE_TIMEOUT_SECONDS` (default 600). Purely local commands
(`checkout`, `rev-parse`, `symbolic-ref`, `cat-file`, `diff`, `status`,
`worktree`, `reset`) run through `gitRun` with no deadline, because a large
checkout can legitimately take long and cannot wait on DNS. `fetch` and
`clone` also carry `-c http.lowSpeedLimit=1 -c http.lowSpeedTime=60`
(`Limits.LowSpeedArgs`), so a transfer stuck at 0 bytes per second aborts by
itself; they are argv options and never touch the credential header in
`GIT_CONFIG_KEY_0`.

On expiry git and its remote-https helper die together and the error names
the deadline and the elapsed time. A deadline kill is one attempt of the
bounded retry (#8091): the command runs again, up to
`ESHU_GIT_RETRY_ATTEMPTS` times and within `ESHU_GIT_RETRY_SWEEP_BUDGET`
retries per sweep, with a `git command attempt failed` WARN per failed attempt.
Only after the last attempt does a clone, default-branch fetch, or ref-listing
failure take the existing isolated path: one
`git repository sync failed` line per repository failure (with
`failure_class=deadline_exceeded`, `deadline_operation`, `deadline_seconds`,
`elapsed_seconds`, `attempts`, `repository_index`), one
`eshu_dp_git_repo_sync_failures_total` increment, and a retry next cycle.
`eshu_dp_git_command_deadline_exceeded_total{operation}` counts one per killed
attempt, so a repository that recovered on its retry counts a kill and has no
sync-failed line. See `deadline/README.md`. A shutdown cancel is the context error, not a deadline. A killed command
adds nothing to `SelectedRepoPaths`, so no generation is built for that
repository this cycle. The shallow.lock retry fetch carries its own deadline. A
pinned-ref fetch that hits its deadline (two fetches per pinned ref, branch
then tag) is counted in `eshu_dp_git_command_deadline_exceeded_total{operation="fetch"}`
and logged in the `failed to fetch pinned ref; skipping` warning, which then
carries `failure_class=deadline_exceeded`, `deadline_operation`,
`deadline_seconds`, `operation` and `elapsed_seconds` (any other pinned-ref
failure keeps `repo_id`, `ref` and `error` only). It is not a
`git repository sync failed` line and is not in
`eshu_dp_git_repo_sync_failures_total`; the repository's other refs and the
cycle continue. A clone that outlives its
deadline fails every cycle, so size `ESHU_GIT_CLONE_TIMEOUT_SECONDS` for the largest repository you index.
See `deadline/README.md`.

The deadlines bound a resolver stall; the address cache removes the lookup
from the fetch path (#8092). The `resolve` subpackage resolves each
provider host once with Go's resolver (5 s by default,
`ESHU_GIT_RESOLVE_CACHE_LOOKUP_SECONDS`) on a single receiver goroutine and
pins every A and AAAA address into clone, fetch, pinned-ref fetch, and both
`ls-remote` calls as `-c http.curloptResolve`, rebuilt per attempt so a retry
after a refresh pins the fresh set. A retry whose attempt died at its deadline
or with a closed curl wording row (resolve, connect, or reset text — never an
HTTP, auth, or pack answer, and never a plain local `operation timed out`)
requests a refresh through the #8091 hook, single-flight per host with a 30 s
minimum gap; an hourly background refresh (`ESHU_GIT_RESOLVE_CACHE_REFRESH_SECONDS`)
travels the same path. A failed refresh keeps the last good set, the first
command for a host falls back unpinned after the lookup deadline
(`eshu_dp_git_resolve_cache_fallback_total{reason="cache_empty"}`), nothing is
pinned under a proxy setting (`reason="proxy_set"`) or for SSH remotes, and
`ESHU_GIT_RESOLVE_CACHE=off` turns the cache off. See `resolve/README.md`.

Performance Evidence (#8092): the pinned path costs one map read per network
command and one lookup per host per hour in place of one lookup per git
process; the issue's shim measured a pinned `ls-remote` at 0.45–0.6 s with DNS
black-holed where the unpinned command failed after about 12 s.
`TestConcurrentRefreshSingleFlight` (resolve, `-race`) proves 32
concurrent failure requests start exactly one lookup, and
`TestNetworkCommandsCarryCurlResolvePin` proves the pin rides clone, fetch,
pinned-ref fetch, and `ls-remote` argv ahead of the subcommand. Terminal
counts are unchanged: the pin only skips curl's own lookup, so the same refs
and packs arrive.

Observability Evidence (#8092): `eshu_dp_git_resolve_cache_lookups_total`,
`eshu_dp_git_resolve_cache_refresh_requests_total`,
`eshu_dp_git_resolve_cache_fallback_total`, and
`eshu_dp_git_resolve_cache_lookup_duration_seconds`, the
`git.resolve_cache.lookup` span, and the `git resolve cache lookup` log lines,
asserted from recorded runs in the resolve tests.

## Directory size

This directory is over the 40-file cap. The gate (`go/cmd/dirgate`) derives its
ratchet from git: the count may not grow past its count at the base unless a
commit carries a `Dirgate-Growth:` trailer, and shrinking needs no action.
Check it with `bash scripts/verify-dirgate.sh --pre-push`.

## Reindex watermark

`NativeRepositorySelector` and `WebhookTriggerRepositorySelector` read the fleet
reindex watermark once per git cycle through `ReindexWatermarkReader` (#7620).
`decideForScope` checks a scope the sweep leaves `fresh` against it and forces
reason `reindex_requested` when the newest activated full predates it. Forcing
shares the sweep's per-cycle budget and throttle. A watermark later than the
cycle's `observedAt` is deferred, because generations are ingested at
`observedAt`. The reader is read-only: the request is never claimed. See
`docs/public/reference/reconciliation-sweep.md#reindex-requests`.

Both selectors also read the per-repository watermarks newer than the cycle's
active fleet watermark (every row when none is active) through
`RepositoryReindexWatermarkReader`, once per cycle
(`resolveRepositoryReindexWatermarks`), and defer each row on its own. A
scope's effective watermark is the later of the two
(`gitDeltaBaseline.reindexWatermarkFor`); the reason is
`repository_reindex_requested` only when the scope's own row is later.
`prioritizeRepositoryReindex` moves requested repositories to the front of
the sync order so the shared per-cycle budget cannot starve them. With the
sweep off and no fleet watermark, `reconcileDue` reads state only for scopes
with a row. This directory is over the dirgate cap, so a new file needs a
growth trailer. The code lives in `selection_reconcile_config.go` and
`selection_baseline.go`, not a new file.

Deltas are cumulative (#8780, `selection_cumulative_delta.go`). The resolver's
`DeltaSyncBaseline` (or the same cycle's `FullReconcileState`) gives the
active commit and the newest activated full's commit F. `updateRepository`
diffs the changed set from F, deletes `D(F..head) ∪ D(active..head)`, and
takes a full on `no_projected_full`, an unreachable commit, or the growth
valve (`delta_exceeds_base_half` at half of F's `git ls-tree` count).
Evidence: `docs/internal/evidence/8780-cumulative-delta.md`.

An unchanged forced reconcile can skip the parse (#7978 follow-up, ruling
a9-143). After the fetch, `skipParse` (`selection_cli.go`) compares the
pre-parse identity (`preparse.Identity`, built from `PreparseConfigFor`) with the
active generation's; on a match it calls the resolver's
`ReprojectActiveGeneration` and selects nothing. Every full, clean selection
carries its identity on `SelectedRepository.PreparseIdentity` to the generation.
The cmd wiring passes one `Preparse` source to the selectors, built from the
snapshotter it also wires. See `preparse/README.md`.

An expired pending full holds its scope (#7978, ruling a9-126). When `decide`
forces `in_flight_expired` or `reconcile_request_stale` over a newest full that
is pending with live projector work, `expiredPendingHoldHead` returns that
full's commit. `updateRepository` compares it with the remote head after the
fetch. At the same head the sync returns `GitSyncDelta.ReconcileHeld`: no
snapshot, no parse, and no per-cycle budget slot, counted as
`in_flight_expired_held` with an INFO `git_reconcile_held` log. Every entry
path (scheduled sync, sweep, webhook) goes through this loop, so each has the
remote head when it holds.

A scheduled sweep yields to a queued webhook trigger (#8123). When the
cmd wiring sets `NativeRepositorySelector.SweepYieldProbe` (ingester and
collector-git, only with the handoff on), `syncGitRepositoriesWithLogger`
probes the durable queue every `ESHU_WEBHOOK_TRIGGER_SWEEP_YIELD_CHECK_REPOS`
repositories (default 16) and stops at the next repository boundary when an
accepted trigger is queued. The partial selection returns as a normal batch;
`NextReconcileStartOffset` resumes past the last visited repository and the
next walk is limited to the unvisited tail, so one logical sweep fetches each
repository once — including pinned reindexes: a resumed walk skips the pins
the sweep already fetched (watermark rows persist, so the prefix would
otherwise re-fetch on every resume), while pins requested after the yield
stay at the head and keep their queue-jump. Yields per logical sweep are
bounded by
`ESHU_WEBHOOK_TRIGGER_SWEEP_MAX_YIELDS` (default 8); then the walk completes
unstopped, so a trigger stream cannot starve coverage. The probe never
claims: the webhook selector claims at the next cycle under the existing
lease. A probe error completes the sweep as before and counts
`eshu_dp_webhook_store_operations_total` with outcome `failed` (provider and
status `unknown`), so yield-disabled-by-outage alerts instead of hiding
behind the quiet queue. The claim side records
`eshu_dp_webhook_trigger_claim_wait_seconds`; each yield records
`eshu_dp_collector_sweep_yields_total` and the INFO `git repository sweep
yielded to queued webhook trigger` log with the resume point.

Performance Evidence (#7620, per-repository): the read is one sequential scan
per cycle per shard, about 1 ms per 10,000 rows on PostgreSQL 18. Ordering
derives a scope ID per repository only when at least one row is active, using
`gitScopeIDForManagedRepo`, which does path and string work with no disk or
network I/O. `TestRepositoryReindexTargetedScopeIsNotStarved` fails without
the reordering (the requested 30th repository is not forced in the first
cycle with a budget of 10) and passes with it, still forcing exactly 10.

## Default branch tracking

`git clone` records `refs/remotes/origin/HEAD` once, and the single-branch
fetch never refreshes it. `fetchDefaultBranch` (`selection_delta.go`) adds
`+HEAD:refs/eshu/remote-head` to the branch fetch (#7678). A probe commit
different from the tracked tip (`moved`), or a fetch that fails with
`couldn't find remote ref refs/heads/<branch>` (`missing_ref`), triggers
`ls-remote --symref origin HEAD`. A different branch is fetched first, and
only then is `origin/HEAD` repointed, so a failed fetch is detected again on
the next sync. `updateRepository` then takes a full snapshot with
`skip_reason=default_branch_changed` and logs the WARN
`git repository default branch changed`. A remote `HEAD` that names a missing
branch fails the probe; the sync drops it and fetches the tracked branch as
before. Detection matches
git's stderr text, so `gitCommandEnv` sets `LC_ALL=C` on every managed git
command.

A forced reconciliation or a failed baseline lookup passes no fallback
callback, so a change found there is logged but not counted in
`skip_reason=default_branch_changed`. The returned `GitSyncDelta` sets
`DefaultBranchChanged`; the sync keeps that path-less entry in
`DeltaByRepoPath`, and `buildSelectedRepositories` copies the flag to the
default-branch `SelectedRepository` only, never to a pinned-ref entry. The
fact builder sets `ScopeGeneration.DefaultBranchChanged` and the v3 hint folds
the default branch, so the store always stores that generation (#7978) and a
rename that kept the tree does not leave the old `default_branch` on the
repository fact. It is not a reconciliation: unless a sweep reconciliation lands on the
same cycle, `reconciliation_generation` stays unset, so the old branch's
removals do not count as drift on
`eshu_dp_reconciliation_drift_retractions_total`, and no sweep budget or
reconciliation counter is spent. One gap remains: when `list_refs` fails on
the cycle that adopts a same-tree rename, `origin/HEAD` already names the new
branch, so the next cycle is a no-op and `default_branch` waits for the next
content change.

Performance Evidence (#7678): the extra refspec rides the existing fetch.
Scratch remotes measured 2 protocol requests over `file://` with or without
it on git 2.56 and 2.47.2, and 3 HTTPS requests in steady state against
GitHub with or without it. The unchanged path adds no process: the tracked
tip and the probe resolve in one `rev-parse`. Two unusual remotes cost more
each cycle: a remote `HEAD` that names a missing branch costs a second fetch,
and a detached remote `HEAD` (a bare commit, no branch) at a commit other than
the tracked tip costs one `ls-remote`.

Observability Evidence (#7678): before this change the only signal was the
repeating sync error `couldn't find remote ref refs/heads/<branch>`, and a
moved default branch with the old branch kept produced no signal at all. A
detected change now emits the WARN `git repository default branch changed`
(`previous_branch`, `branch`, `detection`) and, on the incremental path, one
increment of `eshu_dp_collector_delta_baseline_fallback_total` with
`skip_reason=default_branch_changed`; `TestUpdateRepositoryFollowsDeletedDefaultBranch`
and `TestUpdateRepositoryFollowsMovedDefaultBranch` assert both against a real
git remote. Steady-state syncs emit nothing new.

## Repository selection observations

In githubOrg mode, `NativeRepositorySelector` hands the full pre-shard org
listing to its `SelectionObserver` right after discovery, on shard 0 only
(#7625). `githubOrgSelectionRequest` maps each listed repository to the scope
ID a sync of it would write (`gitScopeIDForRepositoryID`) and to a state:
`selected`, `archived_excluded`, or `rule_excluded`. The `membership`
subpackage compares that listing with the org's known repository scopes and
records `not_listed` evidence; it never deletes, hides, or writes the graph.
In explicit mode, shard 0 hands the full pre-shard configured list to the
observer as one all-`selected` listing per owner
(`explicitSelectionRequests`), with scope IDs and slugs from
`gitScopeIdentityForRepositoryID`; only configured repositories that already
have scopes get rows. In a cycle that issues any request, exactly one carries
`SweepExpired` (the githubOrg request, or the last explicit owner's), so the
expired-row sweep runs at most once per cycle; filesystem mode, an explicit
list with no owners, and a cycle whose discovery fails issue none.
`listGitHubOrgRepositories` requests every page at `per_page=100`, because
GitHub pages by offset and a smaller page would re-read earlier repositories,
and trims to `ESHU_REPO_LIMIT` client-side. Only an empty page or the limit
ends the listing; a short page mid-listing keeps paging, so the repositories
after it still sync. It reports the listing complete only when an empty page
arrives before the limit without a `Link` `rel="next"`; a listing that
reaches the limit is truncated even when the org holds exactly that many
repositories, so a listing cut at `ESHU_REPO_LIMIT` is never evaluated.
A store failure is logged and counted, and the cycle carries on. The webhook
selector never observes. Telemetry: the
`eshu_dp_collector_repository_selection_evaluations_total` counter, the
`eshu_dp_collector_repository_selection_scopes` gauge, the
`eshu_dp_collector_repository_selection_observations_deleted_total` counter
for the expired-row sweep (#7774), and the `git_repository_selection_*` logs;
see `membership/README.md`.

## GitHub API retry and discovery failures (#8084)

The org listing (`fetchGitHubListingPage`) and the GitHub App token mint
(`mintGitHubAppToken`) share one retry helper, `rest.Do`, in the
`rest` subpackage; see `rest/README.md` for the fault classes, the
constants, and the telemetry. `RepoSyncConfig.GitHubAPI` (a
`rest.Settings`) carries the REST base, which defaults to
`https://api.github.com`, and the retry budget. It has no environment
variable; a test sets it to an `httptest` server and an injected clock.

A retryable failure that outlasts the in-call budget returns
`*rest.RetryableError`, which implements `collector.RetryableError` and
unwraps to the underlying cause. One `rest.Failures` per process is built by
the wiring (`cmd/ingester`, `cmd/collector-git`) and set as `DiscoveryFailures`
on both `NativeRepositorySelector` and `WebhookTriggerRepositorySelector`. It
counts per operation, `list` and `mint`, each with the 20-cycle cap
(`sdk.MaxConsecutiveTransportFailures`) and its own backoff, so a good mint
cannot hide a dead listing. The native selector resets both operations only
when `SelectRepositories` returns nil. The webhook selector resets `mint` only
after a sync of a non-empty repository set, and a webhook batch never resets
`list`. At the cap the owner returns a plain error that names the operation,
class and count, so a permanent fault still ends the process.

On the webhook path a retryable sync failure (the token mint inside the sync)
is counted against the shared counter and skips the cycle. The claimed
triggers are not marked failed: `failed` is terminal, so marking would lose
them to a GitHub blip. They stay `claimed` and the lease reaper requeues them
once per lease window (`ESHU_WEBHOOK_TRIGGER_CLAIM_LEASE_WINDOW`, 15 minutes by
default) until the claim-attempt cap (3 by default) is reached. The bound: a
claimed trigger survives an outage of about three lease windows, 45 minutes
with the defaults (requeued after the first and second window, exhausted after
the third). A longer outage still ends it as `failed` with
`claim_lease_exhausted`, a class that names a poison row, not the provider
outage. It is visible in
`eshu_dp_webhook_trigger_claim_reaps_total{outcome="exhausted"}`, the
`reaped expired webhook trigger claims` log, and the stale-claims gauge. Base
marked every claimed trigger failed on the first blip. In priority mode the
scheduled native sync picks the repository up on its next successful listing,
if the native selection includes it; in webhook-only mode the change waits for
the next push. A
webhook selector built without `DiscoveryFailures` strips the skip and stays
fatal, and a sync failure joined with a failing `MarkTriggersFailed` write is
a plain fatal error.

A failed discovery leaves `reconcileWalkOffset` alone, reads no reindex
watermark, and makes no selection observation. The collector loop that waits
`RetryAfter` instead of exiting is `collector.Service.Run`; this package only
classifies and counts.

## Git auth host scope

`gitCommandEnv` builds the auth environment for every managed git command. With
`ESHU_GIT_AUTH_METHOD=token` it scopes the credential header to one host, the
host of the repository the command runs against. It derives that host from the
checkout path (`<ReposDir>/<repoID>`) the same way `repoRemoteURL` builds the
clone URL, so the header and the remote always name the same host (#7763):
github.com with username `x-access-token`, the GitLab host with `oauth2`, or
bitbucket.org with `x-token-auth`. The GitLab host is `gitlab.com` unless
`ESHU_GITLAB_HOST` sets a self-managed host (#7764); `repoProviderHost` is the
single host source for both, so the header follows the configured host. A path
with no provider prefix resolves to github.com, as `repoRemoteURL` does. A path that is not a managed checkout gets
no credential header: `tokenAuthProvider` applies the same `repoCheckoutName`
rule the clone path uses, so a path outside `ReposDir` (no repository ID) and
the reserved `.eshu-ref-worktrees` namespace (never cloned; only local commands
run there) fail closed instead of defaulting to github.com. The skip has no
runtime signal, because the function cannot tell a local command from a network
one and ref worktrees run local commands every cycle. The fail-closed rule
applies to `token` mode only: a `githubApp` token is scoped to github.com from
any path, because it authenticates nowhere else.
`TestGitCommandEnvTokenHeaderMatchesCloneURLHost` asserts the header host
matches the clone URL host for each provider, and
`TestGitCommandEnvTokenAuthUnmanagedPathSendsNoHeader` asserts the fail-closed
cases.

No-Regression Evidence: #7763 adds one `filepath.Abs`/`filepath.Rel`, a string
split, and a `repoCheckoutName` validation to `gitCommandEnv`, which runs once
per git child process. A throwaway `go test -bench` (not committed) of
`gitCommandEnv` in token mode with `ReposDir=/data/repos`, `-count=5`, on
darwin/arm64 (Apple M5) measured:

| Build | Checkout path | ns/op (median) | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Baseline `0a65fccd0`, Go 1.26.6 | (no path argument) | 760 | 2273 | 6 |
| Host scope, Go 1.26.6 | `/data/repos/acme/app` | 1169 | 2410 | 10 |
| Host scope, Go 1.26.6 | `/data/repos/gitlab/example-org/payments/example-web-app` | 1616 | 2618 | 12 |
| Host scope and fail-closed, Go 1.26.3 | `/data/repos/acme/app` | 1355 | 2554 | 13 |
| Host scope and fail-closed, Go 1.26.3 | `/data/repos/gitlab/example-org/payments/example-web-app` | 1949 | 2802 | 15 |
| Host scope and fail-closed, Go 1.26.3 | `/data/repos/.eshu-ref-worktrees/gitlab/acme/app/main` | 1739 | 2482 | 8 |

The two toolchain patch levels were not run side by side, so deltas between
the row groups are approximate. The whole function costs about 2 µs next to
the git process it configures. A local `git rev-parse HEAD` spawn took a median
of 12.3 ms over 50 runs on the same host, and clone and fetch are
network-bound, so the full cost is under 0.02% of the cheapest git command. No
query, queue, graph-write, or worker path changes.

No-Observability-Change: the change alters only the environment handed to the
git child process. No metric, span, log key, or status field changes, and the
token still appears only in that child environment, never in logs.

No-Regression Evidence (#7764): the configurable host adds one argument and
one call indirection to `repoRemoteURL`, one empty-check for gitlab IDs, and
one hostname normalization per process start. A throwaway `go test -bench`
(not committed) of `repoRemoteURL` with `-benchtime 300000x -count 3`, Go
1.26.2 linux/amd64, same shared host back to back, measured:

| Build | Case | ns/op (median) | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| Baseline `22ba1ed133` | github https | 225 | 104 | 4 |
| After (this branch) | github https | 235 | 104 | 4 |
| Baseline `22ba1ed133` | gitlab https | 350 | 200 | 6 |
| After (this branch) | gitlab https | 362 | 200 | 6 |
| Baseline `22ba1ed133` | gitlab ssh | 359 | 200 | 6 |
| After (this branch) | gitlab ssh | 365 | 200 | 6 |
| Baseline `22ba1ed133` | bitbucket https | 355 | 224 | 6 |
| After (this branch) | bitbucket https | 373 | 224 | 6 |

Allocations are identical and the deltas (+6 to +18 ns, 2-5%) sit inside
shared-host run-to-run noise. The bench ran on this branch before this note
was committed, and no production file changed after, so the numbers describe
the merged head. The function runs once per repository per git command, so
~0.36 µs next to a millisecond-scale process spawn and a network-bound clone
or fetch. No query, queue, graph-write, or worker path changes. A host
change also alters the pre-parse identity (`Config.GitlabHost`, folded by
`PreparseConfigFor`), so scopes re-parse instead of reusing a snapshot
stamped under another host.

No-Observability-Change (#7764): no metric, span, log key, or status field
is added or renamed. The configured host already surfaces in the clone URLs
and git error text the collector logs, and an invalid `ESHU_GITLAB_HOST`
fails startup with an error naming the variable.

## Two-phase content

Snapshotting collects content file *metadata* first (bodies are temporary), then
re-reads each body from disk at emit time. Memory stays proportional to a single
file rather than the whole repository. `model.ContentFileMeta` is the phase-A
record; `model.ContentFileSnapshot` carries a body.

## Verification

```bash
cd go && go test ./internal/collector/... -count=1
cd go && go build ./... && go vet ./...
```

Anything that changes emitted facts also needs the golden-corpus gate (B-7) and
a byte-identical B-12 snapshot — see
`docs/public/reference/local-testing/golden-corpus-gate.md`.
