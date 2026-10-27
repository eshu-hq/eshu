# Environment Variable Reference

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `bash scripts/generate-env-registry-doc.sh`. -->

This reference is generated from the code-owned registry in `go/internal/envregistry`. It covers the core platform subsystems and the hosted collectors. It is split into one page per subsystem family so each page stays under the repository's Markdown line cap. Run `eshu config validate` to check the current environment against it.

Subsystem anchors such as `#api` and `#auth` moved to the family pages when this reference was split, so a link to `env-registry.md#api` lands at the top of this index. The Subsystems table below links every subsystem section on its family page.

## Pages

| Page | Covers | Variables |
| --- | --- | --- |
| [API, auth, and MCP](env-registry/api-and-auth.md) | Listen addresses, authentication, browser sessions, OIDC and GitHub login, and the MCP server. | 48 |
| [Runtime and storage](env-registry/runtime-and-storage.md) | Shared runtime settings, Postgres, the graph backend, and backend conformance test knobs. | 55 |
| [Reducer and projection](env-registry/reducer-and-projection.md) | The reducer, projector, workflow coordinator, push hooks, and semantic providers. | 92 |
| [Ingester and collector framework](env-registry/collectors-platform.md) | The ingester's git collector, the component package manager, and the component-extension collector. | 38 |
| [Infrastructure and registry collectors](env-registry/collectors-infrastructure.md) | Cloud, Kubernetes, Vault, OCI registry, and package registry collectors. AWS cloud and Terraform state now collect from their extracted satellites (eshu-hq/eshu-collector-aws-cloud, eshu-hq/eshu-collector-terraform-state). | 35 |
| [Observability, delivery, and security collectors](env-registry/collectors-signals.md) | Grafana, Prometheus/Mimir, PagerDuty, CI/CD run, security alert, SBOM, and vulnerability collectors. Jira now collects from its extracted satellite (eshu-hq/eshu-collector-jira). Tempo now collects from its extracted satellite (eshu-hq/eshu-collector-tempo). Loki now collects from its extracted satellite (eshu-hq/eshu-collector-loki). | 35 |

## Subsystems

| Subsystem | Variables | Page |
| --- | --- | --- |
| [`api`](env-registry/api-and-auth.md#api) | 35 | API, auth, and MCP |
| [`auth`](env-registry/api-and-auth.md#auth) | 7 | API, auth, and MCP |
| [`backend-conformance`](env-registry/runtime-and-storage.md#backend-conformance) | 8 | Runtime and storage |
| [`collector`](env-registry/collectors-platform.md#collector) | 22 | Ingester and collector framework |
| [`collector-azure-cloud`](env-registry/collectors-infrastructure.md#collector-azure-cloud) | 8 | Infrastructure and registry collectors |
| [`collector-cicd-run`](env-registry/collectors-signals.md#collector-cicd-run) | 5 | Observability, delivery, and security collectors |
| [`collector-component-extension`](env-registry/collectors-platform.md#collector-component-extension) | 9 | Ingester and collector framework |
| [`collector-gcp-cloud`](env-registry/collectors-infrastructure.md#collector-gcp-cloud) | 6 | Infrastructure and registry collectors |
| [`collector-grafana`](env-registry/collectors-signals.md#collector-grafana) | 5 | Observability, delivery, and security collectors |
| [`collector-kubernetes-live`](env-registry/collectors-infrastructure.md#collector-kubernetes-live) | 3 | Infrastructure and registry collectors |
| [`collector-oci-registry`](env-registry/collectors-infrastructure.md#collector-oci-registry) | 7 | Infrastructure and registry collectors |
| [`collector-package-registry`](env-registry/collectors-infrastructure.md#collector-package-registry) | 5 | Infrastructure and registry collectors |
| [`collector-pagerduty`](env-registry/collectors-signals.md#collector-pagerduty) | 5 | Observability, delivery, and security collectors |
| [`collector-prometheus-mimir`](env-registry/collectors-signals.md#collector-prometheus-mimir) | 5 | Observability, delivery, and security collectors |
| [`collector-sbom-attestation`](env-registry/collectors-signals.md#collector-sbom-attestation) | 5 | Observability, delivery, and security collectors |
| [`collector-security-alerts`](env-registry/collectors-signals.md#collector-security-alerts) | 5 | Observability, delivery, and security collectors |
| [`collector-vault-live`](env-registry/collectors-infrastructure.md#collector-vault-live) | 6 | Infrastructure and registry collectors |
| [`collector-vulnerability-intelligence`](env-registry/collectors-signals.md#collector-vulnerability-intelligence) | 5 | Observability, delivery, and security collectors |
| [`component`](env-registry/collectors-platform.md#component) | 7 | Ingester and collector framework |
| [`coordinator`](env-registry/reducer-and-projection.md#coordinator) | 20 | Reducer and projection |
| [`graph`](env-registry/runtime-and-storage.md#graph) | 18 | Runtime and storage |
| [`mcp`](env-registry/api-and-auth.md#mcp) | 6 | API, auth, and MCP |
| [`postgres`](env-registry/runtime-and-storage.md#postgres) | 16 | Runtime and storage |
| [`projector`](env-registry/reducer-and-projection.md#projector) | 3 | Reducer and projection |
| [`push-hooks`](env-registry/reducer-and-projection.md#push-hooks) | 1 | Reducer and projection |
| [`reducer`](env-registry/reducer-and-projection.md#reducer) | 62 | Reducer and projection |
| [`runtime`](env-registry/runtime-and-storage.md#runtime) | 13 | Runtime and storage |
| [`semantic`](env-registry/reducer-and-projection.md#semantic) | 6 | Reducer and projection |
