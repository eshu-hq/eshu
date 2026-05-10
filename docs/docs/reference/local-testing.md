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

## Docs And Hygiene

Docs, `CLAUDE.md`, `AGENTS.md`, and README changes require:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```
