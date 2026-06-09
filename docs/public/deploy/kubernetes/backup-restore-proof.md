# Hosted backup and restore proof

Use this gate after the platform backup system has performed a restore drill or
after an operator has rebuilt graph projection from preserved Postgres facts.
It validates the public evidence packet only. It does not run `pg_restore`,
copy backup contents, inspect private object storage, or connect to a cluster.

The authoritative recovery boundary is:

```text
Postgres facts, queues, status, content, and workflow rows -> schema bootstrap
-> reducer projection replay -> graph/backend readback -> API and MCP proof
```

With the default NornicDB backend, graph storage is rebuildable projection
state. A graph-only rebuild must preserve Postgres unless the operator is
intentionally running a full source-system recollection in an isolated restore
environment.

## Evidence input

Create the input JSON from operator-local restore evidence. Keep raw backup
artifacts, restore logs, private hostnames, repository paths, signed URLs,
machine paths, credentials, and API/MCP transcripts outside the repository.

```json
{
  "schema_version": 1,
  "proof_id": "hosted-restore-drill-20260609",
  "generated_at": "2026-06-09T18:00:00Z",
  "mode": "clean_restore",
  "backup": {
    "artifact_handle": "backup-generation-20260609",
    "age_seconds": 600,
    "checksum_present": true,
    "encrypted": true
  },
  "restore": {
    "status": "succeeded",
    "duration_seconds": 92,
    "failure_class": "none",
    "target_scope_class": "isolated_restore_environment"
  },
  "graph_rebuild": {
    "status": "succeeded",
    "postgres_preserved": true,
    "schema_bootstrap_rerun": true,
    "projection_replayed": true,
    "full_recollection_explicit": false
  },
  "parity": {
    "status": "match",
    "drift_count": 0
  },
  "queue": {
    "pending": 0,
    "retrying": 0,
    "failed": 0,
    "dead_letter": 0
  },
  "readback": {
    "api_status": "pass",
    "mcp_status": "pass",
    "first_query_status": "pass"
  },
  "security": {
    "artifact_contents_platform_owned": true,
    "secret_scan": "passed",
    "private_locator_scan": "passed"
  }
}
```

`artifact_handle` must be an opaque public-safe handle, not a bucket path,
signed URL, host, repository path, or local path. The output summary
intentionally omits the handle.

## Run the gate

```bash
scripts/test-verify-hosted-backup-restore-proof.sh

scripts/verify-hosted-backup-restore-proof.sh \
  --input restore-proof.json \
  --output-json restore-proof.summary.json \
  --output-markdown restore-proof.summary.md
```

The default maximum backup age is 86,400 seconds. Override it only when the
platform recovery point objective permits a different bound:

```bash
scripts/verify-hosted-backup-restore-proof.sh \
  --input restore-proof.json \
  --output-json restore-proof.summary.json \
  --output-markdown restore-proof.summary.md \
  --max-backup-age-seconds 172800
```

## Failure classes

The verifier fails closed when evidence is missing, corrupt, stale, partial, or
unsafe to publish. It rejects:

- missing or invalid JSON;
- missing backup artifact handle, checksum proof, or encryption proof;
- backup age above the configured maximum;
- restore status other than `succeeded` or failure class other than `none`;
- graph rebuild evidence that does not preserve Postgres, rerun schema
  bootstrap, and replay projection work;
- parity drift after restore or rebuild;
- nonzero pending, retrying, failed, or dead-letter queue counts;
- missing API, MCP, or first-query readback proof;
- private locators, raw transcripts, machine paths, credential-like keys, or
  token-shaped values.

## Operator proof order

1. Restore Postgres in an isolated environment with platform-owned tooling.
2. Run schema bootstrap against the restored Postgres and graph backend.
3. Replay projection work or rebuild graph state from preserved facts and
   workflow rows.
4. Verify queue terminal state is zero.
5. Compare restored readback against the expected source-of-truth summary.
6. Capture one bounded API read, one bounded MCP read, and first-query status.
7. Produce only the aggregate JSON accepted by this gate.

No-Regression Evidence: `scripts/test-verify-hosted-backup-restore-proof.sh`,
`scripts/verify-hosted-backup-restore-proof.sh`, strict MkDocs build, and
`git diff --check` validate the public restore-proof contract and docs.

No-Observability-Change: this gate adds offline evidence validation only. It
does not change runtime, query, collector, reducer, queue, graph, status,
telemetry, API, MCP, Helm, Compose, GitOps, or credential-loading behavior.
