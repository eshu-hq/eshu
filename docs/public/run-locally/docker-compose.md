# Docker Compose

Use Docker Compose when you want the full local product stack: API, MCP,
ingestion, reduction, Postgres, and a graph backend. Use
[Local binaries](local-binaries.md) when you are editing source code and want
fast rebuilds without containers.

## Choose A Compose File

| Compose file or profile | What it starts | Use it when |
| --- | --- | --- |
| `docker-compose.yaml` | Default stack: NornicDB, Postgres, migration, workspace setup, bootstrap indexing, API, MCP, ingester, and reducer. | You want the normal local product stack. |
| `docker-compose.neo4j.yml` | Neo4j compatibility stack. Includes the workflow-coordinator profile but not the default webhook-listener profile. | You need Neo4j compatibility checks. |
| `docker-compose.telemetry.yml` | Jaeger, OpenTelemetry collector, and OTLP export settings. | You need local traces or metrics export. |
| `--profile workflow-coordinator` | Adds the workflow coordinator to the default or Neo4j stack. | You are testing collector claims, scheduling, or control-plane behavior. |
| `--profile webhook-listener` | Adds the webhook listener to the default NornicDB stack. | You are testing webhook-driven freshness or external event ingestion locally. |
| `docker-compose.tier2-tfstate.yaml` | Layers MinIO, MinIO setup, active workflow coordination, and one Terraform state collector onto the default stack. | You are running the Tier-2 Terraform state drift proof. |
| `docker-compose.tier2-tfstate-v25.yaml` | Layers MinIO, two generation-specific MinIO setup jobs, active workflow coordination, and two Terraform state collectors. | You are running the v2.5 Terraform state drift proof across two fixture generations. |
| `docker-compose.remote-e2e.yaml` | Standalone remote proof stack with runtime services, preflight, workflow coordination, webhook listener, and cloud/package/registry collectors. | You are on an EC2 or VPN-attached host and need a full remote collector proof. |

## Default Stack

Start the default stack from the repository root:

```bash
docker compose up --build
```

The default stack uses NornicDB for graph storage and Postgres for relational
state, facts, queues, status, content, and recovery data.

| Service | Responsibility |
| --- | --- |
| `nornicdb` | Default graph database. |
| `postgres` | Facts, queues, status, content, recovery, and read-model state. |
| `db-migrate` | One-shot Postgres and graph schema bootstrap. |
| `workspace-setup` | One-shot `/data/.eshu`, `/data/repos`, and optional `.eshuignore` setup. |
| `bootstrap-index` | One-shot initial indexing and first projection pass. |
| `eshu` | HTTP API runtime on `localhost:8080`. |
| `mcp-server` | MCP HTTP runtime on `localhost:8081`. |
| `ingester` | Continuous repository sync, discovery, parsing, and fact emission. |
| `resolution-engine` | Reducer queue drain, graph projection, repair, and shared materialization. |

The NornicDB service defaults to a pinned multi-arch Docker manifest:
`timothyswt/nornicdb-cpu-bge:v1.1.0@sha256:65855ca2c9649020f7f9e29d2e0fbedf0bf9601457de233d87160ddbe4b473f0`.
Leave `NORNICDB_PLATFORM` unset for normal local runs. Docker selects the
`linux/arm64` image on Apple Silicon and the `linux/amd64` image on x86 hosts.

When testing a local NornicDB build, override image and platform together:

```bash
NORNICDB_IMAGE=nornicdb-main-eshu:cb20824-arm64 \
NORNICDB_PLATFORM=linux/arm64 \
docker compose up --build bootstrap-index
```

Eshu Compose sets `NORNICDB_EMBEDDING_ENABLED=false` and
`NORNICDB_PERSIST_SEARCH_INDEXES=true`. NornicDB does not currently document a supported switch that disables search/BM25 services entirely for graph-only deployments; track [orneryd/NornicDB#175](https://github.com/orneryd/NornicDB/issues/175) and do not invent a fake `NORNICDB_SEARCH_ENABLED` variable.

## Optional Profiles

The default `docker-compose.yaml` also defines services that are off unless you
enable a profile.

| Profile | Service | Provides | Use it when |
| --- | --- | --- | --- |
| `workflow-coordinator` | `workflow-coordinator` | Collector scheduling and claim ownership control plane on `18082`, with metrics on `19469`. | You need to inspect scheduler state or run an active claim proof. |
| `webhook-listener` | `webhook-listener` | HTTP intake for GitHub, GitLab, Bitbucket, and AWS freshness events on `18083`. | You need to test webhook-driven refresh behavior. |

Start the workflow coordinator in its default dark mode:

```bash
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

Use active mode only in fenced proof runs.

## Neo4j Stack

Start the Neo4j-backed stack with:

```bash
docker compose -f docker-compose.neo4j.yml up --build
```

The service shape and host ports match the default stack except the graph
service is `neo4j`, `ESHU_GRAPH_BACKEND=neo4j`, and the graph database name is
`neo4j`. The file includes the `workflow-coordinator` profile and omits the
default-stack `webhook-listener` profile. Use this stack only when you need
Neo4j compatibility behavior; use the default NornicDB stack for normal local
evaluation.

## Telemetry Overlay

The telemetry overlay layers tracing and metrics export onto the default or
Neo4j stack.

Default stack with telemetry:

```bash
docker compose -f docker-compose.yaml -f docker-compose.telemetry.yml up --build
```

Neo4j stack with telemetry:

```bash
docker compose -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml up --build
```

The overlay adds Jaeger on `http://localhost:16686`, an OpenTelemetry collector
on `4317`, `4318`, and `9464`, and OTLP env overrides for API, MCP, ingester,
reducer, bootstrap index, and workflow coordinator.

## Tier-2 Terraform State Overlay

Use `docker-compose.tier2-tfstate.yaml` for the Terraform state drift proof
with MinIO, active workflow coordination, and one Terraform state collector.
The overlay points runtime services at
`./tests/fixtures/tfstate_drift_tier2/repos/`.

| Service or override | Provides |
| --- | --- |
| `minio` | Local S3-compatible object store for Terraform state. |
| `minio-init` | One-shot MinIO bucket and object setup. |
| `workflow-coordinator` | Active claim coordinator for the proof. |
| `collector-terraform-state` | Terraform state collector worker. |
| Runtime fixture overrides | Point runtime services at the Tier-2 fixture repositories. |

The overlay pins MinIO images to immutable release tags
(`minio/minio:RELEASE.2025-09-07T16-13-09Z` and
`minio/mc:RELEASE.2025-08-13T08-35-41Z`). Confirm replacement tags exist on
Docker Hub before bumping them; do not switch to `:latest`.

Run commands live in
[Verification Gates](../reference/local-testing/verification-gates.md#targeted-graph-and-terraform-gates).

## Tier-2 Terraform State v2.5 Overlay

Use `docker-compose.tier2-tfstate-v25.yaml` for the two-generation Terraform
state drift proof. It does not stack on
`docker-compose.tier2-tfstate.yaml`; layer it with the default stack.

| Service or override | Provides |
| --- | --- |
| `minio` | Local S3-compatible object store shared by the two proof generations. |
| `minio-init-gen1` | One-shot object setup for the first fixture generation. |
| `minio-init-gen2` | Optional second-generation object setup behind the `gen2` profile. |
| `workflow-coordinator` | Active claim coordinator for both generations. |
| `collector-terraform-state-gen1` | Terraform state collector for generation 1. |
| `collector-terraform-state-gen2` | Terraform state collector for generation 2. |
| Runtime fixture overrides | Point runtime services at the v2.5 fixture repository tree. |

Use this overlay only for the v2.5 proof. Use the simpler Tier-2 overlay for
the single-generation proof.

## Remote Collector E2E Stack

Use `docker-compose.remote-e2e.yaml` on a VPN-attached or account-local test
machine for the default runtime plus claim-driven Terraform state, OCI
registry, package registry, AWS cloud, and optional Confluence collectors. It
is standalone and defaults the Compose project to `eshu-remote-e2e`.

For the service list, proof commands, AWS credential requirements, pprof ports,
and acceptance evidence, see
[Remote Collector E2E](../reference/local-testing/remote-collector-e2e.md)
and [Profiling And Concurrency](../reference/local-testing/profiling-and-concurrency.md#remote-e2e-worker-profiles).
The optional `docker-compose.remote-e2e.pprof.yaml` overlay binds host pprof
ports to `127.0.0.1`; keep profiler access private and use it only for focused
proof runs.

## Point CLI Commands At Compose

The API is available at `http://localhost:8080` by default. The MCP service is
available at `http://localhost:8081` by default.

For repository indexing from the host CLI, including the environment variables
needed to point `eshu scan` or `eshu index` at Compose stores, see
[Index repositories](../use/index-repositories.md#host-cli-into-compose-stores).

See [Connect MCP locally](mcp-local.md) for MCP client setup.
