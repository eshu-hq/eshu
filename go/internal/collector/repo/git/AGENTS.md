# AGENTS.md — go/internal/collector/repo/git

## Read first

1. `go/internal/collector/repo/git/README.md` — layout, the import direction, and
   why the core is one package
2. `go/internal/collector/repo/git/doc.go` — the package contract
3. `go/internal/collector/README.md` — the collector seam this package plugs into
4. `go/internal/collector/repo/git/model/README.md` — the shared types

## Import direction is the invariant

`git -> leaf -> model`, never the reverse. A leaf that needs something
from `git` is telling you one of two things:

- the thing is shared, and belongs in `model`; or
- the thing is snapshot/selection logic that ended up in the wrong file, and
  belongs here.

Both have happened already. `commitSHAByRelativePath` was living in the
observability emitter and reads `RepositorySnapshot`, so it moved here.
`documentationFileMetasForPaths` was living in a snapshot file and is pure
documentation logic, so it moved into `docs`. Prefer moving the misfiled
function over exporting a new symbol.

## Do not split the core on prefix alone

`git_snapshot_*`, `git_selection_*` and `git_source*` look like three families
and are one. The three-way production import cycle between them is measured, not
assumed. Before proposing a split, prove the cycle is gone — a filename prefix
is not evidence.

## Facts are a contract

Emission changes projected truth. A new fact kind, a changed payload shape, or a
different fact count needs the cassettes and the B-12 snapshot updated in the
same change, and the golden-corpus gate re-run. Load `eshu-golden-corpus-rigor`
and `eshu-contract-rigor` first. A restructure that changes projected truth is a
bug, not a new baseline.

A delta generation must carry the commit it was diffed from
(`GitSyncDelta.BaselineCommitSHA` through `ScopeGeneration.DeltaBaselineCommitSHA`,
#7319); the projector refuses a delta whose baseline is not the active commit,
and `validateGenerationInput` rejects a delta without one.

`UncoveredProjectionWriters` (#7389) is a reconcile reason, not a fallback:
`decideForScope` probes it only when the sweep is not due and not throttled,
inside the per-cycle budget, and `decideGraphDirty` shares `throttle` with
`decide` (never the fresh check). A graph_dirty scope must go through the
forced-reconcile path so the reason reaches `ScopeGeneration.ReconcileReason`;
the store then re-projects a matching active generation (#7978). Never blank
the hint: bump `gitGenerationContentVersion` when emitted facts change for
unchanged input without a parser-binding or schema change
(`TestGenerationContentVersionPinsCollectorOutput` fails until you do, then
re-pin), or the store drops the new output as a duplicate. A parser
registration change moves the folded fingerprint on its own (#7986). The `deltaFallback*` constants are the closed
`skip_reason` set and the `reconcileReason*` constants the closed reconcile
`reason` set; add a value there and in the telemetry docs together.

The generation estimate is assembled from per-family pre-count functions. If you
change what an emitter sends, change its pre-count in the same edit.

## Follow-up entity keys come from the repository ID

Every `shared_followup` envelope in `followup_facts.go` builds its `entity_key`
through `followupEntityKey(prefix, repoID)`, and `streamFacts` feeds it
`repo.ID`: the same canonical id the repository fact publishes
(`repository:r_<8-hex>`). Never derive a key from the checkout path
(`filepath.Base(repoPath)`), the display name, or the path basename, and never
build the string inline. In dependency mode `ESHU_BOOTSTRAP_PACKAGE_NAME` sets
the name, so the path basename differs; names are not unique across a run and
can end in a colon the alias normalizer cannot match, so name-keyed keys never
select. The reducer's candidate filter and retract matcher compare ids only
(#7384); a legacy name-keyed work item matches nothing and heals on the next
full generation, which re-emits id keys. A new follow-up envelope takes
`repoID` and calls the helper; add its domain to `followupKeyPrefixes` in
`followup_key_test.go` so the count assertion covers it.

## Every git spawn that can wait on the network is deadline-bound

Run a network command through `gitRunRemote` (`ls-remote`), `gitFetchBranch` or
`gitFetchPinnedRef` (`fetch`), or `deadline.RunRetrying` with a
`deadline.Command` whose `Build` makes the command from the context it hands you
(`cloneRepository` does this). `RunRetrying` is the deadline plus the bounded
retry of #8091; a bare `deadline.Run` gets the deadline and no retry. `Build`
runs once per attempt, so it must reset every output buffer the caller reads
afterwards, and `Command.Stderr` must keep the line structure the classifier
reads (`sanitizeGitStderrLines`, not `sanitizeGitProgressMessage`). Run a purely
local command through `gitRun`, which has no deadline on purpose. Building a
network command from the loop context directly gives it no deadline, and a
stalled fetch pauses the whole fleet sync for 300 s (#8052). Put
`Limits.LowSpeedArgs` before the subcommand of a fetch or clone. Do not add a
way to switch a deadline off, and do not mark a deadline kill as a shutdown:
`deadline.Run` already tells the two apart by checking the parent context.

## Git auth follows the checkout path

Pass the managed checkout path (`<ReposDir>/<repoID>`) to `gitCommandEnv` and
`gitRun` for every command that reaches a remote. Token auth derives the
credential's host from that path (#7763) and fails closed for any other path:
`tokenAuthProvider` applies the `repoCheckoutName` rule the clone path uses, so
a path outside `ReposDir` or under the reserved `.eshu-ref-worktrees` namespace
gets no credential header at all. A network command run from such a path goes
out anonymous and fails at the remote instead of offering a GitLab or Bitbucket
token to github.com. The skip has no runtime signal: the function cannot tell a
local command from a network one, and ref worktrees run local commands every
cycle, so a log there would be noise.
`TestGitCommandEnvTokenAuthUnmanagedPathSendsNoHeader` pins the skip. Keep the
header host and `repoRemoteURL` in lockstep:
`TestGitCommandEnvTokenHeaderMatchesCloneURLHost` covers that contract.
Both read the GitLab host from the config (`ESHU_GITLAB_HOST`, default
`gitlab.com`): `TestRepoRemoteURLUsesCustomGitlabHost`,
`TestGitCommandEnvScopesTokenHeaderToCustomGitlabHost`, and
`TestRepositoryIdentityStableOnDefaultGitlabHost` cover the custom host and
the default-host identity stability (#7764).

## Directory size

This directory is grandfathered over the 40-file cap. The ledger row is a
ratchet in both directions: moving files out without re-pinning fails the gate.
Re-pin in the same commit as the move.

## Verification

```bash
cd go && go test ./internal/collector/... -count=1
cd go && go build ./... && go vet ./...
bash scripts/verify-dirgate.sh --all
```

## SCIP consumer: parser/scip package split (#6772)

`snapshot_scip.go` and `snapshot_scip_groups.go` consume the SCIP index
parser and the external `scip-*` indexer runner. Those moved out of
`internal/parser` into `internal/parser/scip`, so the symbols are now
`scip.IndexParser`, `scip.ParseResult`, `scip.Indexer`,
`scip.LanguageFileGroup`, and `scip.DetectProjectLanguageGroups`. The
unexported `scipResultParser` / `scipProjectIndexer` interfaces in this
package are satisfied structurally, with no compile-time assertion binding
them, so a signature change in `internal/parser/scip` surfaces as a build
failure here rather than in that package's own tests. Build both after
touching `Parse`, `Run`, or `IsAvailable`.

Local identifiers here (`recordingSCIPIndexer`, `concurrentSCIPIndexer`,
`delayedSCIPIndexer`, `fakeSCIPParser`, `rootSCIPParser`,
`scipLanguageSubtrees`) belong to package `git`, not to `scip`, and keep
their names.

No-Regression Evidence: #6772 changed these two files by import and type
repoint only — no logic, control flow, signatures, or error wrapping.
Baseline origin/main 788edd169, same tree and toolchain. Normalizing all seven
moved files through the rename map leaves four residual hunks, every one
semantically null. Two are in production: locals renamed to avoid shadowing the
functions that took their names -- `occurrenceLine` -> `defLine` in parser.go,
and inside `filesByLanguage` the local became `byLanguage` (the `grouped` locals
in that function's two callers are pre-existing and untouched). The other two
are in moved tests: `parser_test.go` inlines two unexported parent-package
helpers that could not move, and `regex_hoist_bench_test.go` de-stutters a
benchmark helper whose body is byte-identical. So there is no measurable path
to regress.
`go test ./internal/collector/repo/git/... -count=1` passes (ok 4.684s,
unchanged set), and `go test -list` shows the SCIP test inventory moved
intact at 10 of 10 with the parent package retaining 127 tests and none
matching SCIP. No benchmark is quoted because no hot-path statement
changed; a timing comparison here would measure host noise, not the diff.

Performance Evidence (#8038 item 2): `skipParse` reports its clean-tree
observation and `snapshotIdentity` reuses it (`selection_cli.go`), so a
parse after the clean-tree check runs `git status --porcelain` once per
cycle instead of twice. Baseline origin/main 13bd606525:
`TestSkipParseParseAfterCleanCheckRunsStatusOnce` counts 2 runs on both
the dirty_tree and reprojection_superseded paths before the fix and 1
after, with identical stamped identities. Input shape: one managed
checkout behind the fake git. Wall-time anchor: one status run costs
0.02 s warm (5 runs, git 2.43.0) on a synthetic 26,000-file tree, so an
affected cycle saves one such run against a minutes-long parse. Terminal
counts unchanged: same selected paths, same skip decisions, and
`go test ./internal/collector/...` green.

No-Observability-Change (#8038 item 2): no metric instrument, label,
span, log key, or status field is added, removed, or renamed. The same
`eshu_dp_collector_reconcile_parse_skip_decisions_total` outcomes are
recorded on the same paths.

No-Observability-Change: no metric instrument, metric label, span, status
field, log key, queue table, worker, lease, or runtime knob is added,
removed, or renamed. The SCIP snapshot path keeps emitting
`eshu_dp_scip_snapshot_attempts_total` and `eshu_dp_scip_process_wait_seconds`
through the same `recordSCIPSnapshotAttempt` call sites with the same
`telemetry.FailureClassAttr("scip_"+reason)` values, so existing dashboards
and the telemetry-coverage row for this path stay valid.

Performance Evidence (#8123): a scheduled sweep now yields to a queued
webhook trigger at a repository boundary (`selection_sweep_yield.go`,
probe every 16 visited repositories, at most 8 yields per logical sweep,
both env-tunable). Before, on the QA fleet (issue #8123, 784 repos):
`claimed_at - received_at` median 155.9 s, p90 311.6 s — one sweep period.
After, locally: `TestPrioritySelectorClaimsTriggerQueuedMidSweepBeforeSweepEnds`
claims with 2 of 5 repositories visited (before: the control
`TestNativeSelectorWithoutYieldProbeWalksEveryRepository` visits all 5
before any claim is possible), and
`TestSweepYieldReturnsPartialSelectionAndResumeCoversTail` visits each of
5 repositories exactly once across the yielded+resumed walks, so fetch
work per logical sweep is unchanged. Fleet arithmetic at the measured
~0.28 s/repository: detection within ~4.5 s plus the in-flight
repository, then the partial batch's drain (usually empty) and one 1 s
poll; probe cost is at most 49 `EXISTS` reads per 784-repository sweep on
the existing `(status, received_at)` index. Stated target: p90 wait
bounded by probe cadence plus partial-batch drain instead of the sweep
period. Fleet p90 and sweep-period regression are NOT_CHECKED (no fleet
in this lane; the new histogram is the instrument that proves them).
Conflict domain: `webhook_refresh_triggers` claims. The probe is
read-only and claiming stays solely in
`WebhookTriggerRepositorySelector` under the #7661 lease and fencing
token, so no double-claim or lost-trigger path is added; selector yield
state is single-goroutine like `reconcileWalkOffset`.
`TestNativeSelectorSweepYieldBoundCompletesSweepUnderTriggerStorm`
proves the sweep still completes under a permanent trigger queue, and
the existing claim-lease tests pass unchanged.

Observability Evidence (#8123): new `eshu_dp_webhook_trigger_claim_wait_seconds`
histogram (accepted rows, `claimed_at - received_at` at claim time), new
`eshu_dp_collector_sweep_yields_total` counter, the INFO
`git repository sweep yielded to queued webhook trigger` log
(`visited_repositories`, `selected_repositories`, `resume_offset`,
`remaining_repositories`), and the WARN
`git sweep webhook trigger probe failed; completing sweep` degradation
log. An operator reads the wait histogram for the #8123 outcome, the
yield counter and log for where sweeps stopped, and the existing sweep
logs for the sweep period.
