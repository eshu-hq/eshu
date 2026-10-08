# 7322: sweep EvidenceArtifact orphans by the missing source edge

## Problem

1,231 `EvidenceArtifact` nodes on ops-qa are orphaned: 805 with no
relationship and 426 attached only to an `Environment` (#7322). The
repo-relationship retract reaches artifacts only through the source
repository's `HAS_DEPLOYMENT_EVIDENCE` edges
(`RetractSingleRepoEvidenceArtifactsCypher`,
`RetractRepoEvidenceArtifactsCypher`), so an artifact whose source edge was
already deleted can never be reached again. The orphan sweep covers
`EvidenceArtifact` but its S2 connectivity check treats any relationship as
connected, so the 426 env-attached artifacts are never swept either.

The artifact node carries no source-repo property (`id` is a sha1 hash;
`resolved_id` embeds the source only for some evidence sources), so a
repo-scoped property match is fragile. The robust signal is structural: no
incoming `HAS_DEPLOYMENT_EVIDENCE` edge.

## Change

`EvidenceArtifact` gets a source-edge-aware orphan definition inside the
existing sweep machinery (same S1/S3/S4, TTL, mark, TOCTOU re-verify,
bounded batches, lease):

- S2' (`BuildConnectedKeysQuery`): for this label only, check the typed
  incoming edge —
  `MATCH (n:EvidenceArtifact {id: candidate_key})<-[r:HAS_DEPLOYMENT_EVIDENCE]-(source)`
  — instead of any relationship. Concrete relationship variable, anchored
  on the caller-supplied key: the only shape proven reliable on both pinned
  backends. No negated or counted pattern-existence predicate. The far end
  stays untyped on purpose so a hand-repaired edge from a non-Repository
  node still masks the orphan; the key-anchored near end bounds the
  expansion to that artifact's own incoming edges.
- S5' (`BuildSweepOrphanNodesStatement`): this label detaches
  (`DETACH DELETE n`) with the same guard and marker+age rechecks, because
  its orphans may still carry target or environment edges that plain
  `DELETE` fails on. Only the artifact node and its own edges are removed.
  A source edge re-created between the re-verify and the delete is removed
  with the artifact, and the idempotent writer MERGEs it back on the next
  cycle.

Cardinality: S1 page <= CountLimit, S2' <= page keys (UNWIND-anchored, one
row per key max via DISTINCT), S5' <= BatchLimit keys. Anchor:
`EvidenceArtifact(id)` via the `evidence_artifact_id` UNIQUE constraint
(Neo4j) / id lookup index (NornicDB).

Neo4j EXPLAIN (pinned `neo4j:2026-community`, local container, constraint
applied, COST/SLOTTED):

- S2': `Unwind -> NodeUniqueIndexSeek (UNIQUE n:EvidenceArtifact(id)) ->
  Expand(All) (n)<-[:HAS_DEPLOYMENT_EVIDENCE]-() -> Distinct`. No label
  scan.
- S5': `Unwind -> NodeUniqueIndexSeek(Locking) -> Filter
  (evidence_source) -> Eager -> DetachDelete`. Index-backed locking seek;
  Eager separates the read from the delete.

Live proof: `TestLiveOrphanSweepReachesSourcelessEvidenceArtifact` seeds a
sourceless env-attached artifact (source edge deleted first, reproducing
the post-#7285-bug state), a fully disconnected artifact, and a control
with an intact source edge; cycle 1 marks exactly the two sourceless ones,
cycle 2 past the TTL deletes exactly those two, cycle 3 re-runs the sweep
and deletes nothing (idempotent); the control node and its source edge
survive. RED before the fix (the env-attached artifact was never marked),
GREEN after, on the pinned Neo4j image.

No-Regression Evidence: this is a correctness fix on a background
maintenance path (leased runner, bounded batches), not a hot path, so no
full benchmark; the no-regression check is the unchanged anchor shape
(UNWIND + label(id) UNIQUE seek on both statements, same as before) plus
the EXPLAIN plans above showing no label scan on either statement, plus
the existing sweep unit suite (S1/S3/S4 shapes, guards, chunking, races)
passing unchanged. Before: env-attached sourceless artifacts survived
every cycle (0 swept). After: swept within TTL+1 cycle. The DETACH cost
note: NornicDB prices DETACH DELETE above plain DELETE, but S5' runs at
most BatchLimit keys per cycle with <=3 edges per artifact (source,
target, environment), and the production repo retract already DETACHes
this exact node type per cycle -- same operation, smaller batches.
NornicDB live proof is NOT_CHECKED locally; both new shapes reuse
primitives the retract path already exercises on the pinned NornicDB
backend (typed directed concrete-variable MATCH, key-anchored DETACH
DELETE), and the CI live-backend legs cover the sweep suite there.

Observability Evidence: no new signals. The runner already logs
`graph orphan sweep cycle completed` with per-label `counts_by_label`,
`marked_by_label`, `deleted_by_label`, and `skipped_by_label`
(`go/internal/reducer/maintenance/graph_orphan_sweep_runner.go`), so
EvidenceArtifact mark/delete counts for sourceless artifacts flow through
the existing operator-visible log. `No-Observability-Change` for new
instruments; behavior change is visible in the existing per-label counts.
