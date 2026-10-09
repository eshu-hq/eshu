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

## Directory size

This directory is over the 40-file cap and carries a row in
`scripts/lib/dirgate-grandfather.tsv`. That row is a monotonic ratchet: the gate
fails if the count grows OR shrinks without the row being re-pinned in the same
change, so every future extraction has to lower it. Recompute with:

```bash
bash scripts/dev/precommit-go.sh dirgate-digest internal/collector/repo/git
bash scripts/generate-dirgate-grandfather-go.sh
```

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
with a row. This directory is pinned at its file count by the dirgate ledger,
so the code lives in `selection_reconcile_config.go` and
`selection_baseline.go`, not a new file.

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
fact builder then empties the generation's freshness hint: the snapshot hint
does not fold git refs, so a rename that kept the tree would otherwise be
dropped as unchanged and leave the old `default_branch` on the repository
fact. It is not a reconciliation: unless a sweep reconciliation lands on the
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
have scopes get rows.
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
`eshu_dp_collector_repository_selection_scopes` gauge, and the
`git_repository_selection_*` logs; see `membership/README.md`.

## Git auth host scope

`gitCommandEnv` builds the auth environment for every managed git command. With
`ESHU_GIT_AUTH_METHOD=token` it scopes the credential header to one host, the
host of the repository the command runs against. It derives that host from the
checkout path (`<ReposDir>/<repoID>`) the same way `repoRemoteURL` builds the
clone URL, so the header and the remote always name the same host (#7763):
github.com with username `x-access-token`, gitlab.com with `oauth2`, or
bitbucket.org with `x-token-auth`. A path with no provider prefix resolves to
github.com, as `repoRemoteURL` does. A path that is not a managed checkout gets
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
