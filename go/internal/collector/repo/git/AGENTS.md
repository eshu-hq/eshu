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

A delta generation must carry the active commit it was diffed under
(`GitSyncDelta.BaselineCommitSHA` through `ScopeGeneration.DeltaBaselineCommitSHA`,
#7319); the projector refuses a delta whose baseline is not the active commit,
and `validateGenerationInput` rejects a delta without one. Since #8780 the
changed set is diffed from the newest activated full instead
(`GitSyncDelta.UnionBaseCommitSHA` through `DeltaUnionBaseCommitSHA`) and the
deleted set must keep the `D(active..head)` term (`cumulativeGitDelta`): drop
it and a file the active chain added is never retracted. The no-change check
stays on the active commit. `syncBaselineFromState` reuses the sweep's
`FullReconcileState`; keep every `DeltaBaselineResolver` fake's two reads
consistent (`withSyncBaseline` in the tests), and give fake-git scripts the
`fakeGitLsTreeArm` or the growth valve fires on an empty base.

`UncoveredProjectionWriters` (#7389) is a reconcile reason, not a fallback:
`decideForScope` probes it only when the sweep is not due and not throttled,
inside the per-cycle budget, and `decideGraphDirty` shares `throttle` with
`decide` (never the fresh check). A graph_dirty scope must go through the
forced-reconcile path so the reason reaches `ScopeGeneration.ReconcileReason`;
the store then re-projects a matching active generation (#7978). Never blank
the hint: bump `gitGenerationContentVersion` when emitted facts change for
unchanged input without a parser-binding or schema change
(`TestGenerationContentVersionPinsCollectorOutput` fails until you do, then
re-pin), or the store drops the new output as a duplicate. A fixture under
`testdata/content_version` must sit in a directory discovery admits:
`TestContentVersionFixtureFilesReachDiscovery` fails on a parsed file under
`deps`, `vendor`, `build` or another `defaultIgnoredDirs` entry (#8905),
because the digest never sees it. A parser registration change moves the folded
fingerprint on its own (#7986). The `deltaFallback*` constants are the closed
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

## Filesystem copy keeps only admitted dotfiles

The managed-workspace copy prunes dot-prefixed entries unless
`preserveFilesystemHiddenPath` (`selection_copy_workspace.go`) allows them. When
the parser registry admits a new dot-prefixed name (#8814, and the #8809
version pins), add its basename there, or filesystem mode never sees it. Never
add a credential-bearing dotfile (`.env`, `.pypirc`, `.netrc`) unless a record admits
it under a content-store policy (`.npmrc` is the one exception, #8978), and
keep `.gitignore` out: the copy applies the source ignore rules itself, and
copying the file would make discovery filter a second time. Names match case-insensitively, like the
registry, and are kept under non-hidden or allowlisted directories (`.github`,
`.github/workflows`, `.mvn`, and a dot-directory whose name is an allowlisted
file name) at any depth. A name under any other, unallowlisted dot-directory
(`tools/.config/.eslintrc`) is pruned with its parent, because the copy drops
such dot-directories whole; clone mode admits it, and
`TestFilesystemCopyPrunesAdmittedDotfilesUnderUnknownDotDirectories` pins the
difference. The copy keeps the whole `.mvn` directory and discovery applies the
registry rules to it, as in clone mode: `.mvn/extensions.xml` is admitted as
text, a `.java` file under `.mvn/wrapper` is parsed as Java, and `.mvn/jvm.config`,
`.mvn/maven.config` and the wrapper properties have no registry entry
(`TestFilesystemCopyKeepsMvnDirectoryAndDiscoveryAppliesRegistryRules`).
`TestPreserveFilesystemHiddenPathCoversRegistryDotfiles` fails when a
dot-prefixed registry exact name (all except `.gitignore`) is missing from
`preservedHiddenFilesystemFileNames`. A PR that adds dotfile names to the
registry goes red on it until it adds them to that map in the same change; the
version-pin names from #8882 (`.nvmrc`, `.node-version`, `.tool-versions` and
the rest) are in the map for that reason.

No-Regression Evidence (#8814): discovery path. `LookupByPath` over 29,083
repository paths averaged 14.94 ms before and 15.29 ms after the new names and
the `.mvn/extensions.xml` rule (six alternating single-CPU runs of a throwaway
benchmark, +12 ns per path), and `resolveNativeSnapshotFileSet` over the staged
35-repository golden corpus (192 files, none of the admitted names) averaged
3.22 ms before and 3.20 ms after. Every lookup pays one base-name compare for
the new rule. The copy path gains one base-name lowercase per dot-prefixed
entry and now copies the `.mvn` directory whole, which is not measured here.

Indexing cost, measured as proxies on synthetic corpora. The code touches no
Postgres, graph, queue or lease path, but each newly admitted file adds a file
fact and a content fact, a `fact_records` insert, a `content_files` upsert and a
run of the migration-138 secret-line scan. Per newly admitted 1-5 KB file the
steady-state total is about 1.19 ms: snapshot and fact build 0.266 ms (one CPU)
or 0.246 ms (16 CPUs), `fact_records` insert 0.163 ms, `content_files` upsert
about 0.76 ms (mostly the three trigram GIN indexes). The migration-138 trigger
adds 0.011 ms on clean content, 0.21 ms at about 2 matching lines and about
0.73 ms at 10. A 500 KB Makefile costs about 147 ms (26 ms collector, 115 ms
upsert). A control corpus with no admitted names stayed flat (+0.6% on the
snapshot step), 1,000 against 5,000 files scaled linearly within 1.5%, and peak
RSS grows about 15 KB per admitted file. This repository has 9 admitted names
among 29,088 tracked files (`git ls-tree -r --name-only 86b925b260 | wc -l`), so
+2.1 ms is predicted (8 indexed files x 0.266 ms); one same-tree base/HEAD pair
measured 641.629 s and 641.853 s (n=1, no noise estimate). Only 8 of the 9 files
were indexed in that pair: `tests/fixtures/sample_projects/sample_project_c/Makefile`
is dropped by the repository's own `.eshuignore` rule `tests/` (line 157), an
operator opt-out. Not measured: concurrent writers, graph and reducer
cost, a tuned `shared_buffers` (runs used defaults, one connection, a loaded
host with loadavg 5-21), and the 20-repository corpus gate, which is CI-only.
A separate pre-existing bimodal slowdown of the migration-138 update trigger is
tracked in #8915 and is not caused by this change. Input shape: the
golden-corpus fixtures, synthetic corpora and the repo's own path list.

No-Observability-Change: discovery reports the same skip and admit counters. A
newly admitted file is counted as an ordinary admitted file, and an unadmitted
dotfile is still pruned silently by the copy, as before. When a PR adds dotfile
names to the registry without adding them to `preservedHiddenFilesystemFileNames`,
`TestPreserveFilesystemHiddenPathCoversRegistryDotfiles` fails.

## Content-store policy (#8812)

`Definition.ContentStore` (parser registry) decides what the content store may
hold for a path. The policy is applied twice, by the same pure function
(`gitcontent.ApplyContentStore`), because the body exists at two stages:

1. `applyContentStoreToShapeFiles` (`content_store_policy.go`) runs after parse
   and before `shape.Materialize`, so `content_entities.source_cache` is sliced
   from the stored body. It also blanks entity source snippets and replaces the
   parser's whole-file `parsed_file_data.source` copy with the stored body.
2. `streamFacts` re-reads each body from disk and applies the policy again with
   `contentStoreTally`; every emitter after it (content fact, service catalog,
   workflow image, documentation) sees the stored body, never the source.

`addContentStoresForMetas` adds the policy for every path the stream re-reads
(content, documentation, and workflow-image metas, including CODEOWNERS and
`.gitmodules`, which are diverted before the parser), so a definition that binds
such a name is enforced. `TestEveryBodyReadingEmitterSeesTheStoredBody` and
`TestDeltaOnlyBodyReadersApplyThePolicy` scan every emitted fact payload for a
seeded credential.

Rules: the digest stays computed on the SOURCE bytes (`shapeFileFromParsed`), so
the freshness hint is policy-independent and the registry fingerprint carries a
policy change; the content record is always emitted (empty body for `metadata`)
so the projector upsert overwrites an old row on full and delta generations; no
tombstone and no `RetainedPaths` edit. `RepositorySnapshot.ContentStores` is
sparse and nil for a registry with no non-store policy. Structured values a
parser extracts (for example a Terraform variable `default` in
`entity_metadata`) are parser output and are not governed, by design (8813
record; `TestParserOutputIsNotGovernedKnownLeak` pins the hcl `.tf` echo). A
definition may bind `metadata` or `redact` only after that is proven: a seeded
full-stream scan over every path shape the definition covers
(`assertNoSeedInAnyFact`). When parser output becomes governed, flip that test.

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
work per logical sweep is unchanged. With a repository reindex pinned
(#8512 P2), `TestNativeSelectorSweepYieldResumeSkipsVisitedPinnedRepos`
fetches each of 5 repositories exactly once across three cycles: resumed
walks skip visited pins (carried on the selector, thread through
`SweepVisitedPinned`/`SweepVisitedPinnedRepos` into `limitWalk`) while
unvisited pins keep the head, and the fetch count reads
`fullScopeIDs` because a forced pinned visit skips the baseline-SHA
lookup. Fleet arithmetic at the measured
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
log. A probe failure also counts `eshu_dp_webhook_store_operations_total`
with outcome `failed` (provider and status `unknown`, #8512 P2), so
yield-disabled-by-outage alerts instead of hiding behind the quiet queue;
`TestSweepYieldProbeErrorIsCounted` pins the stream and its labels. An
operator reads the wait histogram for the #8123 outcome, the yield
counter and log for where sweeps stopped, the failed-probe stream for
silent yield loss, and the existing sweep logs for the sweep period.

## Symlink admission evidence (#8809)

`symlinkAdmission` is the one symlink rule both sync modes apply. Full
discovery calls `symlinkTargetAdmitted` from the walk matcher, and
`resolveNativeSnapshotFileSetForTargets` calls `admitTargetSymlink` for each
`--file-targets` entry, so a link dropped by one mode is dropped by the other
(`TestSymlinkTargetAdmissionAgreesAcrossSyncModes`). Discovery tests the link
name but the content read follows the link, so a target must be a regular file
inside the root, in the link's admission category (the same parser definition,
or both terraform state, documentation, `.gitmodules`, or CODEOWNERS), and not
excluded by `discovery.PathRules` built from the repo-merged options. Dangling
links, cycles, and directory links are dropped too. The delta path still
records an admitted link under its resolved target path, as before.

Behavior change in the delta path: an in-root link whose target leaves the
repository is dropped and counted, the way full discovery drops it, where it
used to fail the whole delta with "outside repository root". A target whose own
path is outside the repository, link or regular file, still returns that error
(`TestResolveNativeSnapshotFileSetForTargetsOutOfRoot` pins both).

Both callers resolve `repoPath` with `EvalSymlinks` once at the top, and
`newSymlinkAdmission` requires that resolved root: the containment check does
not resolve it again. CODEOWNERS and `.gitmodules` detection, containment, and
the returned `RepoRoot` all use one root
(`TestResolveNativeSnapshotFileSetSymlinkedRepoRootKeepsSameCategoryLinks`
covers both modes).

Performance Evidence: `BenchmarkSymlinkTargetAdmitted` (committed in
`snapshot_native_symlink_cost_test.go`) measures the matcher, not an end-to-end
discovery before/after. `go test -run '^$' -bench BenchmarkSymlinkTargetAdmitted
-benchtime=20000x -count=5 ./internal/collector/repo/git/`, run twice on a
shared 16-thread arm64 Linux host, gave 0.68 to 1.40 µs per regular file (one
`Lstat`) and 10.6 to 16.4 µs per symlink (`EvalSymlinks`, the location rules,
and `Stat`), against zero before. The spread is host load. Both are far below
the parse and read cost of the file they guard, and no queue, graph, or SQL
path changes. A repository-scale discovery comparison is NOT_CHECKED.

Observability Evidence: each dropped link adds one to
`DiscoveryStats.FilesSkippedByContent["symlink_target"]`. That map already
feeds `eshu_dp_discovery_files_skipped_total` as
`skip_reason=content:symlink_target`, the `discovery stats` log key
`files_skipped.content.symlink_target`, and the discovery advisory report's
content breakdown. The delta path counts into its own stats value, which the
production caller discards because full discovery already counted the same
link. No instrument, label set, span, or status field is added. The matcher
runs once per file on one goroutine, so the counter needs no lock. The walk
asks the matcher before it checks path globs, so a rejected link whose own path
matches an operator glob counts as `content:symlink_target`, not `user:`.
`.gitignore` and `.eshuignore` run after the walk, so a dropped link those
files would also skip counts as `content:symlink_target`, not
`gitignore` or `eshuignore` (before #8809 it counted under those).
`TestResolveNativeSnapshotFileSetCountsDroppedSymlinks` pins the count.

Known limits: the target check re-applies ignored directory names, hidden
paths and preserved hidden prefixes, ignored extensions, and ignored and
preserved path globs (including `.eshu/discovery.json`, vendor roots, and
`ESHU_DISCOVERY_IGNORED_PATH_GLOBS`). It does not re-apply `.gitignore` or
`.eshuignore`, so a path excluded only by those files can still surface through
an in-root link whose own name is admitted. The delta path checks only the
target's leaf. Git refuses a path beyond a symbolic link, so a git-diff target
never reaches a file through a directory link; an explicit operator
`--file-targets` path can, and is then judged as the regular file it reaches.

No-Regression Evidence (#8939): discovery path. `.swift-version` joins the
`raw_text` exact names and `preservedHiddenFilesystemFileNames`; both are map
lookups, so the added entry changes no lookup's complexity. A throwaway
benchmark of `LookupByPath` over eight mixed paths (none of them `.swift-version`),
run on the Go version pinned in `go/go.mod` with three alternating base/HEAD
rounds of three runs each, gave per-eight-lookup times of 2.10-4.25 us before and
2.03-4.87 us after. The host was loaded, so the runs are noisy; the fastest run
of each side was 2.10 us before and 2.03 us after, and no difference is
distinguishable. Each newly admitted `.swift-version` is one small file (a
version string), so indexing cost per file is the 1-5 KB figure recorded for #8814
above. Not measured: end-to-end collector wall time, because this repository has
no `.swift-version` and the golden corpus has none.

No-Observability-Change: discovery reports the same skip and admit counters, and
a `.swift-version` is counted as an ordinary admitted file. No metric, span, log,
status field, queue, worker, lease, batch, or runtime knob is added. When a PR adds
a dot-prefixed registry name without adding it to
`preservedHiddenFilesystemFileNames`,
`TestPreserveFilesystemHiddenPathCoversRegistryDotfiles` fails.
