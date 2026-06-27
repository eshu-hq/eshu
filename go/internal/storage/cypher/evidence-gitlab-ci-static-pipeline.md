# Evidence: GitLab CI static-pipeline parsing (rc-27 / rc-28)

Scope: new `.gitlab-ci.yml` static parser and its graph wiring across hot-path
files — `canonical_gitlab_edges.go`, `canonical_node_writer_phases.go`
(storage/cypher), `git_selection_filesystem.go`, `git_snapshot_native.go`
(collector). Adds `GitlabPipeline`/`GitlabJob` nodes and `DEFINES_JOB`/`NEEDS`
edges, mirroring the Atlantis feature. Part of #3873.

## Graph model

`.gitlab-ci.yml` (hidden file, preserved through the managed-workspace copy by an
exact-basename allowlist in `preserveFilesystemHiddenPath`) is parsed by filename
in the YAML parser into `gitlab_pipelines` + `gitlab_jobs` buckets, projected as
single-label `GitlabPipeline` / `GitlabJob` canonical entity nodes, and connected
by two distinct edge types (kept distinct from the generic `DEFINES`/`DEPENDS_ON`
to avoid conflation, matching the ATLANTIS_DEPENDS_ON / DEPENDS_ON_PACKAGE
precedent):

- `(GitlabPipeline)-[:DEFINES_JOB]->(GitlabJob)`
- `(GitlabJob)-[:NEEDS]->(GitlabJob)` (resolved within the same file by job name)

## No-Regression Evidence

No-Regression Evidence: B-7 golden corpus gate green on 3 consecutive
deterministic runs after this change — rc-27 `DEFINES_JOB` count=2, rc-28 `NEEDS`
count=1, `GitlabPipeline`=1, `GitlabJob`=2, all 27 required correlations pass,
~36s wall-clock (budget ceiling 1800s); git/package_registry/Atlantis canonical
writes byte-unchanged (details below).

- Baseline (before): no `.gitlab-ci.yml` parsing; `GitlabPipeline`/`GitlabJob`
  nodes and `DEFINES_JOB`/`NEEDS` edges did not exist.
- After: B-7 golden corpus gate green; `node_present_GitlabPipeline` count=1,
  `node_present_GitlabJob` count=2, rc-27 `DEFINES_JOB` count=2, rc-28 `NEEDS`
  count=1; all required correlations pass; elapsed ~36s (budget ceiling 1800s).
- Backend / version: NornicDB 1.1.6 (default), Bolt, database `nornic`.
- Input shape: the `terraform_comprehensive` corpus repo carries a 2-job
  `.gitlab-ci.yml` where `terraform-plan` `needs: [terraform-validate]`.
- Cost: the GitLab edges are single-label-anchored and ride the existing
  canonical structural-edge phase (same UNWIND/MERGE-by-uid pattern as the
  Atlantis MANAGES/ATLANTIS_DEPENDS_ON edges) in the main atomic write group — no
  new transaction, no deferred edge group, no extra graph round-trip. The
  discovery change is an O(1) basename map lookup per hidden file. Existing git,
  package_registry, and Atlantis canonical writes are byte-unchanged.
- NornicDB read-your-writes safety: `GitlabPipeline`/`GitlabJob` are single-label
  nodes, so the #3980 multi-label same-transaction `UNWIND $param`-`MATCH`
  visibility defect does not apply; the edges materialize in the same atomic
  group as the nodes (verified by the gate above).

## No-Observability-Change

No-Observability-Change: the new GitlabPipeline/GitlabJob nodes and
DEFINES_JOB/NEEDS edges are counted by the existing projector `canonical_write`
runtime-stage telemetry and preserved-hidden files by the discovery
`FilesSkippedHidden` counter; no new metric, span, status field, or log key.

The new nodes/edges are counted by the existing projector `canonical_write`
runtime-stage telemetry (entity + structural-edge counts) on the same
`slog`/metric path; preserved-hidden files remain covered by the discovery
`FilesSkippedHidden` counter. No new metric, span, status field, or log key is
introduced, and none is removed.
