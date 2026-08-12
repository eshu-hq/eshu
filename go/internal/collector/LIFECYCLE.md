# Collector Service Lifecycle And Workflow

This file carries the collector service's poll-and-dispatch lifecycle: the
`Service.Run` loop, drain-hook semantics, fence-aware commit, dead-letter
replay, and the generation/scope bookkeeping each run depends on. It is split
out of the package README because the workflow narrative is too detailed for
the README's exported-surface overview, the same reason `SCHEDULING.md` and
`OPERATIONS.md` are separate (issue #5959).

`Service.Run` is the poll-and-dispatch loop. Sources that implement
`ObservedSource` can start `SpanCollectorObserve` once they know the poll is a
real collection attempt, which keeps drained or idle polls out of trace export.
When a generation is available, the span covers source collection and durable
commit. When no generation is ready, the service calls `AfterBatchDrained` if
at least one generation was committed since the last drain, then waits
`PollInterval` (1 second in `cmd/ingester`). Runtimes that must include empty
source batches in a fleet barrier may set `AfterEmptyBatchDrained`; the default
keeps idle polls from running drain hooks. The opt-in path fires on every idle
poll until this process's FIRST generation commit, then never again -- a shard
that owns no repositories never commits, so it keeps arriving at the fleet
barrier for its whole lifetime rather than arriving once and starving every later
epoch (#5852). `AfterBatchDrained` takes a `hasCommitted bool` -- true when this
drain follows a real commit since the last drain, OR when it is the
never-committed empty-batch escape's own first-ever call in this process's
lifetime (the `startupMaintenanceEscapeUsed` once-latch in
`collector.Service.Run`); false for every later empty-batch-escape call on a
shard that still has not committed -- so a caller wiring a fleet barrier can
tell "this shard has real work, or is due its one startup pass" from "this
shard is only re-arriving with nothing new to report." The
ingester forwards this into
`postgres.DeferredMaintenanceBarrierConfig.HasCommitted`, which keeps a
never-committed shard's arrival join-only past its startup escape: it may join an
epoch another shard already opened, but it never opens one itself (see
`go/internal/storage/postgres/README.md`). Cadence is therefore not uniformly
barrier-paced: a never-committed shard's call returns immediately when no
epoch is open, and only blocks synchronously until the epoch finishes when it
actually joins one. On receipt of a generation it calls `Committer.CommitScopeGeneration`
with the `facts.Envelope` channel and records
`CollectorObserveDuration`, `FactsEmitted`, `GenerationFactCount`, and
`FactsCommitted`.
If the durable commit returns an error and `DeadLetters` is wired, `Service`
records bounded scope/generation replay metadata without storing fact payloads
or local repository paths.

`GitSource.Next` manages a per-batch streaming lifecycle. On the first call per
batch it launches `startStream`, which:

1. Calls `Selector.SelectRepositories` to discover the current repository list
   (span: `SpanScopeAssign`).
2. Resolves all paths to absolute form, orders repositories largest-first by
   file count (`countRepositoryFiles`), and computes a stable `sourceRunID` via
   `facts.StableID`. The `sourceRunID` is derived from the input-order paths, so
   the largest-first reorder never changes the run identity.
3. Classifies repositories into `smallCh` and `largeCh` by file count. The
   count is walked once during step 2 (`countRepositoryFiles`, skipping `.git`,
   `node_modules`, `vendor`, `.venv`, `__pycache__`) and reused here, so the
   tree is not re-walked. `isLargeRepository` exposes the same count to callers
   that need the exact number.
4. Launches `s.SnapshotWorkers` goroutines (default 8). Workers prefer small
   repos; large repos acquire a `largeSem` semaphore (capacity
   `LargeRepoMaxConcurrent`) before snapshotting so at most N large parses run
   concurrently.
5. A coordinator goroutine closes `s.stream` when all workers finish.

Subsequent `Next` calls read one generation from `s.stream`. When the stream
channel closes, `Next` returns `ok=false` and resets for the next discovery
cycle.

For filesystem sources, `NativeRepositorySelector.SelectRepositories` uses a
manifest under the managed repository cache to avoid reselecting unchanged
workspaces. The manifest hashes the files the collector can actually use:
`.gitignore` and `.eshuignore` rule files are included, while files excluded by
those rules are skipped. This keeps local watch mode from creating new
generations for ignored logs, build outputs, or editor scratch files.
The managed copy preserves `.gitmodules` for content discovery but deliberately
omits `.git`; `SelectedRepository.GitTreePath` therefore points committed-tree
reads such as submodule gitlink resolution at the original source checkout.
For hosted Git sources, update sync lists remote branch heads with
`git ls-remote --symref` without fetching every branch, then update sync
computes a `git diff --name-status -z --find-renames` delta between the previous
checkout HEAD and the fetched remote ref before checkout. Changed and renamed
destination files become `SelectedRepository.FileTargets`; deleted and renamed
source files become repo-relative tombstone paths. Clones still produce a full
snapshot because no prior checkout exists.

`NativeRepositorySnapshotter.SnapshotRepository` runs five sequential stages
per repository:

1. **Discovery** — `resolveNativeSnapshotFileSet` calls
   `discovery.ResolveRepositoryFileSetsWithStats` with repo-local overrides from
   `.eshu/discovery.json`, `.eshu/vendor-roots.json`, `.gitignore`, and
   `.eshuignore` applied before parsing.
2. **Pre-scan** — `engine.PreScanRepositoryPathsWithWorkers` builds the import
   map concurrently.
3. **Go semantic pre-scan** — `engine.PreScanGoPackageSemanticRoots` builds
   package interface escapes, imported receiver method roots, chained receiver
   roots, generic constraint roots, and package import paths for parser options.
4. **Parse** — `buildParsedRepositoryFiles` parses each file through the
   `parser.Engine` worker pool; each parsed file becomes a `map[string]any`
   entry in `snapshot.FileData` and may carry semantic metadata such as
   dead-code root evidence. `snapshotParserOptions` keeps language-specific
   variable scope close to query needs: Java uses module-level variables so
   method locals do not flood canonical graph projection, while dynamic
   languages that rely on local-variable evidence still parse with
   `VariableScope=all`. Terraform parser buckets are mapped explicitly into
   content entities, including backends, imports, moved blocks, removed blocks,
   checks, and lockfile providers. Declared Grafana, Prometheus/Mimir, Loki,
   and Tempo observability parser buckets plus applied Argo CD/Kubernetes
   observability state buckets are emitted as versioned `observability.*`
   source facts during fact streaming, not as graph truth.
5. **Materialize** — `shape.Materialize` turns parsed files into
   `ContentFileMeta` records and `ContentEntitySnapshot` rows. Body strings are
   released after materialization; `streamFacts` re-reads them from disk at emit
   time so snapshot memory is `O(single_file)`.

`buildStreamingGeneration` launches a background goroutine that streams
`facts.Envelope` values through a buffered channel (`factStreamBuffer = 500`).
Delta snapshots add repository fact metadata (`delta_generation`,
`delta_relative_paths`, and `delta_deleted_relative_paths`), emit file and
content tombstones for deleted paths, and skip repo-wide reducer follow-up facts
until reducer-owned shared projection domains have their own file-scoped delta
contract. Full snapshots emit the shell-exec follow-up alongside SQL and
inheritance follow-ups so stale command-execution edges retract when command
calls disappear. Source-local projection and content writes still run for the
changed files in the generation. One evidence family is deliberately complete:
every delta also reads the current `.github/workflows/*.yml|yaml` files through
a body-free workflow metadata lane so advancing the active generation cannot
retire unchanged `ci.workflow_image_evidence`. This lane extracts only static
image evidence; unchanged workflows do not enter the parser or create file and
content rows. A changed workflow still follows the ordinary delta path and
replaces its prior evidence. A deleted workflow is absent from the new
generation. Pre-scan still covers the full discovered parser file set plus
explicit targets so imports and Go package semantic context match a full
snapshot.
When the stream re-reads repo-hosted service-catalog descriptors
(`catalog-info.yaml`, `opslevel.yml`, or `cortex.yaml`), it delegates to the
`servicecatalog` normalizer and emits observed `service_catalog.*` facts under
the same scope and generation. A documentation-only lane normalizes further
repo-hosted document kinds (Markdown, HTML, Office, archives, diagrams) into
source-neutral facts under the same scope and generation; see
`OPERATIONS.md` for the per-kind extraction and safety-limit detail.
`AfterBatchDrained` runs only after the service has committed at least one
generation and then observes the source batch drain. Idle polls do not trigger
it unless `AfterEmptyBatchDrained` is set for a caller that needs configured
empty source batches to participate in a cross-process barrier. The empty path
is gated on never-having-committed, not edge-triggered per drain window: it
repeats on every idle poll until this process commits its first generation, and
is permanently disabled by that commit. A shard that never commits therefore
never stops re-arriving at the barrier (#5852) -- but "arriving" is join-only
after the first call: `AfterBatchDrained`'s `hasCommitted` argument is true
for exactly the FIRST empty-batch-escape call in this process's lifetime (the
`startupMaintenanceEscapeUsed` once-latch), letting that one call open a
barrier epoch, or join one, and get the one startup maintenance pass
origin/main always ran; every later escape call on the same shard reports
`false` and stays join-only, joining an epoch another shard already opened
without ever opening one on its own. A fleet where nothing has committed
anywhere therefore has each shard open the barrier epoch at most once per
process per shard, not once per idle poll. A restart (pod eviction, rolling
deploy, crash-loop) is a new process: it re-arms the once-latch, so a
restarting shard may open another epoch. That is intended, since a
restarted fleet warrants its own startup pass, not a leak.

The delta-generation, documentation-extraction, and `AfterEmptyBatchDrained`
evidence for this section (No-Regression, Performance, and
No-Observability-Change) lives in `OPERATIONS.md`.
