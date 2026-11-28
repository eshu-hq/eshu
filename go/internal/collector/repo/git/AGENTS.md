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
the hint. When emitted facts change for unchanged input,
`TestGenerationContentVersionPinsCollectorOutput` fails until you re-pin the
content epoch ledger (`epoch/ledger.tsv`, #9097) with
`-update-content-version-pin`; the re-pin advances the epoch when a fixture
file's output moved on unchanged bytes, or the store drops the new output as a
duplicate. Never hand-edit the ledger. A fixture-only change keeps the epoch,
so a change that edits a fixture file and its output together, or adds a path
rule outside the registry fingerprint, also passes `-advance-content-epoch`.
On a ledger merge conflict, take `origin/main`'s copy and re-pin; a hand-merged
ledger fails its seal check. A fixture under
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
`preserveFilesystemHiddenPath` (`selection_copy_workspace.go`) allows them. The
kept set is derived from the parser registry (#8904): a dot-prefixed entry is
kept when `Registry.LookupByPath` admits it under the store content-store
policy, so a new registry dotfile name needs no second edit here. That covers
exact names (`.editorconfig`, the #8809 version pins), extension-admitted names
(`.gitlab-ci.yml` via `.yml`), and path rules alike. Three exemptions stay out:
`.gitignore`, which the copy must not duplicate because it applies the source
ignore rules itself (copying it would let discovery filter a second time);
the names a metadata or redact policy governs (`.npmrc`, `.yarnrc.yml`,
`*.tfvars`), which stay pruned even though the registry admits them; and the
unadmitted credential-bearing dotfiles (`.env`, `.pypirc`, `.netrc`), which no
registry change keeps unless their definition binds a policy. Names match
case-insensitively, like the registry, and are kept under non-hidden or
allowlisted directories (`.github`, `.github/workflows`, `.mvn`, and a
dot-directory whose name the registry admits) at any depth. A name under any
other, unadmitted dot-directory (`tools/.config/.eslintrc`) is pruned with its
parent, because the copy drops such dot-directories whole; clone mode admits
it, and `TestFilesystemCopyPrunesAdmittedDotfilesUnderUnknownDotDirectories`
pins the difference. The copy keeps the whole `.mvn` directory and discovery
applies the registry rules to it, as in clone mode: `.mvn/extensions.xml` is
admitted as text, a `.java` file under `.mvn/wrapper` is parsed as Java, and
`.mvn/jvm.config`, `.mvn/maven.config` and the wrapper properties have no
registry entry
(`TestFilesystemCopyKeepsMvnDirectoryAndDiscoveryAppliesRegistryRules`).
`.gitmodules` is kept by explicit structural rule: no parser definition binds
it, but the submodule facts read it from the copy
(`TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitlabCI`).
`TestPreserveFilesystemHiddenPathCoversRegistryDotfiles` fails when the derived
set drifts from what the copy keeps, in either direction.

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

`eshu_dp_collector_content_store_files_total` carries `policy`, `redactor`
(`marker`, `skeleton`, or `none`), and `outcome`; the stream log carries one
count per key (`metadata_metadata`, `redact_<redactor>_<outcome>`).

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

Known limits: the target check re-applies ignored directory names, hidden
paths and preserved hidden prefixes, ignored extensions, and ignored and
preserved path globs (including `.eshu/discovery.json`, vendor roots, and
`ESHU_DISCOVERY_IGNORED_PATH_GLOBS`). It does not re-apply `.gitignore` or
`.eshuignore`, so a path excluded only by those files can still surface through
an in-root link whose own name is admitted. The delta path checks only the
target's leaf. Git refuses a path beyond a symbolic link, so a git-diff target
never reaches a file through a directory link; an explicit operator
`--file-targets` path can, and is then judged as the regular file it reaches.

## Per-file read cap (#8986)

A parser file whose discovered size is over `shared.SourceReadCap` (64 MiB) is
removed by `splitOversizeParserFiles` after `partitionNativeSnapshotFiles`, in
both full and delta mode, before the import pre-scan. Keep it there: pre-scan
reads every parser file and one read failure fails the whole snapshot, so a
file left in the set stops the repository from indexing. Terraform state
candidates stay (metadata-only), and documentation files keep their own bounded
readers. Only the full set counts, as `skip_reason=content:oversize`, and
`snapshotOneRepository` logs WARN `git_snapshot_file_skipped_oversize` per file
once the generation ids exist. A size skip is deterministic for the commit, so
it never degrades the snapshot. On a delta, `withOversizeDeltaTombstones` lists
each skipped target as deleted so its earlier facts are retracted, not left
stale; keep that or a delta serves the old file as current. A file that grew past the cap after discovery
is refused by `shared.ReadFileCapped` in the parse (`file_read_failed`) and the
stream (`content_read_failed`). A file that grows past the cap after discovery
and before the import pre-scan is skipped by
`Engine.PreScanRepositoryPathsSkippingOversizeStats`, not a snapshot failure;
`skipPreScanGrownFiles` then counts it under `content:oversize`, lists it in
`OversizeSkippedFiles`, drops it from the parse set and the Go package pre-scan,
tombstones a delta target, and marks the snapshot degraded (`file_read_failed`),
because the tree changed under the collector. Only `ErrSourceTooLarge` from a
file whose own size is over the cap is a per-file skip there; keep every other
pre-scan error fatal, and keep the Go package pre-scan on the filtered set
(`withoutPreScanSkips`). A Kotlin sibling over the cap must stay out of its
neighbours' pre-scan (`snapshot_prescan_sibling_test.go`).
`snapshot_oversize_test.go` and `snapshot_prescan_growth_test.go` pin all of it.
