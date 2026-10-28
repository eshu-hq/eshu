# #8871 eshu-deploy extraction census

## Scope

Base: `origin/main` at `dc65b4d597`. Worktree
`/home/ubuntu/personal-repos/eshu-8871-deploy-cutover`, branch
`drive/8871-deploy-cutover`.

This record answers #8871 acceptance criterion 1 (inventory chart
templates, values/schema, Compose overlays, GitOps examples, image/build
references, release automation, deployment docs, and every consumer). It
moves no code and authorizes no release, deployment, rollback, or in-tree
removal: the destination repository `eshu-hq/eshu-deploy` holds a README
only at `9f18dca`, so criteria covering publication, independent release,
rollback rehearsal, and production acceptance remain unmet by construction.

Migration stage: `planned`. Destination and owner are assigned by
`docs/internal/design/eshu-ecosystem-repository-migration.md` (row:
`eshu-deploy` owns Helm, Compose, GitOps examples, deployment defaults,
and cross-repository compatibility matrices). The same plan keeps
schema-bootstrap and repair implementation core-owned: they directly
operate core-owned schema and recovery state.

## Counts

- chart templates: 102
- argocd files: 11
- minimal manifests: 6
- observability files: 8
- grafana dashboards: 3
- deploy docs: 18
- run-locally files: 9

Count locations, in order: files directly under
`deploy/helm/eshu/templates`; all files under `deploy/argocd`; all files
under `deploy/manifests`; files directly under `deploy/observability`;
files directly under `deploy/grafana/dashboards`; all files under
`docs/public/deploy`; files directly under `docs/public/run-locally`.
`go/internal/deploy/extraction_census_test.go` pins every line above to the
tree.

## Helm family (`deploy/helm/eshu`)

Chart entry points: `deploy/helm/eshu/Chart.yaml` (name `eshu`,
`version: 0.0.3-pre-release-26`, `appVersion: v0.0.3-pre-release-18`),
`deploy/helm/eshu/values.yaml` (1567 lines of deployment defaults),
`deploy/helm/eshu/values.schema.json` (1731 lines, value gate),
`deploy/helm/eshu/README.md`, and the CI values overlay
`deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml`.

Template breakdown of the 102 chart templates: 27 `deployment-*.yaml`
workloads (collectors, resolution engine, workflow coordinator, webhook
listener, scanner worker, MCP server, NornicDB), 31 `service-*.yaml`
services plus `service.yaml`, 4 `servicemonitor*` monitors, 17 `_*.tpl`
helpers (`_helpers.tpl`, `_read_env.tpl`, per-collector helpers,
`_validation_core.tpl`, `_validation_component_activations.tpl`), the core
`deployment.yaml` and `statefulset.yaml`, `job-schema-bootstrap.yaml`,
4 configmaps, `httproute.yaml`, `ingress.yaml` plus
`ingress-webhook-listener.yaml`, 2 networkpolicies, 3 poddisruptionbudgets,
`pvc-nornicdb.yaml`, `rbac-kubernetes-live-collector.yaml`,
`serviceaccount.yaml`, 3 `validate*.yaml` pre-render gates, and `NOTES.txt`.
The chart identity, default-file sizes, and breakdown figures in this section
are pinned by TestExtractionCensusPinsHelmContractFigures; update that test
with this record.

Values top-level defaults cover `image`, `serviceAccount`, security
contexts, `schemaBootstrap` (enabled flag, hook modes, resources,
precheck image), `service`, `networkPolicy`, `apiAuth`, `api`,
`mcpServer`, `resolutionEngine`, `workflowCoordinator`,
`webhookListener`, one stanza per collector (`confluenceCollector`,
`ociRegistryCollector`, `terraformStateCollector`, `awsCloudCollector`,
`gcpCloudCollector`, `kubernetesLiveCollector`, `vaultLiveCollector`,
`azureCloudCollector`, `packageRegistryCollector`,
`sbomAttestationCollector`, `securityAlertCollector`, `cicdRunCollector`,
`pagerDutyCollector`, `componentExtensionCollector`, `grafanaCollector`,
`prometheusMimirCollector`, `lokiCollector`, `tempoCollector`,
`jiraCollector`, `vulnerabilityIntelligenceCollector`, `scannerWorker`),
`exposure`, `ingester`, `env`, `contentStore`, `observability`, `neo4j`,
`nornicdb`, `repoSync`, and `eshuignore`.

## Compose family (20 root files plus `docs/public/run-locally`)

Root overlays, every one named here so the cutover cannot drop a lane:
`docker-compose.yaml`, `docker-compose.demo.corpus.yaml`,
`docker-compose.demo.runtime.yaml`, `docker-compose.demo.yaml`,
`docker-compose.e2e.mutation.yaml`, `docker-compose.e2e.yaml`,
`docker-compose.live-backend-neo4j.yml`,
`docker-compose.live-backend-nornicdb.yml`, `docker-compose.neo4j.yml`,
`docker-compose.read-api-latency-gate.yaml`,
`docker-compose.remote-e2e.foundation.yaml`,
`docker-compose.remote-e2e.observability.pprof.yaml`,
`docker-compose.remote-e2e.observability.yaml`,
`docker-compose.remote-e2e.pprof.yaml`,
`docker-compose.remote-e2e.runtime.yaml`,
`docker-compose.remote-e2e.seed.yaml`, `docker-compose.remote-e2e.yaml`,
`docker-compose.telemetry.yml`, `docker-compose.tier2-tfstate-v25.yaml`,
`docker-compose.tier2-tfstate.yaml` (tier2/v25 tfstate drift E2E overlays,
revived on the extracted satellite per #9288).

`docs/public/run-locally` holds 4 compose fragments
(`docker-compose.component-extension-oci.yaml`,
`docker-compose.component-extension-pagerduty.yaml`,
`docker-compose.component-extension.yaml`,
`docker-compose.governance-two-team.yaml`) and 5 guides
(`docker-compose.md`, `index.md`, `local-binaries.md`,
`mcp-catalog-sweep.md`, `mcp-local.md`).

## GitOps family (`deploy/argocd`, `deploy/manifests`)

ArgoCD: `deploy/argocd/base/` (`application.yaml`, `kustomization.yaml`,
`values.yaml`), `deploy/argocd/overlays/qa/` (`application-patch.yaml`,
`kustomization.yaml`, `values.yaml`), `deploy/argocd/overlays/aws/`
(`application-patch.yaml`, `externalsecret-examples.yaml`,
`kustomization.yaml`, `values.yaml`), plus `deploy/argocd/README.md`.
The QA overlay values reference the schema-bootstrap boundary; the AWS
overlay carries secret-reference examples only, no credential material.

Minimal manifests: `deploy/manifests/minimal/` (`kustomization.yaml`,
`namespace.yaml`, `configmap.yaml`, `service.yaml`, `statefulset.yaml`,
`secret-example.yaml` with placeholder content only).

Adjacent deployment assets moving with the cutover: `deploy/observability`
(OTLP collector config, 4 Prometheus rules, 2 hosted-operations alert
files, plus its README) and `deploy/grafana/dashboards` (freshness,
hosted-operations, observability dashboards). `deploy/README.md` indexes
the families.

## Deployment defaults, images, and release automation

Core image default: `repository: ghcr.io/eshu-hq/eshu`,
`tag: v0.0.3-pre-release-18`, rendered as `repository:tag` by every
workload template including `job-schema-bootstrap.yaml`. Bundled
collectors are digest-pinned in defaults:
`ghcr.io/eshu-hq/eshu-collector-loki@sha256:12eac89e…` and
`ghcr.io/eshu-hq/eshu-collector-tempo@sha256:0230e17…`. NornicDB defaults
to `ghcr.io/eshu-hq/nornicdb-amd64-cpu` with a tag carrying both a moving
ref and a digest. The `ghcr.io/example/collector-pagerduty` entry is a
documentation placeholder the schema documents as digest-or-nothing.
`TestExtractionImageRefsCarryExplicitTags` forbids empty and `latest`
tags: immutable artifacts require explicit versions, and moving-tag
promotion stays in release automation (`promote-moving-tags` lane).

Release automation moving with the assets:
`.github/workflows/docker-publish.yml`
(`CHART_PATH: deploy/helm/eshu`,
`CHART_REGISTRY: oci://ghcr.io/<owner>/charts`, lint/package/push
lanes), `scripts/ci/push-helm-chart.sh`,
`scripts/ci/select-docker-publish-lanes.sh`, and the root `Dockerfile`
that builds the release image (including
`/usr/local/bin/eshu-bootstrap-data-plane`).

## Core-owned boundary that stays

`deploy/helm/eshu/templates/job-schema-bootstrap.yaml` moves as a
template, but what it invokes stays core: `go/cmd/bootstrap-data-plane`
owns the schema and recovery-state implementation, and every template
renders environment through core-owned helpers (`_read_env.tpl`,
`renderContentStoreEnv`, `renderDataPlaneBootstrapEnv`). The migration
plan states the reason: bootstrap and repair commands directly operate
core-owned schema and recovery state, so they remain utilities of the
retained core, not independently released services.

## Consumers

- Contract tests that stay core and must keep passing against the
  extracted artifacts: 46 files under `go/internal/runtime`
  (`helm_*_contract_test.go`, `helm_*_test.go`, `compose_*_test.go`),
  including `helm_chart_contract_test.go`,
  `helm_schema_bootstrap_contract_test.go`,
  `helm_component_activations_core_version_test.go`, and
  `compose_nornicdb_image_test.go`. They render the in-tree chart and
  compose files; post-cutover they pin the eshu-deploy artifact versions.
- CI lanes selecting deployment assets: `docker-publish.yml`,
  `e2e-tests.yml`, `golden-corpus-gate.yml`, `ifa-determinism-gate.yml`,
  `live-backend-tests.yml`, `mcp-schema-drift.yml`,
  `read-api-latency-gate.yml`, `security-scan.yml`, `test.yml`.
- Operator scripts referencing compose or the chart (representative):
  `scripts/verify_collector_git_runtime_compose.sh`,
  `scripts/verify_oci_registry_collector_helm.sh`,
  `scripts/verify-helm-schema-bootstrap.sh`,
  `scripts/verify-gitops-rendered-diff-preflight.sh`,
  `scripts/sync_local_compose_mcp.sh`, all `remote-e2e` compose drivers.
- Deployment docs under `docs/public/deploy` (18 files: EKS guide,
  Kubernetes Helm/ArgoCD/manifests guides,
  `runtime-parity-matrix.md`, backup/restore, upgrades/rollbacks,
  production checklist) and the `deploy-kubernetes` tutorial.
  `docs/public/deploy/kubernetes/runtime-parity-matrix.md` is the current
  compatibility matrix baseline the extracted matrix must supersede with
  tested cross-repository version pairs.
- `go/internal/cigates` lockstep (this record registers in
  `specs/ci-docs-lockstep-paths.txt`, `docsset.go`, and the three
  workflow `lockstep` filters so docs-only CI treats it as code).

## Compatibility matrix (starting point, not proof)

Baseline: `docs/public/deploy/kubernetes/runtime-parity-matrix.md`.
Current chart pins: core `v0.0.3-pre-release-18`, Loki/Tempo digests
above, NornicDB digest above, Postgres `18-alpine`, Neo4j per
`compose_neo4j_image_test.go`. Oldest/newest supported core and
component version pairs, credential-handling and artifact-provenance
statements, and vulnerability-response ownership are NOT_CHECKED: the
extracted repository must publish them with tested pairs, and the QA EKS
deployment configuration is verified read-only while cutover authority
is pending.

## Destination content plan (no push authorized in this lane)

Proposed `eshu-deploy` layout, prepared as a PR plan only:

- `helm/eshu/` from `deploy/helm/eshu/` (chart, values, schema, ci
  overlay, all 102 templates)
- `compose/` from the 18 root `docker-compose.*` files plus
  `docs/public/run-locally` fragments
- `gitops/` from `deploy/argocd/` and `deploy/manifests/`
- `observability/` and `grafana/` from `deploy/observability` and
  `deploy/grafana`
- `docs/` from `docs/public/deploy` guides plus a compatibility matrix
  superseding `runtime-parity-matrix.md`
- release automation (chart publish lane, `push-helm-chart.sh`) rewired
  to the new repository with independently owned rollout/recovery
  procedures

The cutover PR removes the in-tree originals only after independent
build, release, deploy, and rollback rehearsal against the QA EKS target
with production acceptance recorded; `go/internal/runtime` contract
tests repoint at the published artifact versions in that PR.
