<!-- docs-catalog
title: Operate Eshu
description: Routes operators to support triage, health, freshness, telemetry, governance, and maintenance runbooks.
type: operate
audience: operator
entrypoint: false
landing: true
-->

# Operate Eshu

Use this section when Eshu is already deployed and you need to support users,
investigate a stale answer, tune performance, or run a safe maintenance
procedure.

For initial installation, service ownership, and scaling topologies, see
[Deploy Eshu](../deployment/index.md) and the
[Flow Atlas](../concepts/flows/index.md).

Eshu separates API reads, MCP transport, ingestion, reduction, Postgres state,
and graph storage. A healthy API only proves the read surface is up — it does
not prove that ingestion finished or that the resolution engine drained queued
work.

## Support and triage

| Symptom or need | Start here |
| --- | --- |
| Start from a symptom (stale answers, slow indexing, auth errors, failing queries) | [Troubleshooting](troubleshooting.md) |
| First-response checklist for an overnight alert | [3 AM Operator Runbook](3am-operator-runbook.md) |
| Resolution engine queue depth or oldest age keeps growing | [Stuck Reducer Queue Runbook](stuck-reducer-queue-runbook.md) |
| A vulnerability scope keeps its old generation after repeated guard refusals | [Vulnerability Source Held Stale](vulnerability-source-held-stale.md) |
| Hold or hand over the backfill park, or clear a schema Job xmin refusal | [Park-Holder Runbook](park-holder-runbook.md) |

## Health, freshness, and tuning

| Need | Start here |
| --- | --- |
| Check process health, readiness, and index completeness | [Health Checks](health-checks.md) |
| Choose metrics, traces, logs, or `/admin/status` | [Telemetry](telemetry.md) |
| Track freshness convergence across scopes | [Freshness and Convergence](freshness-convergence.md) |
| Tune slow indexing, stale answers, graph timeouts, or memory pressure | [Tuning Playbook](tuning-playbook.md) |
| Tune Postgres pools, queues, hot tables, or search-index write pressure | [Postgres Tuning](../reference/postgres-tuning.md) |
| Alert thresholds for shared deployments | [Hosted Ops Alert Pack](hosted-ops-alert-pack.md) |

## Access, governance, and extensions

| Need | Start here |
| --- | --- |
| Check hosted governance posture before onboarding a team | [Hosted Governance Posture](hosted-governance.md) |
| Configure MCP client auth and OAuth 2.1 discovery | [MCP Client Authentication](mcp-client-auth.md) and [MCP OAuth 2.1 Discovery](mcp-oauth-discovery.md) |
| Manage identity, tokens, sessions, roles, grants, and revocation | [User Management Runbook](user-management-runbook.md) |
| Decide whether a community extension may run in hosted Eshu | [Hosted Extension Policy](hosted-extension-policy.md) |
| Review hosted security posture and SAML advisory guidance | [Hosted Security Posture Gate](hosted-security-posture.md) and [SAML Library Advisory](saml-library-advisory.md) |

## Maintenance, validation, and cutovers

| Need | Start here |
| --- | --- |
| Move from remote Compose proof to hosted Kubernetes operations | [Hosted Operations Runbook](hosted-operations-runbook.md) |
| Rebuild the canonical graph from Postgres facts | [Rebuild the Graph From Facts](graph-rebuild-from-facts.md) |
| Hand a collector back to its in-tree producer after a cutover | [Collector Cutover Rollback](collector-cutover-rollback.md) |
| Rebuild the webhook satellite or intake host forward after endpoint moves | [Webhook Satellite Roll Forward](webhook-satellite-roll-forward.md) |
| Prove a local change or validate a hosted deployment | [Local Testing](../reference/local-testing.md) and [Cloud Validation](../reference/cloud-validation.md) |
| Prepare Kubernetes rollout checks or roll back a chart | [Kubernetes Production Checklist](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/production-checklist.md) and [Upgrade and Rollback](https://github.com/eshu-hq/eshu-deploy/blob/main/docs/kubernetes/upgrades-rollbacks.md) |

## Steady-state path

```text
ingester -> Postgres facts/queues -> resolution engine -> Neo4j graph + Postgres read models -> API / MCP reads
```

Bootstrap indexing is a one-shot seed or recovery helper. It is not the normal
freshness mechanism for a running service.
