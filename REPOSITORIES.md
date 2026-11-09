# Repository Map

Eshu is split across 31 repositories in the
[`eshu-hq`](https://github.com/eshu-hq) organization. That's a lot of repos,
so here's the full list with one friendly line on each. The
[README](README.md) is the front door and gives you a first path. Come here
when you're trying to work out which repo owns the piece you're looking at.

A few tips before you dive in. Start with `eshu` itself. It holds the core
and the docs, and most questions end there. If you want to run the system,
you'll also need `eshu-deploy`.

The satellites hold pieces that were pulled out of the core so they can be
built, released, and run on their own. That's the API, the CLI, the MCP
server, a handful of workers, and the shared libraries.

Collectors are simpler. Each one reads a single outside system, like a cloud
account, a registry, or a tracker, and hands Eshu facts. Facts are just
small, checked records of what the collector saw. Collectors only emit those
source facts. The reducer, the worker that joins facts into the graph, owns
the linked truth. Before you enable a collector family, have a read of
[Collector And Reducer Readiness](docs/public/reference/collector-reducer-readiness.md).

## Core, clients, and libraries

- [eshu](https://github.com/eshu-hq/eshu) — this repo: core parsing, facts, reducer, query, and docs.
- [eshu-api](https://github.com/eshu-hq/eshu-api) — the HTTP API service, now living in its own repo.
- [eshu-cli](https://github.com/eshu-hq/eshu-cli) — the command-line client you run from your terminal.
- [eshu-mcp](https://github.com/eshu-hq/eshu-mcp) — the MCP server your AI assistants talk to.
- [eshu-sdk](https://github.com/eshu-hq/eshu-sdk) — shared client and contract libraries the other repos build on.
- [eshu-agent-kit](https://github.com/eshu-hq/eshu-agent-kit) — helpers for coding agents that use Eshu.

## Runtime services and workers

- [eshu-ingester](https://github.com/eshu-hq/eshu-ingester) — the intake step that accepts and batches incoming items. It's an early skeleton today, not a running service yet.
- [eshu-workflow](https://github.com/eshu-hq/eshu-workflow) — schedules collectors and hands out their work.
- [eshu-webhook](https://github.com/eshu-hq/eshu-webhook) — takes in verified webhooks when something changes.
- [eshu-extension-host](https://github.com/eshu-hq/eshu-extension-host) — runs approved extension collectors safely.
- [eshu-scanner-worker](https://github.com/eshu-hq/eshu-scanner-worker) — does the heavy security scanning and emits what it finds as facts.

## Deployment

- [eshu-deploy](https://github.com/eshu-hq/eshu-deploy) — everything you need to run Eshu: the Helm chart, Compose files, and GitOps examples.

## Collectors: cloud and infrastructure

- [eshu-collector-aws-cloud](https://github.com/eshu-hq/eshu-collector-aws-cloud) — reads your AWS accounts and reports what it finds.
- [eshu-collector-azure-cloud](https://github.com/eshu-hq/eshu-collector-azure-cloud) — the same idea for Azure.
- [eshu-collector-gcp-cloud](https://github.com/eshu-hq/eshu-collector-gcp-cloud) — and again for Google Cloud.
- [eshu-collector-kubernetes-live](https://github.com/eshu-hq/eshu-collector-kubernetes-live) — looks at your live Kubernetes clusters.
- [eshu-collector-terraform-state](https://github.com/eshu-hq/eshu-collector-terraform-state) — reads Terraform state, with secrets redacted.
- [eshu-collector-vault-live](https://github.com/eshu-hq/eshu-collector-vault-live) — checks Vault posture using metadata only, never secret values.

## Collectors: supply chain and registries

- [eshu-collector-oci-registry](https://github.com/eshu-hq/eshu-collector-oci-registry) — reads container image registries.
- [eshu-collector-package-registry](https://github.com/eshu-hq/eshu-collector-package-registry) — reads package registry metadata.
- [eshu-collector-sbom-attestation](https://github.com/eshu-hq/eshu-collector-sbom-attestation) — reads SBOM and attestation documents.
- [eshu-collector-vulnerability-intelligence](https://github.com/eshu-hq/eshu-collector-vulnerability-intelligence) — pulls in vulnerability advisories.
- [eshu-collector-security-alerts](https://github.com/eshu-hq/eshu-collector-security-alerts) — pulls in security alerts from your providers.

## Collectors: delivery and work

- [eshu-collector-cicd-run](https://github.com/eshu-hq/eshu-collector-cicd-run) — follows your CI/CD runs and jobs.
- [eshu-collector-jira](https://github.com/eshu-hq/eshu-collector-jira) — reads Jira work items.
- [eshu-collector-pagerduty](https://github.com/eshu-hq/eshu-collector-pagerduty) — reads PagerDuty incidents.
- [eshu-collector-confluence](https://github.com/eshu-hq/eshu-collector-confluence) — reads your Confluence docs.

## Collectors: observability

- [eshu-collector-grafana](https://github.com/eshu-hq/eshu-collector-grafana) — reads Grafana dashboards and rules. It's a Go library the core embeds, so there's no binary for you to run.
- [eshu-collector-prometheus-mimir](https://github.com/eshu-hq/eshu-collector-prometheus-mimir) — reads Prometheus and Mimir targets.
- [eshu-collector-loki](https://github.com/eshu-hq/eshu-collector-loki) — reads Loki log-signal metadata.
- [eshu-collector-tempo](https://github.com/eshu-hq/eshu-collector-tempo) — reads Tempo trace-signal metadata.

## Keeping this map current

When you add, rename, or retire a repository, update this file in the same
pull request. Keep it simple: one line per repository, in plain words. The
README stays short, and the detail lives here and in each repo's own README.
