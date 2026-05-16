# Docker Compose

Use Docker Compose when you want the full Eshu service stack on your laptop.
This is the easiest local path for trying API, MCP, ingestion, reduction,
Postgres, and the graph backend together.

## Start the default stack

From the repository root:

```bash
docker compose up --build
```

The default stack uses NornicDB for graph storage and Postgres for relational
state, facts, queues, status, content, and recovery data.

The NornicDB service defaults to a pinned multi-arch Docker manifest:
`timothyswt/nornicdb-cpu-bge:v1.1.0@sha256:65855ca2c9649020f7f9e29d2e0fbedf0bf9601457de233d87160ddbe4b473f0`.
Leave `NORNICDB_PLATFORM` unset for normal local runs. Docker will select the
`linux/arm64` image on Apple Silicon and the `linux/amd64` image on x86 hosts.

When testing a local NornicDB main build, override the image and platform
together:

```bash
NORNICDB_IMAGE=nornicdb-main-eshu:cb20824-arm64 \
NORNICDB_PLATFORM=linux/arm64 \
docker compose up --build bootstrap-index
```

It starts:

- NornicDB
- Postgres
- API
- MCP server
- ingester
- reducer
- bootstrap indexer

## Start the Neo4j stack

Neo4j is the explicit official alternative graph backend:

```bash
docker compose -f docker-compose.neo4j.yml up --build
```

Use this when you need to validate Neo4j behavior or migrate an existing Neo4j
deployment path.

## Add local telemetry

Jaeger and the OpenTelemetry collector are not part of the default Compose
files. Use the telemetry overlay when you want a local collector and trace UI
for developer or DevOps testing:

```bash
docker compose -f docker-compose.yaml -f docker-compose.telemetry.yml up --build
```

For Neo4j with the same local telemetry stack:

```bash
docker compose -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml up --build
```

The overlay adds:

- OpenTelemetry collector
- Jaeger
- OTLP trace and metric export settings for the Eshu runtimes

## Run the workflow coordinator proof profile

The workflow coordinator profile is off by default. It is useful when you want
to inspect the control plane or run an active claim proof without changing the
normal ingester path.

Dark-mode status proof:

```bash
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

Active claim proof requires every guard to be explicit:

```bash
export ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active
export ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true
export ESHU_COLLECTOR_INSTANCES_JSON='[{"instance_id":"collector-git-proof","collector_kind":"git","mode":"continuous","enabled":true,"bootstrap":true,"claims_enabled":true,"configuration":{"source":"local-compose","fairness_weight":1}}]'
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

Use active mode only for fenced claim validation. The Kubernetes chart remains
dark-only until the remote full-corpus proof, API checks, MCP checks, and
evidence truth checks are clean.

## Run the Tier-2 tfstate drift overlay

The overlay `docker-compose.tier2-tfstate.yaml` layers a minio object store,
the `collector-terraform-state` container, and the workflow-coordinator in
active mode on top of the default stack. It is consumed by
`scripts/verify_tfstate_drift_compose_tier2.sh` (see
[Local Testing](../reference/local-testing.md#tier-2-real-collector-chain-proof))
to prove the full collector chain end-to-end.

The overlay pins the minio images to immutable release tags
(`minio/minio:RELEASE.2025-09-07T16-13-09Z`,
`minio/mc:RELEASE.2025-08-13T08-35-41Z`). Docker Hub lags the upstream
GitHub releases page by a few days, so when bumping confirm the tag exists
on Hub before committing the pin change; do not switch to `:latest`.

The overlay also redirects `bootstrap-index`, `ingester`,
`resolution-engine`, and `eshu` to read repositories from
`./tests/fixtures/tfstate_drift_tier2/repos/` so the Tier-2 verifier owns its
fixture corpus and does not collide with the default ecosystem fixtures.

## Run the remote collector E2E stack

Use `docker-compose.remote-e2e.yaml` on a VPN-attached or account-local EC2
test machine when you want one Compose stack for the default runtime plus the
claim-driven Terraform state, OCI registry, package registry, and AWS cloud
collectors. The file is standalone so the remote proof does not mutate the
default local stack or the Tier-2 MinIO overlay.

The stack still uses the pinned NornicDB v1.1.0 image from the default Compose
file. It runs Postgres, NornicDB, schema migration, bootstrap indexing, API,
MCP, ingester, reducer, workflow coordinator, webhook listener, collector
workers, and an optional AWS freshness seeder. AWS cloud scans are freshness
trigger driven today; the seeder posts synthetic AWS Config change events into
the webhook listener so the workflow coordinator creates AWS scan work.

```bash
cp .env.remote-e2e.example .env.remote-e2e
# Edit .env.remote-e2e with the real account, region, tfstate object, and ECR repo.
docker compose --env-file .env.remote-e2e -f docker-compose.remote-e2e.yaml --profile seed up --build
```

Run without `--profile seed` if real AWS freshness events are already being
delivered to the webhook listener:

```bash
docker compose --env-file .env.remote-e2e -f docker-compose.remote-e2e.yaml up --build
```

The EC2 instance role must expose read-only inventory permissions for the target
account. `ReadOnlyAccess` is enough for the AWS cloud and ECR inventory calls
covered by this stack. Terraform state also needs `s3:GetObject` on the
configured state object, plus `kms:Decrypt` if that object uses a customer
managed KMS key. If Docker containers rely on the EC2 instance profile through
IMDSv2, set the instance metadata response hop limit to at least `2`; otherwise
the AWS SDK inside the containers may not be able to obtain role credentials.

Confluence is optional because it needs tenant credentials:

```bash
docker compose --env-file .env.remote-e2e -f docker-compose.remote-e2e.yaml --profile confluence up --build
```

No-Regression Evidence: the remote E2E stack is additive and validates with
`docker compose --env-file .env.remote-e2e.example -f docker-compose.remote-e2e.yaml config`;
it does not change existing Compose service defaults or worker counts.

Observability Evidence: every long-running remote E2E runtime keeps the shared
`/healthz`, `/readyz`, `/metrics`, and `/admin/status` surfaces. The proof uses
existing workflow, AWS freshness, AWS cloud, Terraform state, OCI registry,
package registry, reducer, ingester, API, MCP, Postgres, and NornicDB metrics
and status endpoints to distinguish scheduling, claim, scan, projection, graph,
and store failures.

## Point local CLI commands at Compose

The API is available at `http://localhost:8080` by default. For indexing into
the default NornicDB Compose stack, use:

```bash
export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI=bolt://localhost:7687
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=change-me
export DEFAULT_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu
export ESHU_CONTENT_STORE_DSN=postgresql://eshu:change-me@localhost:15432/eshu
```

Then index and query:

```bash
eshu index .
eshu list
eshu analyze callers process_payment
```

If you use `docker-compose.neo4j.yml`, set `ESHU_GRAPH_BACKEND=neo4j` and use
database `neo4j` instead of `nornic`.

## Local endpoints

- API: `http://localhost:8080`
- MCP server: `http://localhost:8081`
- Postgres: `localhost:15432`
- Graph Bolt endpoint: `localhost:7687`

When the telemetry overlay is enabled, Jaeger is available at
`http://localhost:16686`.

See [Connect MCP locally](mcp-local.md) for MCP client setup.
