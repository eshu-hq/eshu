# Evidence: nested-directory canonical projection (#4019)

Scope: hot-path canonical graph writer — `canonical_node_cypher.go` (directory
Cypher) and `canonical_node_writer_phases.go` / `canonical_node_writer.go` (phase
wiring) in storage/cypher. Splits the single `directories` write phase into a
node phase and a parent-edge phase.

## Bug

On NornicDB (the default backend) the canonical projection silently dropped
every File node — and all its content entities — for any file nested two or more
directories below the repo root. The previous combined directory write emitted
each depth as a separate statement; the depth-N statement did
`MATCH (p:Directory {path: row.parent_path}) MERGE (d:Directory {path: row.path})
MERGE (p)-[:CONTAINS]->(d)` against the parent depth-(N-1) directory that an
earlier statement of the same `directories` phase had MERGE'd.

The NornicDB **phase-group executor** (the production projector + B-7 gate path)
runs each write phase as one transaction that does NOT give a later statement's
`MATCH` read-your-writes against an earlier statement's MERGE in the same phase.
So the depth-N `MATCH (p:Directory ...)` found nothing, the depth-N directory was
never created, and the file write (`MATCH (d:Directory {path: row.dir_path})`)
then found no directory — so the File node and its entities were never written.

This is distinct from the multi-label package_registry visibility defect: the
atomic GroupExecutor path DOES provide cross-statement read-your-writes for
single-label nodes (see the `RequireAtomicGroup` "file entity containment"
conformance case, where a `File` MATCHes a same-group `Directory`), so only the
phase-group path was affected.

Depth-0 directories projected (parent is the Repository, committed in an earlier
phase) and depth-0 files projected (the `directories` phase commits before the
`files` phase). Only the within-phase cross-depth MATCH failed. The 20-repo B-7
corpus had zero files deeper than one directory, so the defect was latent.

## Fix

Directory writes are split:

- `directories` phase: `UNWIND $rows MERGE (d:Directory {path: row.path}) SET ...`
  — every directory at every depth, MERGE by path, NO `MATCH`, so the batch has
  no cross-row visibility dependency.
- `directory_edges` phase (runs after `directories` commits): wires each
  directory to its parent (`MATCH (r:Repository) MATCH (d:Directory)` for depth-0,
  `MATCH (p:Directory) MATCH (d:Directory)` for depth-N), with both endpoints
  already committed by the prior phase.

The `files` phase already ran after directories and is unchanged. The edge phase
stays inline in the main atomic group (single-label read-your-writes); it is not
deferred like the multi-label package_registry edges. Neo4j is behaviorally
unaffected.

## No-Regression Evidence

No-Regression Evidence: B-7 golden corpus gate green after the change — rc-35
`(HelmTemplateValueUsage)-[:HELM_VALUE_REFERENCE]->(HelmValueDefinition)` count rose from 7
to **8** (the nested `templates/config/configmap.yaml` `.Values.service.type`
usage now projects and resolves its edge), all 50 checks pass, ~37s
wall-clock (budget ceiling 1800s). `minimum_count` for rc-35 was raised to 8 so a
regression of depth-2 projection fails the gate.

- Baseline (before): live-graph probe after a clean gate run — `Directory{.../templates/config}`
  absent, `File{.../templates/config/configmap.yaml}` absent,
  `HelmTemplateValueUsage{service.type}` absent; 7 usage nodes, rc-35 count=7.
- After: same probe — `templates/config` Directory present (1), configmap File
  present (1), `service.type` usage present (1), `(:Directory)-[:CONTAINS]->(:File configmap)`
  present (1); 8 usage nodes, rc-35 count=8.
- Backend / version: NornicDB (default), Bolt, database `nornic`; schema applied
  before indexing by the gate's bootstrap-index.
- Cost: total directory write work is unchanged — it was already O(dirs)
  statements (one batch per depth); it is now O(dirs) node MERGEs in one phase
  plus O(dirs) edge MERGEs in a second phase, the same UNWIND/MERGE batches
  re-grouped. The only added cost is one extra batched graph round-trip per repo
  for the edge phase (directories per repo are a small fraction of files +
  entities); no per-file or per-entity work is added. Gate wall-clock is
  unchanged (~37s, same as the pre-change runs). Non-nested repos are unaffected:
  same node + edge rows, just split across two phases. File and entity writes are
  byte-unchanged.
- NornicDB read-your-writes safety: the `directory_edges` phase MATCHes only
  endpoints committed by the prior `directories` phase (and the Repository
  from an earlier phase), so it never depends on same-transaction visibility —
  the exact property the bug violated.

## No-Observability-Change

No-Observability-Change: the new `directory_edges` phase is a standard canonical
write phase counted by the existing per-phase `canonical_write` span/`slog`
telemetry (the writer logs `phase` + `duration_s` for every phase, so the new
phase is observable with no code change). Directory nodes and CONTAINS edges
remain counted by the existing `canonical_write` runtime-stage telemetry. No new
metric, span, status field, or log key is introduced, and none is removed.
