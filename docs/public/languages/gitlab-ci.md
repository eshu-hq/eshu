# GitLab CI Parser

This page tracks the checked-in Go parser contract in the current repository
state. Canonical implementation: `go/internal/parser/registry.go` (the `yaml`
parser) plus the GitLab-CI-specific extraction and tests listed below.

GitLab CI defines a repository's pipeline in a `.gitlab-ci.yml` (or
`.gitlab-ci.yaml`) file. It is plain YAML (no `apiVersion`/`kind`), so it is
dispatched by filename inside the YAML parser, the same way `atlantis.yaml` is
special-cased. Parsing is fully static: only what the file declares is recorded.
No runtime, job logs, image digests, or script bodies are captured (a job's
`script` is reduced to a line count, and `variables` to a count, so no secret or
opaque shell body enters the graph).

`.gitlab-ci.yml` is a hidden (dot-prefixed) repository file. Filesystem
ingestion preserves it through the managed-workspace copy via an exact-basename
allowlist (`preserveFilesystemHiddenPath` in
`go/internal/collector/git_selection_filesystem.go`), the same mechanism that
preserves `.github/workflows`.

## Parser Contract
- Language: `yaml`
- Family: `cicd`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml/gitlab_ci.go`
- Fixture: `tests/fixtures/ecosystems/terraform_comprehensive/.gitlab-ci.yml`
- Unit test suite: `go/internal/parser/yaml/gitlab_ci_test.go`
- Integration validation: B-7 golden-corpus gate asserts the `GitlabPipeline`
  and `GitlabJob` nodes (required node labels) plus the `DEFINES_JOB` (`rc-27`)
  and `NEEDS` (`rc-28`) edges (`scripts/verify-golden-corpus-gate.sh`).

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Pipeline (`.gitlab-ci.yml`) | `gitlab-pipeline` | supported | `gitlab_pipelines` | `name, line_number, path` | `node:GitlabPipeline` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate (`node_present_GitlabPipeline`) | One node per `.gitlab-ci.yml`; name is the constant file marker, anchored at the first key line. |
| Pipeline stages | `gitlab-stages` | supported | `gitlab_pipelines` | `stages` | `property:GitlabPipeline.stages` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate | Ordered, comma-joined stage names. |
| Pipeline variables | `gitlab-variables` | supported | `gitlab_pipelines` | `variable_count` | `property:GitlabPipeline.variable_count` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate | Count only — variable values are never recorded (secret-safe). |
| Jobs | `gitlab-jobs` | supported | `gitlab_jobs` | `name, line_number, path` | `node:GitlabJob` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate (`node_present_GitlabJob`) | One node per top-level job; hidden/template (`.`-prefixed) jobs and reserved global keywords (stages, variables, include, default, image, services, before_script, after_script, cache, workflow) excluded. |
| Job stage / when / image | `gitlab-job-attrs` | supported | `gitlab_jobs` | `job_stage, job_when, image` | `property:GitlabJob.job_stage` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate | `image` accepts bare-string and `{name:}` mapping forms. |
| Job script size | `gitlab-job-script` | supported | `gitlab_jobs` | `script_line_count` | `property:GitlabJob.script_line_count` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate | Count across before/script/after; bodies are intentionally not captured. `extends:` is not resolved. |
| Job needs / dependencies | `gitlab-job-needs` | supported | `gitlab_jobs` | `needs` | `property:GitlabJob.needs` | `go/internal/parser/yaml/gitlab_ci_test.go` | B-7 gate | `needs:` (list-of-strings or list-of-`{job:}`-maps); falls back to `dependencies:` when `needs:` is absent. Drives the NEEDS edge (rc-28). |
| DEFINES_JOB edge (pipeline → job) | `gitlab-defines-job-edge` | supported | `gitlab_pipelines` + `gitlab_jobs` | — | `relationship:DEFINES_JOB` | `go/internal/storage/cypher/canonical_gitlab_edges_test.go` | B-7 gate (`rc-27`) | `(GitlabPipeline)-[:DEFINES_JOB]->(GitlabJob)` for every job in the same file; resolved in Go, matched by uid. Distinct from the code-symbol `DEFINES` edge to avoid conflation. |
| NEEDS edge (job → job) | `gitlab-needs-edge` | supported | `gitlab_jobs` | `needs` | `relationship:NEEDS` | `go/internal/storage/cypher/canonical_gitlab_edges_test.go` | B-7 gate (`rc-28`) | `(GitlabJob)-[:NEEDS]->(GitlabJob)` resolved within the same `.gitlab-ci.yml` by job name; self-loops and unresolved names are dropped. Distinct from the generic `DEPENDS_ON` so CI job ordering is never conflated with package/repo dependency. |

## Graph edges

The pipeline governance edges are projector structural edges resolved in Go and
matched by canonical uid, written in the canonical structural-edge phase
(`go/internal/storage/cypher/canonical_gitlab_edges.go`) alongside the Atlantis
edges. `GitlabPipeline`/`GitlabJob` are single-label canonical entity nodes, so
the edges materialize in the main atomic write group (no deferred-edge group is
needed — that pattern is only required for the multi-label package_registry
nodes).

- `(GitlabPipeline)-[:DEFINES_JOB]->(GitlabJob)` — every job the pipeline
  declares; asserted by the B-7 gate (`rc-27`).
- `(GitlabJob)-[:NEEDS]->(GitlabJob)` — job ordering from `needs:`/`dependencies:`,
  resolved within the same file; asserted by the B-7 gate (`rc-28`).

A `GitlabPipeline` node is connected to its repository through the standard
containment chain (Repository → Directory → File → GitlabPipeline).
