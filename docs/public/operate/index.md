# Operate Eshu

Use these docs when Eshu is running and you need to keep it healthy, validate a
change, or debug a stale answer.

Eshu has separate runtimes for API reads, MCP transport, ingestion, reduction,
Postgres state, and graph storage. A healthy API only proves the read surface
is up. It does not prove that ingestion finished or that the reducer drained
queued work.

## Start Here

| Need | Start here |
| --- | --- |
| Check process health, readiness, and completeness | [Health Checks](health-checks.md) |
| Choose metrics, traces, logs, or admin/status | [Telemetry](telemetry.md) |
| Tune slow indexing, stale answers, graph timeouts, or memory pressure | [Tuning Playbook](tuning-playbook.md) |
| Work through common symptoms | [Troubleshooting](troubleshooting.md) |
| Move from remote Compose proof to hosted Kubernetes operations | [Hosted Operations Runbook](hosted-operations-runbook.md) |
| Check hosted governance posture before onboarding a team | [Hosted Governance Posture](hosted-governance.md) |
| Prepare identity, token, session, role, grant, audit, and revocation proof | [User Management Runbook](user-management-runbook.md) |
| Decide whether a community extension may run in hosted Eshu | [Hosted Extension Operator Policy](hosted-extension-policy.md) |
| Understand service ownership | [Service Runtimes](../deployment/service-runtimes.md) |
| Prove a local change before handoff | [Local Testing Reference](../reference/local-testing.md) |
| Validate hosted or Kubernetes behavior | [Cloud Validation Runbook](../reference/cloud-validation.md) |
| Operate AWS cloud collection | [AWS Cloud Collector](../services/collector-aws-cloud.md) |
| Prepare Kubernetes rollout checks | [Kubernetes Production Checklist](../deploy/kubernetes/production-checklist.md) |
| Roll forward or back | [Upgrade and Rollback](../deploy/kubernetes/upgrades-rollbacks.md) |

## Runtime Model

The steady-state path is:

```text
ingester -> Postgres facts/queues -> reducer -> graph/content stores -> API/MCP reads
```

Bootstrap indexing is a one-shot seed or recovery helper. It is not the normal
freshness mechanism for a running service.
