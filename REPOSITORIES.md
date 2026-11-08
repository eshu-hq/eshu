# Repository Map

Eshu is split across 31 repositories in the
[`eshu-hq`](https://github.com/eshu-hq) organization. This file is the full
list: what each repository holds, in plain words. The
[README](README.md) tells the story and gives you a first path; come here
when you need to find which repository owns a piece of the system.

How to read the map:

- **Start with `eshu`.** It holds the core and the documentation. Most
  questions end there, or in `eshu-deploy` when you want to run the system.
- **Satellites** hold pieces that were taken out of the core so they can be
  built, released, and run on their own: the API, the CLI, the MCP server,
  workers, and shared libraries.
- **Collectors** each read one outside system (a cloud account, a
  registry, a tracker) and hand Eshu facts (small, checked records of what
  they saw). Collectors emit source facts only; Eshu's reducer (the worker
  that joins facts into the graph) owns the linked truth. Before you
  enable a collector family, read
  [Collector And Reducer Readiness](docs/public/reference/collector-reducer-readiness.md).

## Core, clients, and libraries

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu](https://github.com/eshu-hq/eshu) | This repo: core parsing, facts, reducer, query, and docs. |
| [eshu-api](https://github.com/eshu-hq/eshu-api) | The extracted HTTP API service. |
| [eshu-cli](https://github.com/eshu-hq/eshu-cli) | The extracted command-line client. |
| [eshu-mcp](https://github.com/eshu-hq/eshu-mcp) | The extracted MCP server for AI assistants. |
| [eshu-sdk](https://github.com/eshu-hq/eshu-sdk) | Shared client and contract libraries. |
| [eshu-agent-kit](https://github.com/eshu-hq/eshu-agent-kit) | Helpers for coding agents that use Eshu. |

## Runtime services and workers

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu-ingester](https://github.com/eshu-hq/eshu-ingester) | The intake step (accepting and batching incoming items). Early skeleton today, not a running service. |
| [eshu-workflow](https://github.com/eshu-hq/eshu-workflow) | Collector scheduling and claim handling. |
| [eshu-webhook](https://github.com/eshu-hq/eshu-webhook) | Verified webhook intake for change events. |
| [eshu-extension-host](https://github.com/eshu-hq/eshu-extension-host) | Runs approved extension collectors safely. |
| [eshu-scanner-worker](https://github.com/eshu-hq/eshu-scanner-worker) | Heavy security scanning that emits facts. |

## Deployment

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) | Helm, Compose, and GitOps assets to run Eshu. |

## Collectors: cloud and infrastructure

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu-collector-aws-cloud](https://github.com/eshu-hq/eshu-collector-aws-cloud) | Collector: AWS cloud account facts. |
| [eshu-collector-azure-cloud](https://github.com/eshu-hq/eshu-collector-azure-cloud) | Collector: Azure cloud facts. |
| [eshu-collector-gcp-cloud](https://github.com/eshu-hq/eshu-collector-gcp-cloud) | Collector: Google Cloud facts. |
| [eshu-collector-kubernetes-live](https://github.com/eshu-hq/eshu-collector-kubernetes-live) | Collector: live Kubernetes cluster facts. |
| [eshu-collector-terraform-state](https://github.com/eshu-hq/eshu-collector-terraform-state) | Collector: Terraform state facts, redacted. |
| [eshu-collector-vault-live](https://github.com/eshu-hq/eshu-collector-vault-live) | Collector: Vault secrets posture, metadata only. |

## Collectors: supply chain and registries

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu-collector-oci-registry](https://github.com/eshu-hq/eshu-collector-oci-registry) | Collector: container image registry facts. |
| [eshu-collector-package-registry](https://github.com/eshu-hq/eshu-collector-package-registry) | Collector: package registry metadata. |
| [eshu-collector-sbom-attestation](https://github.com/eshu-hq/eshu-collector-sbom-attestation) | Collector: SBOM and attestation documents. |
| [eshu-collector-vulnerability-intelligence](https://github.com/eshu-hq/eshu-collector-vulnerability-intelligence) | Collector: vulnerability advisory facts. |
| [eshu-collector-security-alerts](https://github.com/eshu-hq/eshu-collector-security-alerts) | Collector: provider security alert facts. |

## Collectors: delivery and work

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu-collector-cicd-run](https://github.com/eshu-hq/eshu-collector-cicd-run) | Collector: CI/CD run and job facts. |
| [eshu-collector-jira](https://github.com/eshu-hq/eshu-collector-jira) | Collector: Jira work-item facts. |
| [eshu-collector-pagerduty](https://github.com/eshu-hq/eshu-collector-pagerduty) | Collector: PagerDuty incident facts. |
| [eshu-collector-confluence](https://github.com/eshu-hq/eshu-collector-confluence) | Collector: Confluence documentation facts. |

## Collectors: observability

| Repository | What it holds, in plain words |
| --- | --- |
| [eshu-collector-grafana](https://github.com/eshu-hq/eshu-collector-grafana) | Collector: Grafana dashboard and rule facts. A Go library embedded by the core host; there is no binary to run. |
| [eshu-collector-prometheus-mimir](https://github.com/eshu-hq/eshu-collector-prometheus-mimir) | Collector: Prometheus and Mimir target facts. |
| [eshu-collector-loki](https://github.com/eshu-hq/eshu-collector-loki) | Collector: Loki log-signal metadata. |
| [eshu-collector-tempo](https://github.com/eshu-hq/eshu-collector-tempo) | Collector: Tempo trace-signal metadata. |

## Keeping this map current

When a repository is added, renamed, or retired, update this file in the
same pull request. One row per repository, one plain sentence each. Keep
the README summary short; detail belongs here and in each repository's own
README.
