# Runtime Parity Matrix

Use this matrix when comparing local Compose, remote E2E Compose, and Helm
before promoting a hosted deployment. It records which contract must match,
which differences are intentional, and which static verifier protects the
release evidence path.

Run the verifier from the repository root:

```bash
scripts/verify-compose-helm-runtime-parity.sh
```

The verifier renders:

- `docker-compose.yaml`;
- `docker-compose.remote-e2e.yaml`;
- remote E2E profile collectors for Confluence, Jira, and PagerDuty;
- remote E2E observability collectors for Grafana, Prometheus/Mimir, Loki, and
  Tempo;
- `deploy/helm/eshu` with Prometheus `ServiceMonitor` resources enabled.

It fails when required services are missing, critical Postgres or graph
environment wiring disappears, health or metrics probes disappear, Helm core
workloads stop rendering, or enabled Helm collector families lose
`ServiceMonitor` template coverage.

## Core Runtime Contracts

| Runtime | Local Compose | Remote E2E Compose | Helm | Static parity contract |
| --- | --- | --- | --- | --- |
| Postgres | `postgres` service | `postgres` service | platform-owned external dependency | Compose must render Postgres; Helm values must wire `contentStore.dsn` and Postgres env. |
| NornicDB or graph backend | `nornicdb` service by default | `nornicdb` service by default | platform-owned external dependency unless bundled NornicDB is explicitly enabled | Compose and Helm must wire `ESHU_GRAPH_BACKEND`, `NEO4J_URI`, and graph database env together. |
| Schema bootstrap | `db-migrate` one-shot | `db-migrate` one-shot | `schema-bootstrap` Job, usually Helm hook | Bootstrap must run before long-lived API, MCP, ingester, reducer, and collectors. |
| Bootstrap index | `bootstrap-index` one-shot | `bootstrap-index` one-shot | operator-run helper, not steady-state chart workload | Compose validates initial indexing before hosted collector proof; Helm promotion uses API/MCP and queue readback instead. |
| API | `eshu` service | `eshu` service | `Deployment/eshu-api` | Must expose health, metrics, Postgres, graph, and bounded read-surface wiring. |
| MCP server | `mcp-server` service | `mcp-server` service | `Deployment/eshu-mcp-server` | Must expose HTTP MCP transport, health, metrics, Postgres, and graph wiring. |
| Ingester | `ingester` service | `ingester` service | `StatefulSet/eshu` | Owns repository workspace storage; no other long-lived Kubernetes runtime should mount the workspace PVC. |
| Resolution engine | `resolution-engine` service | `resolution-engine` service | `Deployment/eshu-resolution-engine` | Owns reducer queue drain, graph projection, retry, replay, and recovery. |
| Workflow coordinator | optional profile, dark by default | active service | optional `Deployment` | Claim-driven collectors require active deployment mode, claims enabled, and matching collector instances. |
| Webhook listener | optional profile | active service | optional `Deployment` | Only configured provider webhook paths should be exposed. |

## Hosted Collector Contracts

| Collector family | Remote E2E service | Helm component | Static parity contract |
| --- | --- | --- | --- |
| Confluence | `collector-confluence` profile | `confluence-collector` | Health, readiness, metrics, and `ServiceMonitor` template coverage when enabled. |
| OCI registry | `collector-oci-registry` | `oci-registry-collector` | Health, readiness, metrics, explicit target values, and workflow claim compatibility. |
| Terraform state | `collector-terraform-state` | `terraform-state-collector` | Claim-driven runtime with redaction key references and active coordinator. |
| AWS cloud | `collector-aws-cloud` | `aws-cloud-collector` | Claim-driven runtime with scoped account and region targets. |
| GCP cloud | not in base remote E2E | `gcp-cloud-collector` | Helm template coverage for explicit claimed-live mode, read-only redaction Secret mount, and ServiceMonitor coverage; live target proof belongs in the scoped GCP smoke gate. |
| Package registry | `collector-package-registry` | `package-registry-collector` | Bounded target or derived owned-package mode with metrics coverage. |
| SBOM attestation | `collector-sbom-attestation` | `sbom-attestation-collector` | Claim-driven SBOM/attestation source facts only. |
| Security alerts | `collector-security-alerts` | `security-alert-collector` | Provider access preflight plus claim-driven worker runtime. |
| CI/CD runs | not in base remote E2E | `cicd-run-collector` | Helm template coverage; live provider proof belongs in a scoped collector run. |
| PagerDuty | `collector-pagerduty` profile | `pagerduty-collector` | Profile-expanded remote proof plus charted claim-driven runtime. |
| Jira | `collector-jira` profile | `jira-collector` | Profile-expanded remote proof plus charted claim-driven runtime. |
| Grafana | `collector-grafana` observability profile | `grafana-collector` | Observability overlay proof plus charted metrics coverage. |
| Prometheus/Mimir | `collector-prometheus-mimir` observability profile | `prometheus-mimir-collector` | Observability overlay proof plus charted metrics coverage. |
| Loki | `collector-loki` observability profile | `loki-collector` | Observability overlay proof plus charted metrics coverage. |
| Tempo | `collector-tempo` observability profile | `tempo-collector` | Observability overlay proof plus charted metrics coverage. |
| Scanner worker | `scanner-worker` | `scanner-worker` | Isolated claim-driven worker; do not move scanner work into reducer lanes. |
| Vulnerability intelligence | `collector-vulnerability-intelligence` | `vulnerability-intelligence-collector` | Claim-driven vulnerability source facts, queue state, and metrics coverage. |
| Component extension | `component-extension-collector` profile | `component-extension-collector` | Trusted registry-backed extension host with health, readiness, metrics, NetworkPolicy, PDB, and `ServiceMonitor` template coverage when enabled. |

## Intentional Differences

- Helm does not install Postgres by default; Compose does.
- Helm expects an external graph backend by default; Compose starts NornicDB.
- Helm schema bootstrap normally runs as a hook Job; Compose uses the
  `db-migrate` one-shot service.
- `bootstrap-index` is a Compose/operator-run helper, not a steady-state Helm
  workload.
- Local Compose exposes host ports for developer access. Helm exposes cluster
  Services, optional Ingress or Gateway resources, and optional
  `ServiceMonitor` resources.
- Pprof is opt-in in both shapes and must stay private.

## Evidence Rules

Use remote Compose as the validation bed before Kubernetes or EKS promotion.
The parity verifier is static release evidence; it does not replace live
runtime-state proof, API/MCP readback, queue/completeness evidence, or
operator-local collector runs.

No-Regression Evidence: `scripts/test-verify-compose-helm-runtime-parity.sh`,
`scripts/verify-compose-helm-runtime-parity.sh`, `helm lint deploy/helm/eshu`,
the strict MkDocs build, and `git diff --check` validate the static parity
matrix, verifier behavior, chart renderability, docs, and repository hygiene.

No-Observability-Change: this gate changes no runtime behavior. It verifies
existing Compose healthchecks, metrics ports, Helm `ServiceMonitor` coverage,
Postgres wiring, graph wiring, and documented deployment topology.
