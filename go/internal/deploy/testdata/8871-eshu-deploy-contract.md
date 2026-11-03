# #8871 eshu-deploy contract (extraction step 2)

## Scope

Base: `origin/main` including the merged step-1 census (#9356).
(That record inventories the assets; this record is the versioned
deploy contract `eshu-deploy` publishes on day one. The base is stated
without a fixed SHA so routine rebases cannot stale it; the contract
tests re-pin every version figure to the tree on each run.)

This record moves no code and authorizes no release, deployment,
rollback, or in-tree removal. The destination repository
`eshu-hq/eshu-deploy` holds a README only at `9f18dca`, so criteria
covering independent release, rollback rehearsal, and production
acceptance remain unmet by construction.

Migration stage: `planned`, per
`docs/internal/design/eshu-ecosystem-repository-migration.md`
(`eshu-deploy` owns Helm, Compose, GitOps examples, deployment
defaults, and cross-repository compatibility matrices; schema-bootstrap
and repair implementation stays core-owned).

## Extraction surface

`eshu-deploy` publishes these in-tree families unchanged in shape:

- Helm: `deploy/helm/eshu/values.yaml` (deployment defaults),
  `deploy/helm/eshu/values.schema.json` (value gate), the chart entry
  points, and every chart template including
  `deploy/helm/eshu/templates/job-schema-bootstrap.yaml`.
- Compose: the root `docker-compose.yaml` lane set (20 files, enumerated
  in the census) plus `docs/public/run-locally` fragments.
- GitOps: `deploy/argocd` base and overlays plus the minimal manifests.
- Operability: the parity baseline
  `docs/public/deploy/kubernetes/runtime-parity-matrix.md` (superseded by
  the matrix below as the published cross-repository contract) and the
  release automation (`docker-publish.yml` with its `promote-moving-tags`
  lane implemented by `scripts/ci/promote-moving-tags.sh`; chart push via
  `scripts/ci/push-helm-chart.sh`).

## Version basis

Chart identity: name `eshu`, version: 0.0.3-pre-release-26,
appVersion: v0.0.3-pre-release-18. The chart and the core image release
in lockstep: `deploy/helm/eshu/values.yaml` defaults the core image to
`ghcr.io/eshu-hq/eshu` at tag `v0.0.3-pre-release-18`, equal to the
chart appVersion. Every workload template renders `repository:tag`;
bundled collectors stay digest-pinned in the defaults:

- `ghcr.io/eshu-hq/eshu-collector-loki@sha256:12eac89e7abd2988529c6f2854ba63293b5507502630764c3f6b8ffc7e3bf84d`
- `ghcr.io/eshu-hq/eshu-collector-tempo@sha256:0230e174284bb7a9831babac9b9927be05df02e8620c20e448e91dbb1ac2ecac`
- NornicDB default
  `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`
  (`fix-500-e022384c` stream, digest-pinned).

Moving tags promote only through release automation
(`promote-moving-tags`); the extracted repository must keep that rule
for every tag it moves.

## Core-owned boundary that stays

`deploy/helm/eshu/templates/job-schema-bootstrap.yaml` moves as a
template, but what it invokes stays core-owned: the Job renders the core
image (`.Values.image.repository:.Values.image.tag`) and execs
`/usr/local/bin/eshu-bootstrap-data-plane`, built from
`go/cmd/bootstrap-data-plane`, which owns the schema and recovery-state
implementation. The `schemaBootstrap` stanza (enabled flag, hook modes,
resources, precheck image) stays gated by `values.schema.json`, and
every template keeps rendering environment through core-owned helpers.
Repair commands stay core-owned for the same reason: they operate
core-owned schema and recovery state, not a releasable service.

## Compatibility matrix

Single tested pair at extraction (v0). This compatibility matrix
converges oldest and newest on that pair; independent releases widen
the range only with tested pairs recorded here.

| Component | Oldest supported | Newest tested |
| --- | --- | --- |
| eshu core (`ghcr.io/eshu-hq/eshu`) | `v0.0.3-pre-release-18` | `v0.0.3-pre-release-18` |
| eshu chart (`eshu`, `version: 0.0.3-pre-release-26`) | `0.0.3-pre-release-26` | `0.0.3-pre-release-26` |
| Loki collector (digest-pinned) | `sha256:12eac89e…` | `sha256:12eac89e…` |
| Tempo collector (digest-pinned) | `sha256:0230e174…` | `sha256:0230e174…` |
| Postgres | `postgres:18-alpine` | `postgres:18-alpine` |
| Neo4j (compose default) | `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` | `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` |
| NornicDB (`nornicdb-amd64-cpu`) | `fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1` | `fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1` |

Credential handling: ArgoCD overlays carry secret-reference examples
only, no credential material; claim-driven collectors mount redaction
keys at runtime. Artifact provenance: release image from `Dockerfile`,
chart published via the `docker-publish.yml` lane to
`oci://ghcr.io/<owner>/charts`. Vulnerability response ownership for
the extracted stream is NOT_CHECKED: `eshu-deploy` must name it before
its first independent release. Production acceptance, deployment, and
rollback rehearsal against the QA EKS target are NOT_CHECKED.

## Cutover readiness

`go/internal/deploy/extraction_contract_test.go` pins every section
above to the tree. The cutover PR repoints the `go/internal/runtime`
contract tests at the published `eshu-deploy` artifact versions and
removes the in-tree originals only after independent build, release,
deploy, and rollback rehearsal with production acceptance recorded.
