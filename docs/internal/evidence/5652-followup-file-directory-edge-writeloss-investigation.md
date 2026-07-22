# P0 #5652 Follow-Up: File/Directory Edge Write-Loss — Investigated, Not Reproducible In Production

Date: 2026-07-22. Branch `p0-5652-followup-file-edge-writeloss`. This is a
follow-up to the two defects Part 2 and Part 3 of
`docs/internal/evidence/5652-nornic-bare-match-writeloss.md` (sibling,
unmerged worktree `.worktrees/p0-nornic-writeloss`) documented as "CONFIRMED
BROKEN, NOT FIXED HERE":

1. `canonicalNodeFileUpdateExistingCypher` /
   `canonicalNodeRootFileUpdateExistingCypher`
   (`go/internal/storage/cypher/canonical_node_cypher.go`): a `WITH`-chained
   node `SET` followed by `MATCH`+`MERGE` edge clauses, claimed to silently
   drop the `REPO_CONTAINS`/`CONTAINS` edge writes.
2. The three `canonicalNodeRefreshCurrent*Cypher` statements (FileImportEdges,
   DirectoryFileEdges, DirectoryParentEdges), claimed to silently drop their
   `UNWIND`-batched `DELETE`.

The assignment was to implement a fix for both and re-prove the shipped code
by live read-back. Instead, live-proving against the exact pinned image
(`timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`,
digest verified via `docker image inspect`) through the REAL production call
site (`storagenornicdb.PhaseGroupExecutor` driving the real
`cypher.CanonicalNodeWriter.Write`, the same path
`internal/replay/offlinetier/delta_tier_live_test.go` uses) found that
**neither defect is reachable through Eshu's actual production dispatch
path**, for two different reasons per defect. No Cypher statement was
changed. Per the repo's Mandatory Prove-The-Theory-First rule ("a theory that
is disproven is a saved implementation, not a failure: record the result"),
this note is that record.

## Environment

Isolated NornicDB v1.1.11 container (`docker run`, not Compose, mirroring
`scripts/verify-replay-tier.sh`), no-auth Bolt, container name
`eshu-defect12-nornicdb`, host ports `48901`/`48902`. Torn down after this
investigation. Go build cache isolated to this worktree
(`GOCACHE=.worktrees/nornic-defect12/.gocache`).

## Finding 1: File node/edge statements do not reproduce as broken

`canonicalNodeFileUpdateExistingCypher` carries `Operation:
OperationCanonicalUpsert`. Production's `PhaseGroupExecutor.ExecutePhaseGroup`
routes every non-retract, non-entity-label phase through
`executeGroupedChunksWithDrain`, which calls `ge.ExecuteGroup` — a real
managed Bolt transaction (`session.ExecuteWrite`) — batching every statement
in the phase's chunk (`DefaultFilePhaseStatements = 5`) into one transaction.
This is the dispatch mode the sibling evidence doc's Part 2 claim needs to be
tested under to be representative.

Four independent reproductions, all against the exact pinned image, all
showing the shipped (unmodified) statement text works correctly:

1. **Real call site, true update** (`TestFileUpdateExistingEdgesGraphTruth_ExistingFile`,
   `go/internal/replay/offlinetier/canonical_file_edge_writeloss_live_test.go`):
   gen1 creates a File (`FirstGeneration=true`, correctly wired via
   `canonicalNodeFileFirstGenerationMergeCypher`), gen2 updates the same File
   through the real writer (`FirstGeneration=false`, driving the code path
   under test). After gen2: `File.generation_id` reads back `gen2`, and both
   `REPO_CONTAINS` and `CONTAINS` edges are present.
2. **Real call site, brand-new file in the same non-first-generation batch as
   an update** (`TestFileUpdateExistingEdgesGraphTruth_BrandNewFile`, same
   file): gen2 introduces a File that never existed in gen1, in the SAME
   `files`-phase batch as an update to an already-existing File — the
   scenario the sibling evidence doc's Part 2 proof did not cover (its
   fixture pre-seeded the File before ever running the statement, so it never
   tested a File created for the first time inside the SAME managed
   transaction as other rows). Both the pre-existing and the brand-new File's
   node and both edges are present after gen2.
3. **Isolated single-statement, ExecuteGroup dispatch** (reproduced during
   this investigation, not committed — a throwaway probe): seeded
   Repository+Directory+File at `generation_id=gen1` via raw Cypher (matching
   the sibling doc's described methodology exactly), then ran the verbatim,
   unmodified `canonicalNodeFileUpdateExistingCypher` constant through
   `ExecuteGroup` with a row setting `generation_id=gen2`. Read-back in a
   separate transaction: `f.generation_id=gen2`, `REPO_CONTAINS.generation_id=gen2`,
   `CONTAINS.generation_id=gen2` — all three present and correct.
4. **Isolated single-statement, auto-commit dispatch** (same fixture, same
   throwaway probe, dispatched via `session.Run` instead of `ExecuteGroup`):
   identical result — both edges present and correctly updated.

The shipped statement was tested via both dispatch modes eshu's own writer
can plausibly use and did not reproduce a write-loss in either. This directly
contradicts the sibling evidence doc's Part 2 claim ("RelationshipsCreated=0
for both, and a direct read-back finds neither edge"). No root-cause
explanation for that discrepancy was found (same pinned image digest, same
statement text, same dispatch surface). The leading hypothesis is stack
contamination — the original probe ran on the same instance as an earlier
abandoned `UNIQUE`-constraint experiment (a documented NornicDB drop/recreate
corruption pitfall) — but that was not confirmed here. The discrepancy is
tracked as #5671; it must be re-verified against the original harness before
this class of statement is "fixed" anywhere else in the codebase on the
strength of the original claim alone.

**No code change made.** `canonicalNodeFileUpdateExistingCypher`,
`canonicalNodeRootFileUpdateExistingCypher`,
`canonicalNodeFileCreateMissingCypher`, and
`canonicalNodeRootFileCreateMissingCypher` are unchanged.

## Finding 2: Refresh DELETE statements reproduce as broken ONLY under a dispatch mode production never uses

Unlike Finding 1, this defect genuinely reproduces — but only under a dispatch
mode that Eshu's production `PhaseGroupExecutor` never applies to these
statements.

`docs/public/reference/nornicdb-query-pitfalls.md` ("Node-Label Disjunction In
A MATCH Matches Zero Rows" section) already documents, from unrelated prior
incidents (#4367, #5116, #5128), that on the pinned v1.1.11 image "multiple
DELETE statements sharing a single managed Bolt transaction do not all
apply... even a SINGLE DELETE statement dispatched through a managed
transaction (ExecuteGroup) can fail to apply... Treat every retract DELETE as
auto-commit-only." This investigation reproduced that exact behavior for the
three `canonicalNodeRefreshCurrent*Cypher` statements specifically:

- **Dispatched via `ExecuteGroup` (managed transaction)**: the shipped,
  unmodified `canonicalNodeRefreshCurrentFileImportEdgesCypher` text left a
  seeded `IMPORTS` edge in place after the "successful" delete (1 survivor,
  reproducing the sibling doc's Part 3 claim exactly).
- **Dispatched via auto-commit (`session.Run`)**: the SAME unmodified
  statement text correctly deleted the edge (0 survivors).

This is a genuine, real, dispatch-mode-sensitive defect in the statement
shape. It does not need a Cypher rewrite to avoid it, because **production
never dispatches these statements through `ExecuteGroup`**:
`canonicalNodeRefreshCurrentFileImportEdgesCypher`,
`...DirectoryFileEdgesCypher`, and `...DirectoryParentEdgesCypher` all carry
`Operation: OperationCanonicalRetract`
(`go/internal/storage/cypher/canonical_node_writer_retract.go`,
`canonical_node_writer_delta_retract.go`). `PhaseGroupExecutor.ExecutePhaseGroup`
detects an all-retract-operation phase
(`allStatementsUseOperation(stmts, OperationCanonicalRetract)`) and routes it
through `executeSequentialRetractPhase`, which — for every statement in that
path, Drain-marked or not — calls `e.Inner.Execute` (auto-commit), never
`ge.ExecuteGroup`. This routing already existed before this investigation,
built for the unrelated #4367/#5116/#5128 incidents; it happens to also
protect these three statements.

**This routing is specific to all-retract phases, not a general property of
`OperationCanonicalRetract`.** `PhaseGroupExecutor.ExecutePhaseGroup` only
takes the `executeSequentialRetractPhase` path when every statement in the
phase shares the retract operation (`allStatementsUseOperation`); entity
phases get the equivalent per-statement Execute/ExecuteGroup split inside
`executeEntityPhaseGroup`. A MIXED phase — retract statements alongside an
upsert, outside both of those paths — falls into
`executeGroupedChunksWithDrain`, which pulls out only Drain-marked statements
to run auto-commit; a non-Drain `OperationCanonicalRetract` statement in a
mixed phase stays in the grouped batch and gets dispatched through
`ExecuteGroup`, the exact managed-transaction mode this investigation proved
silently drops retract DELETEs. That is a real, separate production defect,
found while chasing review feedback on this PR's own dispatch guard: the
`terraform_state` phase mixes a non-Drain retract with an upsert today and is
affected. It is tracked and fixed in #5680 (an order-preserving dispatcher
rewrite, plus a corrected two-part-invariant guard — no retract statement
ever dispatches via `ExecuteGroup`, and retract-before-upsert ordering is
preserved), not in this PR.

Live proof at the real call site, through the real `PhaseGroupExecutor`,
confirms the shipped statements are correct in production as a result:

- `TestRefreshFileImportEdgesGraphTruth` — gen1 seeds a File importing a
  Module; gen2 keeps the File but drops the import; after gen2 the stale
  `IMPORTS` edge is gone (File node survives).
- `TestRefreshDirectoryFileEdgesGraphTruth` — gen1 seeds a File under
  `dir-a`; gen2 moves it to `dir-b` (same File identity, different
  `dir_path`); after gen2 the old `dir-a` `CONTAINS` edge is gone and the new
  `dir-b` `CONTAINS` edge is present.
- `TestRefreshDirectoryParentEdgesGraphTruth` — gen1 seeds a Directory
  reparented under `dir-a`; gen2 reparents it under `dir-b`; after gen2 the
  old `dir-a` `CONTAINS` edge is gone and the new `dir-b` `CONTAINS` edge is
  present. This is a dedicated, self-contained companion to the existing
  shared-cassette `edge-parent-a`/`edge-parent-b` scenario already covered by
  `TestDeltaTombstoneGraphTruth`
  (`go/internal/replay/offlinetier/delta_tier_live_test.go`), which was
  re-run here and also passes unmodified.

**No Cypher change made** to the three `canonicalNodeRefreshCurrent*Cypher`
constants. Instead, a static regression guard was added:
`TestPhaseGroupExecutorRetractPhaseNeverUsesExecuteGroup`
(`go/internal/storage/nornicdb/phase_group_executor_retract_dispatch_test.go`)
asserts that `PhaseGroupExecutor.ExecutePhaseGroup`, given an all-retract
phase built from these three statements' exact shape, calls the inner
executor's `Execute` the same number of times as there are statements and
`ExecuteGroup` zero times. This locks in the routing that keeps this defect
class unreachable; a future change that starts grouping retract-only phases
through `ExecuteGroup` (e.g. an unreviewed "fewer round trips" optimization)
would fail this test before it could silently reintroduce the write-loss.

## Concurrency and ordering considerations

No retract-before-merge or generation-gating behavior changed. The existing
phase order (`retract` -> `repository` -> `directories` -> `directory_edges`
-> `files` -> ...) is unmodified, and the dispatch-mode finding above explains
why the existing order is safe, not why it needs to change. The
`executeSequentialRetractPhase` routing that protects Finding 2 was not
touched; it already existed.

## Why the assigned fix was not implemented

The assignment's premise — read the sibling evidence doc, implement the
documented fix, re-prove it — assumed both defects reproduce in production.
Live-proving against the actual call site before writing any fix (per the
repo's Mandatory Prove-The-Theory-First rule) found:

- Finding 1 does not reproduce under any dispatch mode tested; implementing
  the proposed 3-statement split (with the accompanying
  `canonicalNodeFileCreateMissingCypher` redundancy question the task flagged
  as CRITICAL) would have changed working, live-verified production Cypher on
  the strength of a claim this investigation could not reproduce.
- Finding 2 reproduces only under a dispatch mode production does not use for
  these statements. Rewriting the statement text would not change production
  behavior (it is already correct) and would spend review budget on a
  code path that was never the actual risk; the actual risk (some future
  change routing retract phases through `ExecuteGroup`) is now covered by a
  regression test instead.

Per "MUST NOT optimize behavior that has not been proven correct" and "a
theory that is disproven is a saved implementation, not a failure": this
investigation is the saved result. No production Cypher changed in this PR.

## Regression coverage added (this PR)

```bash
cd go
go test ./internal/replay/offlinetier/... ./internal/storage/nornicdb/... ./internal/storage/cypher/... -count=1
# Live tier (requires the isolated NornicDB container above):
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
  NEO4J_URI=bolt://127.0.0.1:<bolt-port> NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
  go test ./internal/replay/offlinetier/ -run \
  'TestRefreshFileImportEdgesGraphTruth|TestRefreshDirectoryFileEdgesGraphTruth|TestRefreshDirectoryParentEdgesGraphTruth|TestFileUpdateExistingEdgesGraphTruth_ExistingFile|TestFileUpdateExistingEdgesGraphTruth_BrandNewFile' \
  -v -count=1
```

Result: all PASS against the pinned v1.1.11 image (see the four blocks of
`go test -v` output captured during this investigation; each names its
assertion and observed count).

No-Regression Evidence: no shipped Cypher statement changed; the new tests
add coverage for code paths that previously had none (File update-existing
edge persistence, File creation within a shared non-first-generation batch,
and dedicated single-purpose live proof for each of the three Refresh
statements). `TestDeltaTombstoneGraphTruth` and `TestDeltaFileRetractGraphTruth`
(pre-existing, unmodified) were re-run against the same pinned image and
continue to pass, confirming no behavior regressed.

No-Observability-Change: no runtime metric, span, log field, queue stage, or
worker knob changed. This PR is test-and-evidence-only.

## Follow-up for `docs/public/reference/nornicdb-pitfalls.md`

The two "open" follow-up bullets this task referenced (under "Pitfall:
`UNWIND`-Batched Bare-`MATCH` `SET` Silently Drops Its Write") are part of
issue #5652's unmerged branch and do not exist on `main` yet in this worktree
— there is nothing to flip in this PR. When #5652 merges, do NOT mark those
two bullets "fixed" on the strength of a Cypher rewrite that was never
needed: update them to point at this note instead, explaining that (a) the
`WITH`-chained File edge clause drop did not reproduce under either
production dispatch mode in a second independent investigation, and (b) the
`UNWIND`-batched Refresh `DELETE` no-op is real only under a managed-transaction
dispatch that production's `PhaseGroupExecutor` never applies to
`OperationCanonicalRetract` statements, a routing now covered by
`TestPhaseGroupExecutorRetractPhaseNeverUsesExecuteGroup`.
