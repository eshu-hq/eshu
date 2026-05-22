# Docker Compose

Use Docker Compose when you want Eshu's services running together: API, MCP,
ingestion, reduction, Postgres, and a graph backend. Use
[Local binaries](local-binaries.md) instead when you are editing source code and
want fast rebuilds without containers.

## Choose a Compose file

| Compose file or profile | What it starts | Use it when |
| --- | --- | --- |
| `docker-compose.yaml` | Default local stack with NornicDB, Postgres, migration, workspace setup, bootstrap indexing, API, MCP, ingester, and reducer. | You want the normal local product stack. Start here. |
| `docker-compose.neo4j.yml` | Same core runtime shape as the default stack, but with Neo4j instead of NornicDB. It includes the workflow-coordinator profile, but not the default-stack webhook-listener profile. | You need Neo4j compatibility checks or a Neo4j-backed migration path. |
| `docker-compose.telemetry.yml` | Adds Jaeger, the OpenTelemetry collector, and OTLP export settings to the main runtimes. | You need local traces, metrics export, or operator debugging data. |
| `--profile workflow-coordinator` | Adds the workflow coordinator to the default or Neo4j stack. | You are testing collector claims, scheduling, or control-plane behavior. |
| `--profile webhook-listener` | Adds the webhook listener to the default NornicDB stack. | You are testing webhook-driven freshness or external event ingestion locally. |
| `docker-compose.tier2-tfstate.yaml` | Layers MinIO, MinIO setup, active workflow coordination, and one Terraform state collector onto the default stack. | You are running the Tier-2 Terraform state drift proof. |
| `docker-compose.tier2-tfstate-v25.yaml` | Layers MinIO, two generation-specific MinIO setup jobs, active workflow coordination, and two Terraform state collectors. | You are running the v2.5 Terraform state drift proof across two fixture generations. |
| `docker-compose.remote-e2e.yaml` | Standalone remote proof stack with runtime services, preflight, workflow coordination, webhook listener, and cloud/package/registry collectors. | You are on an EC2 or VPN-attached host and need a full remote collector proof. |

## Default stack

Start the default stack from the repository root:

```bash
docker compose up --build
```

The default stack uses NornicDB for graph storage and Postgres for relational
state, facts, queues, status, content, and recovery data.

| Service | Provides | Default host port |
| --- | --- | --- |
| `nornicdb` | Graph database for Eshu's code-to-cloud graph. | `7474`, `7687` |
| `postgres` | Facts, work queues, status, content store, and recovery data. | `15432` |
| `db-migrate` | One-shot data-plane schema migration for Postgres and the graph backend. | none |
| `workspace-setup` | One-shot setup for `/data/.eshu`, `/data/repos`, and optional `.eshuignore` input. | none |
| `bootstrap-index` | One-shot initial repository indexing and first projection pass. | `19467` metrics |
| `eshu` | HTTP API runtime. The API mounts `/metrics` on the same container listener. | `8080`, `19464` metrics |
| `mcp-server` | MCP server for assistant and tool clients. MCP mounts `/metrics` on the same container listener. | `8081`, `19468` metrics |
| `ingester` | Continuous repository sync, discovery, parsing, and fact emission. | `19465` metrics |
| `resolution-engine` | Reducer queue drain, graph projection, repair flows, and shared materialization. | `19466` metrics |

The NornicDB service defaults to a pinned multi-arch Docker manifest:
`timothyswt/nornicdb-cpu-bge:v1.1.0@sha256:65855ca2c9649020f7f9e29d2e0fbedf0bf9601457de233d87160ddbe4b473f0`.
Leave `NORNICDB_PLATFORM` unset for normal local runs. Docker selects the
`linux/arm64` image on Apple Silicon and the `linux/amd64` image on x86 hosts.

When testing a local NornicDB main build, override the image and platform
together:

```bash
NORNICDB_IMAGE=nornicdb-main-eshu:cb20824-arm64 \
NORNICDB_PLATFORM=linux/arm64 \
docker compose up --build bootstrap-index
```

NornicDB graph-only note: Eshu Compose sets `NORNICDB_EMBEDDING_ENABLED=false`
and `NORNICDB_PERSIST_SEARCH_INDEXES=true`. NornicDB does not currently document a supported switch that disables search/BM25 services entirely for
graph-only deployments. Eshu tracks that upstream gap in
[orneryd/NornicDB#175](https://github.com/orneryd/NornicDB/issues/175); until
that exists, do not add a fake `NORNICDB_SEARCH_ENABLED` style variable.

## Optional default-stack profiles

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

Use active mode only in fenced proof runs. The Kubernetes chart remains dark-only
until the remote full-corpus proof, API checks, MCP checks, and evidence truth
checks are clean.

## Neo4j stack

Start the Neo4j-backed stack with:

```bash
docker compose -f docker-compose.neo4j.yml up --build
```

| Service | Provides | Default host port |
| --- | --- | --- |
| `neo4j` | Alternative graph database for backend compatibility testing. | `7474`, `7687` |
| `postgres` | Facts, work queues, status, content store, and recovery data. | `15432` |
| `db-migrate` | One-shot schema migration for Postgres and Neo4j. | none |
| `workspace-setup` | One-shot local workspace setup. | none |
| `bootstrap-index` | One-shot initial indexing and first projection pass. | `19467` metrics |
| `eshu` | HTTP API runtime. The API mounts `/metrics` on the same container listener. | `8080`, `19464` metrics |
| `mcp-server` | MCP server for assistant and tool clients. MCP mounts `/metrics` on the same container listener. | `8081`, `19468` metrics |
| `ingester` | Continuous repository sync, discovery, parsing, and fact emission. | `19465` metrics |
| `workflow-coordinator` | Optional workflow coordinator profile. | `18082`, `19469` metrics |
| `resolution-engine` | Reducer queue drain, graph projection, repair flows, and shared materialization. | `19466` metrics |

Use this stack only when you need Neo4j behavior. Use the default NornicDB stack
for normal local evaluation.

## Telemetry overlay

The telemetry overlay is additive. It does not replace the default or Neo4j
stack; it layers tracing and metrics export onto whichever stack you choose.

Default stack with telemetry:

```bash
docker compose -f docker-compose.yaml -f docker-compose.telemetry.yml up --build
```

Neo4j stack with telemetry:

```bash
docker compose -f docker-compose.neo4j.yml -f docker-compose.telemetry.yml up --build
```

| Service or change | Provides | Default host port |
| --- | --- | --- |
| `jaeger` | Local trace UI. | `16686` |
| `otel-collector` | OpenTelemetry collector for runtime telemetry. | `4317`, `4318`, `9464` |
| Runtime env overrides | OTLP endpoint and metrics export settings for API, MCP, ingester, reducer, bootstrap index, and workflow coordinator. | none |

Jaeger is available at `http://localhost:16686` when this overlay is enabled.

## Tier-2 Terraform state overlay

Use `docker-compose.tier2-tfstate.yaml` with the verifier script:

```bash
scripts/verify_tfstate_drift_compose_tier2.sh
```

This overlay layers Terraform state drift proof services onto the default stack.
It also redirects `bootstrap-index`, `ingester`, `resolution-engine`, and `eshu`
to `./tests/fixtures/tfstate_drift_tier2/repos/` so the proof owns its fixture
corpus.

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

## Tier-2 Terraform state v2.5 overlay

Use `docker-compose.tier2-tfstate-v25.yaml` with the v2.5 verifier script:

```bash
scripts/verify_tfstate_drift_compose_tier2_v25.sh
```

This overlay is for two-generation Terraform state drift proof. It does not
stack on top of `docker-compose.tier2-tfstate.yaml`; use it with the default
stack.

| Service or override | Provides |
| --- | --- |
| `minio` | Local S3-compatible object store shared by the two proof generations. |
| `minio-init-gen1` | One-shot object setup for the first fixture generation. |
| `minio-init-gen2` | Optional second-generation object setup behind the `gen2` profile. |
| `workflow-coordinator` | Active claim coordinator for both generations. |
| `collector-terraform-state-gen1` | Terraform state collector for generation 1. |
| `collector-terraform-state-gen2` | Terraform state collector for generation 2. |
| Runtime fixture overrides | Point runtime services at the v2.5 fixture repository tree. |

Use this overlay for the v2.5 proof only. Use the simpler Tier-2 overlay when
you need the single-generation proof.

## Remote collector E2E stack

Use `docker-compose.remote-e2e.yaml` on a VPN-attached or account-local EC2 test
machine when you want one Compose project for the default runtime plus
claim-driven Terraform state, OCI registry, package registry, AWS cloud, and
optional Confluence collectors. The file is standalone so the remote proof does
not mutate the default local stack or the Tier-2 MinIO overlays.

The Compose project defaults to `eshu-remote-e2e`, so its volumes are isolated
from the default stack even when both files are run from the same checkout.

```bash
cp .env.remote-e2e.example .env.remote-e2e
# Edit .env.remote-e2e with the account, region, state object, and ECR repo.
docker compose --env-file .env.remote-e2e -f docker-compose.remote-e2e.yaml --profile seed up --build
```

Run without `--profile seed` if real AWS freshness events are already delivered
to the webhook listener. Enable the `confluence` profile only when tenant
credentials are available.

The example env defaults to smoke mode with the fixture corpus. For a
full-corpus gate, set `ESHU_REMOTE_E2E_CORPUS_MODE=full`, point
`ESHU_FILESYSTEM_HOST_ROOT` at the absolute corpus path, and set either
`ESHU_REMOTE_E2E_MIN_REPOSITORY_COUNT` or
`ESHU_REMOTE_E2E_EXPECTED_REPOSITORY_COUNT`. The preflight service prints the
effective root and repository counts before indexing and fails early when a
full-corpus run is still mounted on the default fixtures.

For the service list, proof commands, AWS credential requirements, pprof ports,
and acceptance evidence, see
[Remote Collector E2E](../reference/local-testing/remote-collector-e2e.md)
and [Profiling And Concurrency](../reference/local-testing/profiling-and-concurrency.md#remote-e2e-worker-profiles).

## Point local CLI commands at Compose

The API is available at `http://localhost:8080` by default. The MCP service is
available at `http://localhost:8081` by default.

For repository indexing from the host CLI, including the environment variables
needed to point `eshu scan` or `eshu index` at Compose stores, see
[Index repositories](../use/index-repositories.md#host-cli-into-compose-stores).

## Local endpoints

| Endpoint | URL or address |
| --- | --- |
| API | `http://localhost:8080` |
| API metrics | `http://localhost:19464/metrics` |
| MCP server | `http://localhost:8081` |
| MCP metrics | `http://localhost:19468/metrics` |
| Postgres | `localhost:15432` |
| Graph Bolt endpoint | `localhost:7687` |
| Ingester metrics | `http://localhost:19465/metrics` |
| Resolution engine metrics | `http://localhost:19466/metrics` |
| Bootstrap index metrics | `http://localhost:19467/metrics` |
| Workflow coordinator, when profile enabled | `http://localhost:18082` |
| Workflow coordinator metrics, when profile enabled | `http://localhost:19469/metrics` |
| Webhook listener, default stack only and when profile enabled | `http://localhost:18083` |
| Jaeger, with telemetry overlay | `http://localhost:16686` |

See [Connect MCP locally](mcp-local.md) for MCP client setup.
