# Helm Rollout Proof

Use this gate before relying on a hosted Helm install, upgrade, or rollback.
It records a public-safe proof artifact for the chart version, app version,
image reference, rendered workload set, schema-bootstrap hook, API/MCP
readback, queue state, and first bounded query result.

The verifier does not persist rendered manifests, raw endpoint bodies, bearer
tokens, database DSNs, private hostnames, or source payloads. Keep private
values files and endpoint details outside the repository.

For hosted governance promotion, run the governance-specific wrapper after
preparing an operator values file with restricted egress and governance status
environment values:

```bash
scripts/verify-hosted-governance-helm-proof.sh \
  --out-dir .proof/governance-helm \
  --values values.eshu.yaml
```

That wrapper composes this rollout proof with hosted security posture and
NetworkPolicy egress proof, then writes only public-safe summary artifacts.

## Install Proof

Run the proof from the repository root after writing the same values file you
will use for the cluster:

```bash
scripts/verify-hosted-helm-rollout-proof.sh \
  --out-dir .proof/helm-install \
  --namespace eshu \
  --release eshu \
  --values values.eshu.yaml
```

The install phase runs:

- `helm lint`;
- `helm template`;
- `helm upgrade --install --dry-run --debug`;
- required workload checks for API, MCP, ingester, resolution engine, and schema
  bootstrap;
- schema-bootstrap hook checks for pre-install and pre-upgrade.

For live readback, pass API and MCP base URLs after the rollout is reachable:

```bash
scripts/verify-hosted-helm-rollout-proof.sh \
  --out-dir .proof/helm-install-live \
  --live-cluster \
  --namespace eshu \
  --release eshu \
  --values values.eshu.yaml \
  --api-base-url "$ESHU_API_BASE_URL" \
  --mcp-base-url "$ESHU_MCP_BASE_URL" \
  --api-token-env ESHU_API_KEY \
  --mcp-token-env ESHU_MCP_TOKEN \
  --first-query-path /api/v0/index-status
```

Token values are read from environment variables and are not written to the
artifact. The first query path must be a bounded read that is safe to summarize
publicly.

With `--live-cluster`, the verifier waits for the API, MCP, ingester, and
resolution-engine rollout resources and checks that the schema-bootstrap Job is
complete and failure-free.

## Upgrade Proof

Upgrade proof must declare durable-state and queue assumptions explicitly. The
gate fails if any required field is missing:

```json
{
  "durable_state": "postgres-backup-verified",
  "queue_state": "pre-and-post-queue-zero-captured",
  "graph_rebuild": "rebuild-from-postgres-facts-not-required",
  "preserved_volumes": "ingester-workspace-pvc-preserved"
}
```

Run:

```bash
scripts/verify-hosted-helm-rollout-proof.sh \
  --mode upgrade \
  --out-dir .proof/helm-upgrade \
  --namespace eshu \
  --release eshu \
  --values values.eshu.yaml \
  --upgrade-state upgrade-state.json
```

Capture queue depth, retry rows, failed rows, dead letters, and completeness
before and after the upgrade. If the graph must be rebuilt, say whether it will
be rebuilt from Postgres facts or from source systems.

## Rollback Proof

Rollback proof separates chart rollback from data recovery. The gate fails if
the declaration does not name all three decisions:

```json
{
  "helm_rollback": "helm rollback eshu previous-revision",
  "postgres_restore": "restore only if older image cannot read durable state",
  "graph_rebuild": "recreate graph and rerun bootstrap when graph volume is lost",
  "decision_point": "separate chart rollback from data restore"
}
```

Run:

```bash
scripts/verify-hosted-helm-rollout-proof.sh \
  --mode rollback \
  --out-dir .proof/helm-rollback \
  --namespace eshu \
  --release eshu \
  --values values.eshu.yaml \
  --rollback-state rollback-state.json
```

Helm rollback changes Kubernetes resources. It does not restore Postgres and it
does not guarantee graph projection compatibility. Restore Postgres only when
the older image cannot safely read durable state. Rebuild the graph when the
graph volume is lost, corrupt, or intentionally discarded.

## Artifacts

The verifier writes:

- `hosted-helm-rollout-proof.json` for machine-readable evidence;
- `hosted-helm-rollout-proof.md` for operator handoff.

Review the JSON artifact before sharing it. It should contain only versions,
safe image references, workload names, values digests, queue counters, truth or
freshness summaries, and declaration text that is already safe to publish.

No-Regression Evidence: `scripts/test-verify-hosted-helm-rollout-proof.sh`,
`helm lint deploy/helm/eshu`, the strict MkDocs build, and `git diff --check`
validate this proof gate, chart renderability, docs, and repository hygiene.

No-Observability-Change: this gate changes no runtime, collector, reducer,
query, graph, queue, API, MCP, or telemetry behavior. It records existing
operator signals from Helm, `/healthz`, `/readyz`, `/admin/status`, and the
configured bounded query path.
