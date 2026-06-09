# Hosted Operations Runbook

Use this runbook when taking hosted Eshu from remote Compose proof to a
Kubernetes or EKS rollout, and when operating the deployment after handoff. It
links the focused deployment, status, telemetry, MCP, upgrade, rollback, and
governance docs instead of duplicating them.

Preserve this proof order:

```text
remote Compose render/proof -> API and MCP proof -> Helm render -> Kubernetes
rollout -> GitOps or EKS promotion -> steady-state operations
```

Do not promote a deployment because pods are green. Promotion needs process
health, dependency readiness, queue/completeness evidence, and at least one
bounded API or MCP read that proves indexed state is usable.

## Source References

| Need | Use |
| --- | --- |
| Runtime ownership and health versus completeness | [Service Runtimes](../deployment/service-runtimes.md) |
| Local and remote Compose shape | [Docker Compose](../run-locally/docker-compose.md) |
| Remote all-collector proof | [Remote Collector E2E](../reference/local-testing/remote-collector-e2e.md) |
| Hosted runtime-state gate | [Remote E2E Runtime State](../reference/remote-e2e-runtime-state.md) |
| Kubernetes install flow | [Helm Quickstart](../deploy/kubernetes/helm-quickstart.md) |
| EKS shared-service path | [Deploy To EKS](../deploy/eks/index.md) |
| GitOps overlays | [Argo CD And GitOps](../deploy/kubernetes/argocd.md) |
| Production checklist | [Production Checklist](../deploy/kubernetes/production-checklist.md) |
| Health checks | [Health Checks](health-checks.md) |
| Telemetry and signal order | [Telemetry](telemetry.md) |
| MCP clients | [Connect MCP](../mcp/index.md) |
| Hosted onboarding and governance | [Hosted Project Onboarding](../deployment/hosted-onboarding.md), [Hosted Governance Posture](hosted-governance.md) |
| Upgrade and rollback | [Upgrade And Rollback](../deploy/kubernetes/upgrades-rollbacks.md) |

## Minimum Hosted Rollout Checklist

### 1. Prove The Compose Shape

Start with rendered configuration, then run remote proof only from a private
operator environment:

```bash
docker compose --env-file .env.remote-e2e.example \
  -f docker-compose.remote-e2e.yaml config

scripts/test-remote-e2e-hosted-compose-render.sh
```

For a real remote proof, keep the private env file, source selectors, provider
targets, and credential values outside the repository. Capture only aggregate
evidence:

- Compose project name and Eshu commit;
- enabled service/profile list;
- API and MCP health;
- workflow terminal state by source family;
- work-item counts, retry rows, failed rows, and dead letters;
- fact source counts by source family;
- queue-zero after reducer projection;
- NornicDB or Neo4j backend and graph schema/bootstrap state;
- pprof/log availability when scanner-worker or full-corpus work is in scope.

If the remote stack fails, classify the failure before changing settings:
render error, missing private env value, provider auth denial, stale image,
backend health, schema/index state, queue retry, graph-write timeout, or query
shape.

### 2. Prove API And MCP Usability

Run health checks and one bounded read through the API and MCP surfaces before
promoting to Kubernetes:

```bash
curl -fsS "$ESHU_SERVICE_URL/healthz"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/readyz"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/admin/status"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/api/v0/index-status"
```

For MCP, point clients at the MCP runtime, not the API runtime. Use
[Connect MCP](../mcp/index.md) for client-specific setup and keep bearer token
values out of committed files and issue text.

Accept the proof only when:

- `/healthz` or `/readyz` proves the process and dependencies;
- `/admin/status` shows no unexpected retry, failure, or dead-letter backlog;
- `/api/v0/index-status` or repository coverage proves completeness for the
  intended question;
- the bounded read returns a truthful empty, partial, or useful response with
  the expected freshness state.

### 3. Render Helm Before Cluster Changes

Render the exact values intended for the cluster:

```bash
helm template eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

Review workload names, images, environment variables, probes, ServiceMonitors,
ingester PVCs, graph backend settings, API auth secret names, MCP exposure, and
optional collector settings. Fix render errors before `helm upgrade --install`.

### 4. Install Or Upgrade Kubernetes

Follow [Helm Quickstart](../deploy/kubernetes/helm-quickstart.md) for a generic
cluster or [Deploy To EKS](../deploy/eks/index.md) for the EKS path. Install or
upgrade only after Postgres, graph backend, secrets, workspace storage, and
repository sync rules are ready.

```bash
helm upgrade --install eshu ./deploy/helm/eshu \
  --namespace eshu \
  -f values.eshu.yaml
```

Then check rollout and completeness:

```bash
kubectl -n eshu rollout status deployment/eshu-api
kubectl -n eshu rollout status deployment/eshu-mcp-server
kubectl -n eshu rollout status statefulset/eshu
kubectl -n eshu rollout status deployment/eshu-resolution-engine
```

Pod readiness is not enough. Repeat the API/MCP proof against the cluster
endpoint before giving teams an onboarding artifact.

### 5. Promote GitOps Deliberately

If Argo CD owns the rollout, render and review overlays before sync. Keep
secrets in the platform secret manager and keep environment-specific Helm
overrides in overlays. Confirm the sync order keeps Postgres and the graph
backend available before Eshu workloads that depend on them.

Promotion evidence should include:

- rendered Helm diff or reviewed overlay diff;
- image tag and chart source;
- Argo CD app sync status;
- API, MCP, ingester, reducer, and optional collector rollout status;
- the same API/MCP completeness proof used for direct Helm rollout.

## Steady-State Operator Checklist

Run this checklist daily during rollout hardening and after every upgrade:

| Area | Check |
| --- | --- |
| Runtime health | API, MCP, ingester, reducer, workflow coordinator, and enabled collectors have healthy `/healthz`, `/readyz`, `/admin/status`, and metrics where mounted. |
| Completeness | `/api/v0/index-status` reports expected freshness, queue state, and repository coverage for the teams using the service. |
| Queue behavior | Queue depth, oldest age, retry counts, failures, and dead letters are stable or explained by current ingestion. |
| Graph backend | Graph backend is reachable, schema/bootstrap state is current, and graph-write errors are absent from reducer status and logs. |
| Postgres | Backups are recent, restore proof exists, and connection limits match runtime scale. |
| Ingestion scope | Repository sync rules remain narrow enough for the intended teams; broad ingestion has an explicit operator decision. |
| Governance | Shared-token limitations, hosted governance posture, semantic provider policy, extension policy, redaction, and retention caveats are visible before onboarding. |
| MCP | MCP endpoint, token source, and first useful tool call are verified after every network or auth change. |
| Telemetry | Dashboards use `service_name` and `service_namespace`; alerts distinguish process health from convergence and completeness. |

## Signal Order During Incidents

Use the same order every time:

1. Check process health and dependency readiness.
2. Check `/admin/status` on the runtime that owns the failing stage.
3. Check queue depth, oldest age, retry rows, failed rows, and dead letters.
4. Check API index status, repository coverage, or MCP readback for the user
   question.
5. Use metrics to locate the service, phase, and backlog that changed.
6. Use logs for scope, generation, work item, domain, and failure class.
7. Use traces or pprof only after the failing service and stage are known.

Do not restart broadly, widen repository scope, lower worker counts, or switch
graph backends until the failure class is known.

## Upgrade, Rollback, And Restore

Before an upgrade, pin the image tag, render Helm, review workload and
environment changes, verify recent Postgres backups, and capture current queue
and completeness state. Then follow
[Upgrade And Rollback](../deploy/kubernetes/upgrades-rollbacks.md).

Rollback is not a database restore. If a new image writes durable Postgres state
that the old image cannot read, use the platform restore plan. If only the graph
volume is lost, preserve evidence when needed, recreate the graph volume, run
schema bootstrap, and rebuild projection from Postgres facts or source systems.

## Public Artifact Rules

Public docs, issues, PRs, onboarding artifacts, logs, metrics, and status
payloads must not contain:

- raw API tokens, provider keys, cloud credentials, private keys, or signed URLs;
- private hostnames, tenant domains, repository paths, local paths, or source
  payloads;
- raw prompts, provider responses, excerpts, or failure payloads;
- private env-file contents, credential handles when policy forbids them, or
  token-bearing endpoint URLs.

Use aggregate counts, safe scope classes, low-cardinality reason codes, policy
revision hashes, and public follow-up issue numbers in shared evidence.

No-Regression Evidence: strict MkDocs build and `git diff --check` validate
the public runbook, navigation, and repository hygiene.

No-Observability-Change: this is docs-only guidance. It changes no runtime,
query, collector, reducer, queue, graph, status, telemetry, API, MCP, Helm,
Compose, GitOps, or credential-loading behavior.
