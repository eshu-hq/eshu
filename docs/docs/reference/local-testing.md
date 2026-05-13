# Local Testing Reference

This page is the verification reference for engineers and agents changing Eshu.
For first-time local setup, use [Run Locally](../run-locally/index.md).

Use the smallest gate that proves the touched behavior, then run the hygiene
checks required by the files you changed. Do not call work ready without citing
the commands you actually ran.

For operator checks, use [Operate Eshu](../operate/index.md). For process
health, readiness, and completeness, use
[Health Checks](../operate/health-checks.md).

## Common Compose Environment

When running commands directly against the default local Compose stack:

```bash
export ESHU_GRAPH_BACKEND=nornicdb
export NEO4J_URI=bolt://localhost:7687
export NEO4J_USERNAME=neo4j
export NEO4J_PASSWORD=change-me
export DEFAULT_DATABASE=nornic
export ESHU_NEO4J_DATABASE=nornic
export ESHU_CONTENT_STORE_DSN=postgresql://eshu:change-me@localhost:15432/eshu
export ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu
```

For `docker-compose.neo4j.yml`, use `ESHU_GRAPH_BACKEND=neo4j` and database
`neo4j` instead.

## Confluence Collector Smoke

Use this when testing the Confluence collector against a real Atlassian site.
The collector is read-only against Confluence and writes documentation facts to
the local Postgres content store.

Load your local Jira/Confluence credential file, then normalize the env names
the collector expects:

```bash
set -a
source ~/.jira_api_credentials.conf
set +a

export ESHU_CONFLUENCE_BASE_URL="${CONFLUENCE_BASE_URL:-https://example.atlassian.net/wiki}"
export ESHU_CONFLUENCE_EMAIL="${JIRA_EMAIL:?set JIRA_EMAIL}"
export ESHU_CONFLUENCE_API_TOKEN="${JIRA_API_TOKEN:?set JIRA_API_TOKEN}"
export ESHU_CONFLUENCE_SPACE_KEY="${ESHU_CONFLUENCE_SPACE_KEY:-DEV}"
export ESHU_CONFLUENCE_PAGE_LIMIT="${ESHU_CONFLUENCE_PAGE_LIMIT:-25}"
export ESHU_CONFLUENCE_POLL_INTERVAL="${ESHU_CONFLUENCE_POLL_INTERVAL:-5m}"
```

Resolve the space key to the numeric space ID used by the Confluence API:

```bash
export ESHU_CONFLUENCE_SPACE_ID="$(
  curl -fsS \
    -u "${ESHU_CONFLUENCE_EMAIL}:${ESHU_CONFLUENCE_API_TOKEN}" \
    "${ESHU_CONFLUENCE_BASE_URL}/api/v2/spaces?keys=${ESHU_CONFLUENCE_SPACE_KEY}&limit=1" |
    jq -r '.results[0].id'
)"

test -n "$ESHU_CONFLUENCE_SPACE_ID"
test "$ESHU_CONFLUENCE_SPACE_ID" != "null"
```

Start Postgres, apply the data-plane schema, then run the collector:

```bash
docker compose up -d postgres

cd go
go run ./cmd/bootstrap-data-plane
go run ./cmd/collector-confluence
```

In another shell, check the status endpoint and stored facts:

```bash
curl -fsS http://127.0.0.1:8080/readyz

docker compose exec -T postgres \
  psql postgresql://eshu:change-me@localhost:5432/eshu \
  -c "select fact_kind, count(*) from fact_records where source_system = 'confluence' group by fact_kind order by fact_kind;"
```

Stop the collector with Ctrl-C after the first successful sync unless you are
testing repeated polling.

## OCI Registry Live Smokes

Use these only for maintainer/operator validation against real registries. They
are opt-in and must not run in default CI. Keep registry hosts, account IDs,
repository keys, image repository names, profiles, usernames, and tokens in
local shell config only.

JFrog Artifactory Docker/OCI repository validation uses public
`ESHU_JFROG_OCI_*` test variables. Maintainers who already keep private
`JFROG_*` aliases locally may map them before running the test:

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_JFROG_OCI_LIVE=1
export ESHU_JFROG_OCI_URL="${ESHU_JFROG_OCI_URL:-${JFROG_URL:-${JFROG_BASE_URL:-}}}"
export ESHU_JFROG_OCI_REPOSITORY_KEY="${ESHU_JFROG_OCI_REPOSITORY_KEY:-${JFROG_DOCKER_REPOSITORY_KEY:-}}"
export ESHU_JFROG_OCI_IMAGE_REPOSITORY="${ESHU_JFROG_OCI_IMAGE_REPOSITORY:-${JFROG_IMAGE_REPOSITORY:-}}"
export ESHU_JFROG_OCI_REFERENCE="${ESHU_JFROG_OCI_REFERENCE:-${JFROG_IMAGE_REFERENCE:-}}"
export ESHU_JFROG_OCI_USERNAME="${ESHU_JFROG_OCI_USERNAME:-${JFROG_USERNAME:-${JFROG_USER:-}}}"
export ESHU_JFROG_OCI_PASSWORD="${ESHU_JFROG_OCI_PASSWORD:-${JFROG_PASSWORD:-}}"
export ESHU_JFROG_OCI_BEARER_TOKEN="${ESHU_JFROG_OCI_BEARER_TOKEN:-${JFROG_ACCESS_TOKEN:-${JFROG_BEARER_TOKEN:-}}}"

cd go
go test ./internal/collector/ociregistry/jfrog -run TestLiveJFrog -count=1 -v
```

The JFrog challenge smoke can run with only `ESHU_JFROG_OCI_URL`. The tag-list
smoke also needs `ESHU_JFROG_OCI_IMAGE_REPOSITORY`.
`ESHU_JFROG_OCI_REPOSITORY_KEY` is required only when validating the
Artifactory `/artifactory/api/docker/<repository-key>` route. When
`ESHU_JFROG_OCI_REFERENCE` is set, the smoke resolves the manifest after
listing tags.

JFrog Artifactory package-feed validation exercises the package-registry
runtime against one explicit metadata document. The URL must return an
`artifactory_package` wrapper with package-native metadata and repository
topology. The smoke fetches that document through `HTTPMetadataProvider`,
parses it through the configured package-native parser, and requires package,
version, artifact, and repository-hosting fact envelopes:

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_JFROG_PACKAGE_LIVE=1
export ESHU_JFROG_PACKAGE_METADATA_URL="${ESHU_JFROG_PACKAGE_METADATA_URL:?set an Artifactory package metadata wrapper URL}"
export ESHU_JFROG_PACKAGE_ECOSYSTEM="${ESHU_JFROG_PACKAGE_ECOSYSTEM:-npm}"
export ESHU_JFROG_PACKAGE_NAME="${ESHU_JFROG_PACKAGE_NAME:?set the package name in the metadata document}"
export ESHU_JFROG_PACKAGE_NAMESPACE="${ESHU_JFROG_PACKAGE_NAMESPACE:-}"
export ESHU_JFROG_PACKAGE_REGISTRY="${ESHU_JFROG_PACKAGE_REGISTRY:-${JFROG_PACKAGE_REGISTRY:-${JFROG_URL:-${JFROG_BASE_URL:-}}}}"
export ESHU_JFROG_PACKAGE_USERNAME="${ESHU_JFROG_PACKAGE_USERNAME:-${JFROG_USERNAME:-${JFROG_USER:-}}}"
export ESHU_JFROG_PACKAGE_PASSWORD="${ESHU_JFROG_PACKAGE_PASSWORD:-${JFROG_PASSWORD:-}}"
export ESHU_JFROG_PACKAGE_BEARER_TOKEN="${ESHU_JFROG_PACKAGE_BEARER_TOKEN:-${JFROG_ACCESS_TOKEN:-${JFROG_BEARER_TOKEN:-}}}"

cd go
go test ./internal/collector/packageregistry/packageruntime -run TestLiveJFrogPackageFeed -count=1 -v
```

The package smoke is read-only and skips unless `ESHU_JFROG_PACKAGE_LIVE=1`.
It strips query strings and fragments from emitted source references and fails
if configured credential material appears in errors, source refs, or fact
payloads.

Amazon ECR private-registry validation uses AWS shared config plus explicit
repository coordinates:

```bash
export ESHU_ECR_OCI_LIVE=1
export ESHU_ECR_OCI_REGION="us-east-1"
export ESHU_ECR_OCI_REGISTRY_ID="123456789012"
export ESHU_ECR_OCI_REPOSITORY="team/api"
export ESHU_ECR_OCI_REFERENCE="latest"

cd go
go test ./internal/collector/ociregistry/ecr -run TestLiveECR -count=1 -v
```

`ESHU_ECR_OCI_REFERENCE` is optional. When set, the live smoke resolves the
manifest after listing tags. `ESHU_ECR_OCI_REGISTRY_ID` is used to build the
target registry host; the ECR authorization-token call itself does not pass a
registry id because AWS now marks that request field deprecated. Use
`ESHU_ECR_OCI_REGISTRY_HOST` instead when testing a nonstandard host shape.

Docker Hub validation defaults to a public official-library image and uses
anonymous pull tokens unless credentials are provided:

```bash
export ESHU_DOCKERHUB_OCI_LIVE=1
export ESHU_DOCKERHUB_OCI_REPOSITORY="library/busybox"
export ESHU_DOCKERHUB_OCI_REFERENCE="latest"

cd go
go test ./internal/collector/ociregistry/dockerhub -run TestLiveDockerHub -count=1 -v
```

Set `ESHU_DOCKERHUB_OCI_USERNAME` and `ESHU_DOCKERHUB_OCI_PASSWORD` when
validating private Docker Hub repositories or avoiding anonymous rate limits.

GHCR validation defaults to a public image and uses anonymous pull tokens unless
credentials are provided:

```bash
export ESHU_GHCR_OCI_LIVE=1
export ESHU_GHCR_OCI_REPOSITORY="stargz-containers/busybox"
export ESHU_GHCR_OCI_REFERENCE="1.32.0-org"

cd go
go test ./internal/collector/ociregistry/ghcr -run TestLiveGHCR -count=1 -v
```

Set `ESHU_GHCR_OCI_USERNAME` and `ESHU_GHCR_OCI_PASSWORD` when validating
private GHCR repositories or organization packages that deny anonymous pulls.

Harbor validation uses a Harbor endpoint and project/image repository path.
Robot-account usernames and secrets should come from local shell config only:

```bash
export ESHU_HARBOR_OCI_LIVE=1
export ESHU_HARBOR_OCI_URL="https://harbor.example.com"
export ESHU_HARBOR_OCI_REPOSITORY="project/image"
export ESHU_HARBOR_OCI_REFERENCE="latest"
export ESHU_HARBOR_OCI_USERNAME="robot$reader"
export ESHU_HARBOR_OCI_PASSWORD="local-secret"

cd go
go test ./internal/collector/ociregistry/harbor -run TestLiveHarbor -count=1 -v
```

Google Artifact Registry validation uses the Docker host shape documented by
Google, such as `us-west1-docker.pkg.dev`, and a
`PROJECT/REPOSITORY/IMAGE` path. Use a short-lived access token, credential
helper output, or service-account credential translated into local env vars:

```bash
export ESHU_GAR_OCI_LIVE=1
export ESHU_GAR_OCI_REGISTRY_HOST="us-west1-docker.pkg.dev"
export ESHU_GAR_OCI_REPOSITORY="project-id/repository/image"
export ESHU_GAR_OCI_REFERENCE="latest"
export ESHU_GAR_OCI_USERNAME="oauth2accesstoken"
export ESHU_GAR_OCI_PASSWORD="local-access-token"

cd go
go test ./internal/collector/ociregistry/gar -run TestLiveGAR -count=1 -v
```

Azure Container Registry validation uses the `<registry>.azurecr.io` host. For
token auth from `az acr login --expose-token`, pass the documented zero-GUID
username and the token through local env vars:

```bash
export ESHU_ACR_OCI_LIVE=1
export ESHU_ACR_OCI_REGISTRY_HOST="example.azurecr.io"
export ESHU_ACR_OCI_REPOSITORY="samples/artifact"
export ESHU_ACR_OCI_REFERENCE="latest"
export ESHU_ACR_OCI_USERNAME="00000000-0000-0000-0000-000000000000"
export ESHU_ACR_OCI_PASSWORD="local-access-token"

cd go
go test ./internal/collector/ociregistry/acr -run TestLiveACR -count=1 -v
```

## Discovery Advisory Playbook

Use this loop when a repository is slow, unexpectedly large, or timeout-heavy.
It is diagnostic evidence, not a stable API contract.

1. Capture the current shape:

    ```bash
    eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-before.json
    ```

2. Inspect `summary.content_files`, `summary.content_entities`,
   `top_noisy_directories`, `top_noisy_files`, `entity_counts.by_type`, and
   `skip_breakdown`.

3. Choose the narrowest config:

    - `.eshu/discovery.json` for auditable vendored, generated, archive, or
      copied third-party roots.
    - `preserved_path_globs` when a broad ignored root may contain authored
      code.
    - `.eshuignore` when a plain ignore is enough.

4. Rerun with a second report:

    ```bash
    eshu index /path/to/repo --discovery-report /tmp/eshu-discovery-after.json
    ```

5. Accept the config only when the after-report shows the intended skip reason
   and the repository became cheaper for the intended reason.

Do not change graph-write timeouts, global batch sizes, or NornicDB row caps
until the report proves the input shape is already correct.

## Quick Verification Matrix

| If you touched | Minimum verification |
| --- | --- |
| Docs, `CLAUDE.md`, `AGENTS.md`, or README files | `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml` |
| CLI/runtime wiring | `cd go && go test ./cmd/eshu ./cmd/api ./cmd/mcp-server -count=1` |
| Status/admin or completeness contract | `cd go && go test ./internal/status ./internal/query ./cmd/api -count=1` and `cd go && go vet ./internal/status ./internal/query ./cmd/api` |
| Parser platform or collector snapshot flow | `cd go && go test ./internal/parser ./internal/collector/discovery ./internal/collector -count=1` |
| Terraform provider-schema evidence or relationship extraction | `cd go && go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1` |
| Compose, Helm, or deployable runtime shape | `cd go && go test ./cmd/api ./cmd/bootstrap-index ./cmd/ingester ./cmd/reducer -count=1` and `helm lint deploy/helm/eshu` |
| Product-truth fixture registry or expected feature ownership | `./scripts/verify_product_truth_fixtures.sh` |
| Correlation DSL fixture corpus or compose verification lane | `./scripts/verify_correlation_dsl_compose.sh` |
| Graph-backed call-chain, caller/callee, or dead-code compose contract | `./scripts/verify_graph_analysis_compose.sh` |
| Facts-first indexing, queue, or resolution flow | `cd go && go test ./internal/projector ./internal/reducer ./internal/storage/postgres -count=1` |
| Local-authoritative graph backend or MCP local coding flow | `cd go && go test ./cmd/ingester ./internal/projector ./internal/storage/cypher ./internal/storage/neo4j -count=1` |
| Queue ack visibility or lease diagnosis | `cd go && go test ./internal/projector ./internal/reducer ./internal/status ./internal/storage/postgres ./internal/telemetry -count=1` and `cd go && go vet ./internal/projector ./internal/reducer ./internal/status ./internal/storage/postgres ./internal/telemetry` |
| Recovery, replay, or repair controls | `cd go && go test ./internal/recovery ./internal/runtime ./internal/status -count=1` |
| Facts-first telemetry or queue scaling | `cd go && go test ./internal/telemetry ./internal/runtime ./internal/projector ./internal/reducer -count=1` |
| Admin replay flow | `cd go && go test ./internal/query ./internal/recovery ./internal/runtime -count=1` |
| Go source, comments, package contracts, or generated docs | `cd go && golangci-lint run ./...` |
| Repo hygiene gates | `git diff --check` |

## Go Runtime Package Gate

Use this gate when validating the current runtime and collector wiring.

```bash
cd go
go test ./internal/parser ./internal/collector/discovery ./internal/content/shape \
  ./internal/collector ./cmd/collector-git ./cmd/collector-terraform-state \
  ./cmd/collector-aws-cloud \
  ./cmd/ingester ./cmd/bootstrap-index \
  ./internal/runtime ./internal/app ./internal/telemetry \
  ./internal/storage/cypher ./internal/storage/neo4j ./internal/storage/postgres \
  ./internal/projector ./internal/reducer ./cmd/reducer -count=1
```

## Local-Authoritative Gates

Before a local-authoritative run that executes local Eshu binaries, rebuild the
owner and child binaries and put the install directory on `PATH`.

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Use these focused gates when touching local-host startup, graph-backed query
compatibility, or NornicDB routing:

```bash
ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

Manual MCP local-authoritative smokes should end with:

```bash
eshu graph stop --workspace-root "$PWD"
eshu graph status --workspace-root "$PWD"
```

The status output should report no active owner for that workspace.

## Compose Verification Gates

Run these one at a time. They allocate local ports and reuse Compose project
state, so parallel runs will collide.

```bash
./scripts/verify_collector_git_runtime_compose.sh
./scripts/verify_projector_runtime_compose.sh
./scripts/verify_reducer_runtime_compose.sh
./scripts/verify_incremental_refresh_compose.sh
./scripts/verify_webhook_refresh_compose.sh
./scripts/verify_relationship_platform_compose.sh
./scripts/verify_admin_refinalize_compose.sh
./scripts/verify_graph_analysis_compose.sh
./scripts/verify_correlation_dsl_compose.sh
```

Use `./scripts/verify_product_truth_fixtures.sh` when changing a feature Eshu
claims as product truth across graph, evidence, API, MCP, CLI, or cleanup
workflows.

## NornicDB Grouped-Write Safety

Use this opt-in gate when touching grouped canonical writes or
`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES`.

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteSafetyProbe -count=1 -v
```

The stricter promotion gate is:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless-eshu-rollback \
ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteRollbackConformance -count=1 -v
```

Normal laptop runs should leave `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` unset.
Use the default phase-group path with the latest accepted NornicDB `main` build
until release-backed binary policy is settled.

## Terraform Provider-Schema Gate

Use this gate when touching Terraform provider schemas or schema-driven
relationship extraction.

```bash
cd go
go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1
```

The canonical packaged schemas live under
`go/internal/terraformschema/schemas/*.json.gz`.

## Terraform-State Parser Memory Gate

Use these checks when touching the Terraform-state parser or any code on the
`ParseStream` path. The streaming guarantee is enforced on every CI build by
`TestParseStream_PeakMemoryGate`; the benchmark and the 100 MiB proof are
for trend tracking and periodic large-scale validation.

```bash
# Hard gate. Runs on every CI build; fails on streaming regressions.
cd go
go test ./internal/collector/terraformstate -count=1 -run TestParseStream_PeakMemoryGate

# Trend-tracking benchmark across 1k, 10k, and 20k-resource fixtures.
cd go
go test -bench=BenchmarkParseStream_LargeState -benchmem -run=^$ \
    ./internal/collector/terraformstate

# 100 MiB env-gated proof for periodic large-scale validation.
cd go
ESHU_TFSTATE_100MIB_PROOF=true \
    go test ./internal/collector/terraformstate -count=1 \
    -run TestParseStreamLargeState100MiBStreamingProof -timeout 300s
```

## Terraform Config-vs-State Drift Compose Proofs

Use these gates when touching `DomainConfigStateDrift`, the Phase 3.5 drift
enqueue path, `terraformBackendCandidate` and related canonical-side reads, or
the `collector-terraform-state` binary itself.

### Tier-1: seeded-fact handler proof

Hand-seeded Postgres facts drive the production reducer to fire the drift
handler. Fast (under 2 min wall time) and avoids any collector binary.

```bash
bash scripts/verify_tfstate_drift_compose.sh
# optional artifact:
ESHU_TFSTATE_DRIFT_PROOF_OUT=docs/superpowers/proofs/$(date +%Y-%m-%d)-tfstate-drift-compose.md \
    bash scripts/verify_tfstate_drift_compose.sh
```

Asserts non-zero counter deltas on `eshu_dp_correlation_drift_detected_total`
for every drift kind in scope (`added_in_state`, `added_in_config`,
`removed_from_state`, `attribute_drift`, `removed_from_config`) plus the
ambiguous-owner WARN log.

### Tier-2: real collector chain proof

Brings up minio plus `eshu-collector-terraform-state` and the
workflow-coordinator in active mode so every fact the drift handler reads is
emitted by a real Eshu binary. Slower (3-5 min wall time) but proves the wire
Tier-1 cannot.

```bash
bash scripts/verify_tfstate_drift_compose_tier2.sh
```

Tier-2 covers buckets A (`added_in_state`), B (`added_in_config`),
D (ambiguous owner), and E (`attribute_drift`). Buckets C
(`removed_from_state`) and F (`removed_from_config`) stay Tier-1-only because
they need two collector generations of the same state or repo.

Both verifiers use distinct `COMPOSE_PROJECT_NAME` values and dynamic host
ports, so they can run side-by-side without colliding:

```bash
bash scripts/verify_tfstate_drift_compose.sh &
bash scripts/verify_tfstate_drift_compose_tier2.sh &
wait
```

## Webhook Refresh Compose Proof

Use this gate when touching `go/cmd/webhook-listener`,
`go/internal/webhook`, `WebhookTriggerStore`, or the ingester webhook-trigger
handoff path.

```bash
bash scripts/verify_webhook_refresh_compose.sh
```

The verifier creates a local bare Git remote, seeds the managed workspace
checkout, indexes the first generation, advances the remote default branch,
sends a signed GitHub `push` webhook to `eshu-webhook-listener`, and verifies
the queued trigger is handed off through the ingester to a new generation whose
content is visible through the HTTP API. It uses local Compose only; no live
GitHub webhook or public network endpoint is required.

## Runtime Tree Hygiene

The deployable runtime tree is Go-only. Use this check when confirming that
runtime implementation has not drifted into Python.

```bash
rg --files . -g '*.py' | rg -v '^(\\./)?tests/fixtures/'
```

Fixture data under `tests/fixtures/` and explicitly offline-only tooling can
still carry Python source when they are not part of the deployable runtime.

## Concurrency Tuning Reference

Set any variable to `1` to force sequential processing during debugging.

| Env var | Default | Service | Controls |
| --- | --- | --- | --- |
| `ESHU_PROJECTION_WORKERS` | `min(NumCPU, 8)` | Bootstrap Index | Concurrent bootstrap projection goroutines |
| `ESHU_SNAPSHOT_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner: `NumCPU` | Ingester / Bootstrap | Concurrent repository snapshot goroutines |
| `ESHU_PARSE_WORKERS` | `min(NumCPU, 8)`; local-authoritative owner: `NumCPU` | Ingester / Bootstrap | Concurrent file-parse workers inside a repository snapshot |
| `ESHU_PROJECTOR_WORKERS` | `min(NumCPU, 8)`; NornicDB local-authoritative: `NumCPU` | Ingester | Concurrent source-local projection workers |
| `ESHU_REDUCER_WORKERS` | NornicDB: `NumCPU`; Neo4j: `min(NumCPU, 4)` | Reducer | Concurrent reducer intent execution goroutines |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | NornicDB: `workers`; Neo4j: `workers * 4` capped at `64` | Reducer | Reducer intents leased per claim cycle |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | NornicDB: `1`; otherwise disabled | Reducer | Concurrent semantic entity materialization claims after source-local drain |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | Reducer | Maximum code-call shared intents scanned or loaded for one accepted repo/run before failing safely |
| `ESHU_SHARED_PROJECTION_WORKERS` | `1` | Reducer | Concurrent shared projection partition goroutines |
| `ESHU_SHARED_PROJECTION_PARTITION_COUNT` | `8` | Reducer | Partitions per shared projection domain |
| `ESHU_SHARED_PROJECTION_BATCH_LIMIT` | `100` | Reducer | Intents processed per partition batch |
| `ESHU_SHARED_PROJECTION_POLL_INTERVAL` | `5s` | Reducer | Shared projection poll interval |
| `ESHU_SHARED_PROJECTION_LEASE_TTL` | `60s` | Reducer | Partition lease time-to-live |

Validate queue work beyond the happy path:

- expired claims can be reclaimed
- overdue claims surface through status
- ack failures emit logs and metrics
- structured logs keep failure class, queue name, and work item identity

## Process Profiling

Each Go runtime binary (`eshu-api`, `eshu-mcp-server`, `eshu-ingester`,
`eshu-reducer`, `eshu-bootstrap-index`) ships an opt-in `net/http/pprof`
endpoint. It is disabled by default and gated by `ESHU_PPROF_ADDR`.

```bash
ESHU_PPROF_ADDR=:6060 eshu-ingester
# logs: pprof server listening addr=127.0.0.1:6060

go tool pprof -seconds=30 http://127.0.0.1:6060/debug/pprof/profile
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
curl -sS http://127.0.0.1:6060/debug/pprof/goroutine?debug=2 > goroutines.txt
```

A bare port like `:6060` is rewritten to `127.0.0.1:6060` so a typo cannot
silently expose profiling endpoints on a routable interface. Supply an explicit
host (`0.0.0.0:6060`, `192.0.2.5:6060`) only when you intend the broader
exposure; pprof reveals goroutine dumps, heap snapshots, and CPU profiles and
must be treated as credential-grade. Invalid values fail at startup, matching
the rest of the runtime contract.

Capture a profile while reproducing the slow path on the same host as the
runtime; loopback-only binding means `kubectl port-forward` (in a deployment)
or just running the binary locally (for dogfood) is the typical access path.

### CPU Capture During A Phase

For perf investigations that need a CPU profile from the ingester, or matched
profiles from both the ingester and a co-running NornicDB child process,
`scripts/capture-cpu-profile.sh` takes a run directory (containing `run.log`;
profiles land in `$RUN_DIR/profiles/`) plus the ingester pprof endpoint and an
optional NornicDB pprof endpoint. It waits for a configurable log marker, then
fires `curl pprof/profile?seconds=N` requests inside the same wall-clock
window. When a NornicDB endpoint is provided, heap, allocs, and goroutine
snapshots follow once the CPU window closes.

```bash
# In one shell, start the stack with both pprof endpoints enabled
ESHU_PPROF_ADDR=127.0.0.1:0 \
NORNICDB_PPROF_ENABLED=true \
NORNICDB_PPROF_LISTEN=127.0.0.1:19091 \
eshu graph start --workspace-root /path/to/repo --logs terminal \
  > /tmp/run-X/run.log 2>&1 &

# Scrape the ingester's actual pprof port from the run.log
# (ESHU_PPROF_ADDR=:0 lets the kernel pick a free port)
INGESTER_PPROF=$(rg -o '"pprof server listening","addr":"[^"]+","service_name":"ingester"' \
  /tmp/run-X/run.log | rg -o '127\.0\.0\.1:[0-9]+' | head -1)

# Fire the watcher; it captures when the marker fires
PPROF_CPU_S=20 PPROF_SLEEP_S=5 \
  scripts/capture-cpu-profile.sh /tmp/run-X "$INGESTER_PPROF" 127.0.0.1:19091
```

For ingester-only parser profiling, omit the third argument or pass `-` and
trigger from the stage before the parse window:

```bash
PPROF_LOG_MARKER='"stage":"pre_scan"' PPROF_CPU_S=30 PPROF_SLEEP_S=0 \
  scripts/capture-cpu-profile.sh /tmp/run-X "$INGESTER_PPROF" -
```

Defaults match the post-Path-D K8s entities-phase shape (~28s entities-phase
wall): marker is `canonical phase group completed.*phase=files`, sleep 5s,
20s CPU window. Earlier versions of this harness used 20s sleep / 60s window
which is too long for the post-Path-D shape — the run finishes mid-curl and
the captured profile is zero bytes. Tune `PPROF_CPU_S` and `PPROF_SLEEP_S`
to whichever phase you are profiling. Set `PPROF_LOG_MARKER` for a different
trigger line.

Profiles land in `$RUN_DIR/profiles/`:

- `ingester-cpu-${PPROF_CPU_S}s.pb.gz`
- `nornicdb-cpu-${PPROF_CPU_S}s.pb.gz` when a NornicDB endpoint is provided
- `nornicdb-{heap,allocs}.pb.gz` plus `*goroutines.txt` snapshots when a
  NornicDB endpoint is provided
- `watcher.log` (timestamps, per-side curl exit codes, byte counts)

If the CPU profile is zero bytes after the run, check `watcher.log` for the
curl exit codes: rc=7 means the pprof endpoint went away before the curl
finished (the eshu stack tore down mid-capture; shorten `PPROF_CPU_S` or
verify the stack stayed alive long enough).

## Docs And Hygiene

Docs, `CLAUDE.md`, `AGENTS.md`, and README changes require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
