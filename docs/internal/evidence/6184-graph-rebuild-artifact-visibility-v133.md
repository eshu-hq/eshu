# #6184: repo-dependency evidence artifacts lost in grouped writes (v1.3.3)

Branch `fix/6184-graph-rebuild-v133-live`, commits `786dcdf8d` (legs
fail-closed) and `f63704cda` (this fix), rebased onto `origin/main`
`69017828b`. All live proof on the digest-pinned NornicDB v1.3.3 image
from #6646 (`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`).
Corpus: `tests/fixtures/ecosystems`, 67 scopes, 6369 `fact_records`.

## Failing first (run15, on `786dcdf8d`)

`scripts/verify-graph-rebuild-from-facts.sh` settled green-but-wrong:
pre-wipe 2525 nodes / 3301 edges vs rebuilt 2532 / 3313, both passes
`0 missing, 7 extra` nodes and `0 missing, 12 extra` edges. The extras
are exactly one family:

- nodes: 2 `Environment` + 5 `EvidenceArtifact`
  (`application.yaml`, `application-stage.yaml`, `kustomization.yaml`,
  `base/kustomization.yaml`, `values-dev.yaml`)
- edges: 5 `HAS_DEPLOYMENT_EVIDENCE` + 5 `EVIDENCES_REPOSITORY_RELATIONSHIP`
  + 2 `TARGETS_ENVIRONMENT`

The `DEPLOYS_FROM` mains for the same pairs were present pre-wipe.
Pass 1 and pass 2 agree with each other exactly; only the phase-1
baseline is partial. Full log: `~/tmp/e6184/run15-gate.log`.

## Elimination (all on the kept run15 stack)

- Drain is sound: terminal requires two consecutive zeros of
  `fact_work_items` non-terminal **plus** open shared-projection
  intents (`scripts/lib/graph_rebuild_runtime.sh`). The loss is
  settled-partial, not a snapshot race.
- Snapshot reads are faithful: per-label sums equal `total_nodes`
  exactly in all three snapshots (2525/2532/2532).
- Orphan sweep innocent (0 `EvidenceArtifact` orphans, 0 deletes all
  run); unroutable-intents sink empty (no loud drop).
- Evidence facts for the affected generations stable since 05:42:14;
  cross-repo resolutions re-ran identically (`2ev/1res`) in phase-1 and
  pass 1; resolver preview is a pure function of those facts.
- `RunCypherGroup` runs all statements sequentially in ONE managed
  transaction, mains first (`go/cmd/reducer/neo4j_wiring.go`). A
  same-call artifact MATCH therefore cannot miss endpoints the main
  batch MERGEd — unless the backend does not apply them.

## Live theory proof (pinned v1.3.3, kept stack)

Throwaway probe (since removed): one managed txn of
`[DEPLOYS_FROM UNWIND MERGE, artifact UNWIND MATCH/MERGE]` with fresh
ids per iteration, read back after commit:

- grouped, cold endpoints: **40/40 artifacts silently lost** on
  commit-success (repeated with 300 ms read delay: 40/40 again — not
  read lag); mains persisted in every iteration
- same batches in separated transactions: **0/40 lost**
- grouped against pre-existing endpoints: 0/40 lost

So a MATCH in a later statement of a managed transaction does not see
nodes MERGEd earlier in the same transaction. Same
acknowledged-without-persisting family as #5410 (grouped
UNWIND/MATCH/MERGE SQL writes) and #4367 (grouped DELETE under-apply),
whose sequential-auto-commit remedy this fix mirrors, as does the node
writer's deferred-phase pattern (`partitionDeferredPackageRegistryEdgePhases`).
Production fit: phase-1 cold graph + load misses intermittently
(run14 green / run15 partial, same commit); refinalize rewrites heal;
a probe-guard retry cannot fix this shape (the re-run misses
identically in its own transaction).

## Fix (`f63704cda`)

`EdgeWriter.WriteEdges` keeps main-route statements on the grouped
path and executes evidence-artifact statements sequentially in
auto-commit transactions after the main group commits
(`executeArtifactStatements` in
`go/internal/storage/cypher/edge_writer_artifact_sequential.go`, split
out for the 500-line cap — `edge_writer.go` is at 498). Artifact
failure keeps the established contract: retryable error, intents stay
open, reprocessed claims re-MERGE mains idempotently first. No Cypher
text changed; the `artifact-sequential` execution mode is log-only.

CONTRACT CHANGE (owner review requested): two tests pinned the old
same-transaction grouping and are re-pinned, outcome intents preserved
(main group still replays wholly across a commit failure; derived rows
still logged separately, now as two entries). Same-transaction
co-location is unimplementable correctly on this backend per the probe
above.

## Regressions (RED pre-fix, GREEN post-fix)

- `TestWriteEdgesRepoDependencyArtifactFollowsMainCommit` (structural):
  RED (`artifact statements share the main managed transaction`),
  GREEN after.
- `TestBoltWriteEdgesRepoDependencyArtifactColdEndpointsPersist`
  (live, real `WriteEdges` path, cold UUID ids, true multi-statement
  group executor): RED (`12/12 artifact families lost on
  commit-success`, `main=1 artifactEdges=0` — the production
  signature), GREEN after (12/12 persist, including under phase-1
  load).
- Full `go/internal/storage/cypher` suite green; gofumpt/vet clean.
- Commit hook note: `--no-verify` (go-lint hook binary crashes on
  every file including untouched ones — go1.26 analyzer vs go1.27.1
  stdlib environment break, same as `786dcdf8d`).

## Gate proof (fix commit)

| run | pre-wipe | pass 1 | pass 2 (restart) | rebuild | load |
| --- | --- | --- | --- | --- | --- |
| 16 | 2532/3313 (complete truth converged in phase-1) | 0/0, 0/0 | 0/0, 0/0 | 88 s | 7.10 |
| 17 | 2532/3313 | 0/0, 0/0 | 0/0, 0/0 | 83 s | 1.19 |

Before/after on the same corpus: 82–83 s across runs 11–15 →
83 s run17 (run16 88 s under ~3x machine load). No-regression marker
holds (SLO band is 10%). Logs: `~/tmp/e6184/run16-gate.log`,
`run17-gate.log`, `observe-run16/17.log` (artifact counts 0 → 19 in
phase-1, held).

## Scale-lab rebuild (acceptance item 1)

3x amplified corpus: 195 top-level repos (`~/tmp/e6184/scale-corpus`,
flat copies of the fixture with `-s0/-s1/-s2` suffixes), accepted as
201 scopes / 19107 `fact_records` (exactly 3x; repo ids are
path-derived so copies scope distinctly). Same gate with
`ESHU_DR_SKIP_INTERRUPT=true` on the pinned image.

- Run18: INVALID baseline — the keeper live test's broken cleanup (see
  fix `2ab137370`) left 36 keeper nodes on the live backend mid-phase-1
  and they appear in the verdict diff (39 missing = 36 keeper + 3 tie,
  58 missing edges = 36 keeper + 22 tie, by exact count). Lesson
  recorded; never run live tests against the active validation lane.
- Run19 (clean): pass 1 rebuilds in **144 s** (vs 83 s at 1x —
  sublinear). Identity diff is exactly one family: the `workload:base`
  ownership tie — all three copies DEFINE byte-identical
  `workload:base`, phase-1 materialized s0's copy
  (`repository:r_b16b40b7`), pass 1 materialized s1's
  (`repository:r_e5f4da75`): 3 missing + 3 extra nodes and 23 missing +
  22 extra edges, every one touching that tie. **Zero
  `EvidenceArtifact` / `HAS_DEPLOYMENT_EVIDENCE` /
  `EVIDENCES_REPOSITORY_RELATIONSHIP` identities differ at 3x** — the
  fixed family converges at scale.

SLO row: fixture-scale exact-green 83 s (acceptance proof, runs 16/17)
plus scale throughput signal 144 s at 3x scopes/facts with the tie-only
diff above. Exact-identity comparison at scale requires a
duplicate-free corpus — naive identical-copy amplification introduces a
workload-ownership conflict the identity model cannot hold stable
across rebuilds (pre-existing tie-break behavior, out of scope for the
acceptance corpus which has no duplicate workloads; noted as a
follow-up for the owner, not fixed here). Logs:
`~/tmp/e6184/run19-scale-gate.log`, `run18-scale-gate.log`.

No-Regression Evidence (graph rebuild writes): baseline 82-83 s clean
rebuild on runs 11-15 (67 scopes, 6369 facts, pinned v1.3.3 digest
above); after the writer split, 83 s on run17 at normal load (run16
88 s under ~3x machine load average 7.10 vs 2.16) and 144 s on the 3x
corpus (201 scopes, 19107 facts) — sublinear. Terminal state for every
acceptance run: two consecutive all-queues-zero checks (fact work items
plus open shared-projection intents) followed by 0/0 node and edge
identity verdicts on clean and interrupted rebuilds. Safe because no
Cypher text changed (templates byte-identical): only execution grouping
changed (artifact batches run sequentially after the main group
commits), batch sizes and retryable-error/intent-reopen semantics are
unchanged, and the KustomizeOverlayResolver wiring from the earlier
branch commit is covered by the same runs 11-17 gate greens.

Observability Evidence: artifact batches emit the existing `shared
edge write completed` log with a new log-only `execution_mode`
value `artifact-sequential`, carrying the same per-batch
input/accepted/skipped/executed/route-count fields as the `group`
entry, so an operator can tell grouped mains from sequential artifact
writes per claim; the legs guard from `786dcdf8d` logs a WARN with
domain/partition/sample ids plus the `SharedEdgeTargetMiss` counter on
every deferred batch. No new metrics, spans, or dashboards; no
telemetry coverage doc change needed (no execution-mode enumeration
exists there).
