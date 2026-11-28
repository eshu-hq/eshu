<!-- docs-catalog
title: Deploy Eshu
description: Landing hub for choosing a deployment topology, understanding service runtimes, configuring Postgres read routing, and onboarding teams.
type: operate
audience: operator, practitioner
entrypoint: true
landing: true
-->

# Deploy Eshu

Use this section when you are standing up, sizing, or onboarding teams onto a
shared Eshu deployment.

Eshu separates deployment manifests and Helm charts into the companion
[eshu-deploy](https://github.com/eshu-hq/eshu-deploy) repository, while this
repository owns the service contracts, runtime boundaries, and operational
behavior documented below.

## Choose a deployment shape

| Need | Topology | Start here |
| --- | --- | --- |
| Try Eshu in minutes with zero credentials | Synthetic local demo | [Demo Mode](../getting-started/demo-mode.md) |
| Index a repo on your laptop with stdio MCP | Local CLI + embedded graph | [Run Locally](../run-locally/index.md) |
| Run the full API + MCP + Postgres + graph stack locally | Docker Compose (`eshu-deploy`) | [Docker Compose Guide](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/run-locally/docker-compose.md) |
| Deploy a shared team service on Kubernetes | Helm chart (`eshu-deploy`) + external Postgres & Neo4j | [Helm Quickstart](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/helm-quickstart.md) |
| Deploy on AWS EKS | EKS blueprints (`eshu-deploy`) | [Deploy to EKS](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/eks/index.md) |

See [Deployment Topology Flow](../concepts/flows/deployment-topology.md) for a
visual diagram of every service, store, and ingress path.

## Service runtime ownership

Each Eshu runtime has one job and one scaling shape. Before tuning replicas or
debugging a rollout, check which service owns the boundary:

| Page | What it covers |
| --- | --- |
| [Service Runtimes Overview](service-runtimes.md) | Full runtime map, health vs completeness probes, and MCP HTTP auth rules. |
| [Core Services](service-runtimes-core.md) | API, MCP server, ingester StatefulSet, webhook listener, intake host, and coordinator. |
| [Collector Services](service-runtimes-collectors.md) | Hosted collectors (cloud, OCI, Terraform state, SBOM, alerts, incidents) and satellite scanners. |
| [Bootstrap Services](service-runtimes-bootstrap.md) | Schema bootstrap Job and one-shot `bootstrap-index` seeding. |

## Storage, scaling, and onboarding

| Topic | Guide |
| --- | --- |
| Canonical graph backend | **Neo4j** is the supported primary graph backend for shared deployments. See [Graph Backend Operations](../reference/graph-backend-operations.md). |
| Postgres read scaling | Route read-only query traffic to a Postgres replica with writer-checkpoint fencing: [Postgres Read Routing](postgres-read-routing.md). |
| Team onboarding | Hand a team a scoped API/MCP setup and verified first-answer report: [Hosted Project Onboarding](hosted-onboarding.md). |
| Governance before onboarding | Confirm auth, redaction, semantic-provider, and extension rules: [Hosted Governance Posture](../operate/hosted-governance.md). |
| Credential hygiene | Remove accidentally exposed credential files from indexed state: [Credential File Cleanup](credential-file-cleanup.md). |
| Production checks & rollbacks | [Kubernetes Production Checklist](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/production-checklist.md) and [Upgrades and Rollbacks](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/upgrades-rollbacks.md). |

## Next after deployment

Once pods are Ready and `/api/v0/index-status` confirms ingestion is current,
switch to [Operate Eshu](../operate/index.md) for monitoring, tuning, and
symptom-first [Troubleshooting](../operate/troubleshooting.md).
