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

## Semantic Provider Modes

The default stack is a no-external-provider semantic mode with deterministic
local hash search vectors enabled. Leave
`ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`,
`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`, and
`ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` unset to keep source-only indexing,
documentation fact reads, API reads, MCP tools, and reducer projection free of
external provider calls. Compose sets
`ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` to
`${ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER:-auto_hash}`, so API, MCP, and reducer
share the same selector. `auto_hash` yields to one governed `search_documents`
provider profile when provider profile and source policy variables are set, and
otherwise uses the local hash embedder. In the no-provider mode
`/api/v0/status/semantic-extraction` still reports semantic extraction as
unavailable or policy-disabled instead of failing ingestion, while semantic
search can use local vector rows built from curated search documents.

The `eshu`, `mcp-server`, and `resolution-engine` services pass optional
semantic provider variables through when they are set in the Compose
environment. API, MCP, and reducer use the same selector: `hash` and
`local_hash` force deterministic local vectors, `auto_hash` prefers one
governed `search_documents` provider profile and falls back to local hash, and
an unset local override allows provider-only auto-selection. If more than one
eligible search provider profile is configured, set
`ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID`.

First-run diagnostic: after bootstrap and reducer projection finish, call
`/api/v0/search/semantic` with a bounded semantic search request and inspect
`retrieval_state`:

```bash
curl -sS http://localhost:8080/api/v0/search/semantic \
  -H 'Content-Type: application/json' \
  -d '{"repo_id":"local","query":"deployment entrypoints","mode":"semantic","limit":5,"timeout_ms":1000}'
```

`semantic_active` means compatible vector rows are ready and serving
`mode=semantic` requests. `index_unready` means the selected embedder is
configured but vector rows are not ready yet; check reducer progress and retry
after projection catches up. `semantic_unavailable` means the service was
started without either an auto/local hash selector or a governed
`search_documents` provider profile. Use
`/api/v0/status/semantic-extraction` to inspect real-provider governance state;
the no-external-provider default keeps semantic extraction unavailable while
local semantic search remains no-network.

For a local gateway or Ollama-style development profile, use the
`local_dev_profile` credential-source kind and store only a profile handle in
the provider JSON. Do not put a local endpoint token, provider key, prompt body,
or provider response in the environment:

```bash
export ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='{"profiles":[{"profile_id":"semantic-local-ollama","provider_kind":"ollama","credential_source":{"kind":"local_dev_profile","handle":"ollama-local"},"model_id":"llama3.1:8b","source_classes":["documentation"],"source_policy_configured":true}]}'
export ESHU_SEMANTIC_EXTRACTION_POLICY_JSON='{"policy_id":"semantic-local-docs","enabled":true,"rules":[{"rule_id":"docs-local","provider_profile_id":"semantic-local-ollama","source_classes":["documentation"],"scopes":[{"kind":"repository","id":"local"}],"source_allowlist":[{"kind":"all","value":"*"}],"settings":{"limits":{"max_chunk_bytes":8192,"max_tokens_per_chunk":2048,"max_daily_tokens":50000},"redaction":{"mode":"strict","policy_ref":"semantic-redaction-v1"},"retention":{"posture":"metadata_only","prompt":"none","response":"hash_only"}}}]}'
docker compose up --build eshu mcp-server
```

For a secret-backed development profile, keep the provider key in the
referenced environment variable and keep the profile JSON to handles and model
metadata:

```bash
# Load DEEPSEEK_API_KEY from a private env file or shell secret manager first.
export ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='{"profiles":[{"profile_id":"semantic-docs-deepseek","provider_kind":"deepseek","credential_source":{"kind":"environment_variable","handle":"DEEPSEEK_API_KEY"},"model_id":"deepseek-chat","source_classes":["documentation"],"source_policy_configured":true}]}'
export ESHU_SEMANTIC_EXTRACTION_POLICY_JSON='{"policy_id":"semantic-local-docs","enabled":true,"rules":[{"rule_id":"docs-local","provider_profile_id":"semantic-docs-deepseek","source_classes":["documentation"],"scopes":[{"kind":"repository","id":"local"}],"source_allowlist":[{"kind":"path_prefix","value":"docs/"}],"settings":{"limits":{"max_chunk_bytes":8192,"max_tokens_per_chunk":2048,"max_daily_tokens":100000,"max_daily_cost_micros":2500000},"redaction":{"mode":"strict","policy_ref":"semantic-redaction-v1"},"retention":{"posture":"metadata_only","prompt":"none","response":"hash_only"}}}]}'
docker compose up --build eshu mcp-server
```

The examples above configure semantic extraction for documentation source
classes. Search embeddings use the same provider-profile registry but require
`source_classes:["search_documents"]`, `embedding_dimensions`, an endpoint
profile id, and source policy for search documents. Hosted deployments should
use the governed provider-profile and source-policy path, with credentials
supplied by Kubernetes Secrets, Vault-style handles, or workload identity rather
than shell history or Compose command lines.

No-Regression Evidence: `go test ./internal/runtime -run
'TestDefaultComposePassesSemanticSearchConfigToReadersAndVectorBuilder|TestDockerComposeDocsDescribeSemanticProviderModes'
-count=1` proves the default Compose stack enables deterministic local hash
search vectors for API, MCP, and reducer, passes optional semantic profile,
policy, and search-provider selector config only when set, and keeps bootstrap
and ingester on deterministic no-provider defaults.

Observability Evidence: local hash and provider-backed vector builds are still
surfaced through bounded search-vector build results, Postgres query/exec spans,
semantic-search route spans, retrieval state fields, and vector metadata failure
classes. Provider-backed profiles additionally report redacted provider profile
status. Compose adds no raw prompt, credential, endpoint, provider body, path,
or document id to logs or metric labels.

The NornicDB service defaults to a pinned multi-arch Docker manifest:
`timothyswt/nornicdb-cpu-bge:v1.1.6@sha256:e448ccf5cd1c1ff994c6316a1a2c5b06b19b4a3c6545660fa04f43c457625692`.
Leave `NORNICDB_PLATFORM` unset for normal local runs. Docker selects the
`linux/arm64` image on Apple Silicon and the `linux/amd64` image on x86 hosts.

When testing a local NornicDB build, override image and platform together:

```bash
NORNICDB_IMAGE=nornicdb-main-eshu:cb20824-arm64 \
NORNICDB_PLATFORM=linux/arm64 \
docker compose up --build bootstrap-index
```

Eshu Compose sets these NornicDB graph-lane controls:

- `NORNICDB_EMBEDDING_ENABLED=false`
- `NORNICDB_SEARCH_BM25_ENABLED=false`
- `NORNICDB_SEARCH_VECTOR_ENABLED=false`
- `NORNICDB_SEARCH_BM25_WARMING=lazy`
- `NORNICDB_SEARCH_VECTOR_WARMING=lazy`
- `NORNICDB_PERSIST_SEARCH_INDEXES=false`

These are graph-lane safeguards, not an Eshu semantic-search contract. BM25,
vector indexing, and embedding generation stay off for the canonical graph
database. Lazy warming remains the supported fallback if an operator enables a
specific search index for a deliberate proof run.
Optional semantic extraction is configured separately through provider profiles
and source policy; see
[Semantic Enrichment Posture](../reference/semantic-enrichment-posture.md).

## Ask Eshu

The Ask Eshu endpoint (`POST /api/v0/ask`) and the `ask` MCP tool are
default-off. Both require `ESHU_ASK_ENABLED=true` **and** a valid
`agent_reasoning` provider profile in `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`.
When either condition is unmet, the endpoint returns `503` and
`GET /api/v0/status/answer-narration` reports `provider_unavailable`.

The `eshu` and `mcp-server` services pass these variables through when set in
the Compose environment; they are empty by default so the default stack
behaviour is unchanged.

| Variable | Purpose | Default |
| --- | --- | --- |
| `ESHU_ASK_ENABLED` | Set to `true` to enable the ask endpoint and tool. | off |
| `ESHU_ASK_NARRATION_ENABLED` | Set to `true` to permit governed answer narration (requires `ESHU_ASK_ENABLED=true`). | off |
| `ESHU_ASK_MAX_ITERATIONS` | Maximum agent-loop reasoning iterations. Raise it for weaker providers that need more tool-call rounds to reach a supported answer instead of returning empty partial answers; values are clamped to a ceiling of `32`. | engine default (`6`) |
| `ESHU_ASK_MAX_TOOL_CALLS_PER_TURN` | Maximum tool calls dispatched per completion turn; same parse, default, and clamp rules as `ESHU_ASK_MAX_ITERATIONS` (ceiling `16`). | engine default |
| `DEEPSEEK_API_KEY` | API key for DeepSeek; referenced by the example profile below. Keep in a private env file — never commit a real key. | unset |

To enable Ask Eshu with a DeepSeek provider, load the key from a private env
file or shell secret manager and set the profile JSON before starting Compose:

```bash
# Load DEEPSEEK_API_KEY from a private env file or shell secret manager first.
export DEEPSEEK_API_KEY=<your-key>
export ESHU_ASK_ENABLED=true
export ESHU_ASK_NARRATION_ENABLED=true
export ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='{"profiles":[{"profile_id":"ask-deepseek","provider_kind":"deepseek","credential_source":{"kind":"environment_variable","handle":"DEEPSEEK_API_KEY"},"model_id":"deepseek-chat","source_classes":["agent_reasoning"]}]}'
docker compose up --build eshu mcp-server
```

Verify the posture after the stack is healthy:

```bash
curl -sS http://localhost:8080/api/v0/status/answer-narration | jq .
```

`available` means all gates are open. `provider_unavailable` means
`ESHU_ASK_ENABLED` is unset or the provider adapter failed to construct (check
that `DEEPSEEK_API_KEY` is exported and the profile JSON is valid).
`provider_traffic_disabled` means the profile loaded but narration was not
opened (`ESHU_ASK_NARRATION_ENABLED` is unset or not `true`).

No-Regression Evidence: default Compose behaviour (no env vars set) is
unchanged — the `${VAR:-}` passthrough expands to empty string, which is
equivalent to the variable being absent. `ESHU_ASK_ENABLED` unset or empty
causes `IsAskEnabled` in `go/internal/askwiring/askwiring.go` to return false
and the handler to remain in its default 503 state.

No-Observability-Change: these are optional passthrough variables. No new
metrics, spans, or log fields are added by this change. Existing ask-path
telemetry surfaces only when `ESHU_ASK_ENABLED=true` is explicitly set.

## Optional Profiles

The default `docker-compose.yaml` also defines services that are off unless you
enable a profile.

| Profile | Service | Provides | Use it when |
| --- | --- | --- | --- |
| `workflow-coordinator` | `workflow-coordinator` | Collector scheduling and claim ownership control plane on `18082`, with metrics on `19469`. | You need to inspect scheduler state or run an active claim proof. |
| `component-extension-collector` | `component-extension-collector` | Process-backed component extension worker on `18084`, with metrics on `19470`. | You need a fenced process-adapter component claim proof after the coordinator has planned work. |
| `webhook-listener` | `webhook-listener` | HTTP intake for GitHub, GitLab, Bitbucket, AWS, PagerDuty, and Jira freshness events on `18083`. | You need to test webhook-driven refresh behavior. |

Start the workflow coordinator in its default dark mode:

```bash
docker compose --profile workflow-coordinator up --build workflow-coordinator
```

The default Compose coordinator mounts the shared Eshu data volume at `/data`
and reads component activations from `/data/.eshu/components`. This makes
operator-installed component packages visible to the coordinator, but trust
still fails closed by default:

- `ESHU_COMPONENT_TRUST_MODE=disabled`
- `ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=false`
- `ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=dark`

Use active mode and component allowlists only in fenced proof runs. A
component-backed collector needs `ESHU_COMPONENT_TRUST_MODE=allowlist`,
matching `ESHU_COMPONENT_ALLOW_IDS` and `ESHU_COMPONENT_ALLOW_PUBLISHERS`,
`ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE=active`, and
`ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true` before it can create workflow
claims.

For a signed artifact proof, use `ESHU_COMPONENT_TRUST_MODE=strict` with the
same allowlist plus `ESHU_COMPONENT_PROVENANCE_CERTIFICATE_IDENTITY`,
`ESHU_COMPONENT_PROVENANCE_OIDC_ISSUER`, and optional
`ESHU_COMPONENT_COSIGN_BINARY`. Keep registry credentials in Cosign's normal
auth configuration, not in Compose values.

Start the process-backed component extension collector only after installing
and enabling a trusted process-adapter component. It reads the same shared
registry path as the coordinator:

```bash
docker compose --profile component-extension-collector up --build component-extension-collector
```

For the PagerDuty reference component parity proof, use the dedicated overlay.
It builds the fixture component image, installs and enables
`dev.eshu.examples.pagerduty` into the shared component home, starts the
coordinator in active claims mode, and runs the component-extension collector:

```bash
docker build -t eshu:local -f Dockerfile .
docker compose -p pd-ce-proof \
  -f docker-compose.yaml \
  -f docs/public/run-locally/docker-compose.component-extension-pagerduty.yaml \
  --profile component-extension-collector up -d --build
scripts/run-remote-e2e-pagerduty-component-extension.sh --artifacts <run-dir>
```

Set `ESHU_COMPONENT_COLLECTOR_INSTANCE_ID` when more than one trusted
claim-capable activation exists. If the activation config has a `host` block,
the coordinator plans work for that public `sourceSystem` and `scope.id`, and
the worker sends `host.scope.kind` in the SDK claim. OCI adapter execution is
not implemented in this worker; use it only for process-adapter proof runs until
the digest-pinned OCI adapter path lands.

No-Observability-Change: component registry wiring only exposes existing local
component-manager state to the workflow coordinator and component extension
collector. The coordinator continues to report claim, work-item, run-status,
and completeness signals through `/admin/status` and
`eshu_runtime_coordinator_*` metrics; the worker reports process-backed claim
state through the existing collector `/admin/status`, failure-class, commit
counter, and metrics paths.

No-Regression Evidence: component registry Compose wiring is covered by
`go test ./internal/runtime -run 'TestDefaultComposeWiresComponent(RegistryToWorkflowCoordinator|ExtensionCollector)' -count=1`.

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
registry, package registry, provider security alerts, vulnerability
intelligence, scanner-worker, AWS cloud, and optional Confluence, Jira, and
PagerDuty collectors. Add `docker-compose.remote-e2e.observability.yaml` for
optional Grafana, Prometheus/Mimir, Loki, and Tempo workers. The stack is
standalone and defaults the Compose project to `eshu-remote-e2e`.
The root file is the stable operator entrypoint; it includes foundation,
runtime/collector, and optional seed fragments that own the service groups.
Run `scripts/verify-compose-helm-runtime-parity.sh` before treating Compose and
Helm deployment evidence as aligned. The verifier checks this stack against the
static service-contract shape in
[Runtime Parity Matrix](../deploy/kubernetes/runtime-parity-matrix.md).
Its package-registry and vulnerability-intelligence derived target planners run
with `planning_mode=single_pass` so representative proofs stay bounded by the
configured derived target budget instead of rotating through a new owned-package
slice on every coordinator reconcile bucket.
The scanner-worker service accepts the same
`ESHU_COLLECTOR_INSTANCES_JSON` analyzer configuration as Helm. For
`sbom_generation`, keep `sbom_targets[].root_path` runtime-local and private;
Compose proofs should report target count, fact count, CPU, memory, queue
state, retries, dead letters, and pprof availability rather than raw repository
paths.
The remote proof profile also overrides the code-call sidecar defaults with
`ESHU_CODE_CALL_PROJECTION_PARTITION_COUNT=4` and
`ESHU_CODE_CALL_PROJECTION_WORKERS=2` so full-corpus CALLS materialization
proves file-scoped partition concurrency; tune those values in a private env
file for larger hosts.
Performance Evidence: the #2624 baseline remote proof rendered file-scoped
`code_calls` work but leased the domain with `partition_count=1`, while the
queue held 3,454 distinct code-call partition keys and 18,857 pending
file-scoped intents. After this configuration change,
`docker compose --env-file .env.remote-e2e.example -f docker-compose.remote-e2e.yaml config`
renders the code-call sidecar at 4 partitions and 2 workers for the runtime
services. The terminal #2599 remote proof must confirm `code_calls`
`partition_count > 1`, zero dead letters, and drained or bounded queue state on
the pinned NornicDB backend.
No-Observability-Change: this only wires reducer sidecar environment values.
The existing shared-projection lease rows, queue status, reducer logs, metrics,
and pprof surfaces remain the operator evidence for partition count,
throughput, retries, and dead letters.

For the service list, proof commands, AWS credential requirements, pprof ports,
and acceptance evidence, see
[Remote Collector E2E](../reference/local-testing/remote-collector-e2e.md)
and [Profiling And Concurrency](../reference/local-testing/profiling-and-concurrency.md#remote-e2e-worker-profiles).
The optional `docker-compose.remote-e2e.pprof.yaml` overlay binds host pprof
ports to `127.0.0.1`; pair it with
`docker-compose.remote-e2e.observability.pprof.yaml` when the observability
collector overlay is enabled. Keep profiler access private and use it only for
focused proof runs.

Jira, PagerDuty, Grafana, Prometheus/Mimir, Loki, and Tempo are disabled by
default and render only when their explicit profile is selected with a private
env file and, for the observability collectors, the observability overlay.
The observability overlay runs a one-shot preflight before each Grafana,
Prometheus/Mimir, Loki, or Tempo worker. Selecting one of those profiles without
the matching `ESHU_REMOTE_E2E_*_ENABLED=true` flag or required private target
configuration fails the preflight once instead of restart-looping the disabled
collector binary.
Their disabled
registrations can keep blank private target fields and the claim-capable flag,
so preserved-volume restarts do not need placeholder provider values.
The Jira registration uses `jql_env` for `ESHU_JIRA_JQL`; keep private JQL in
the env file so spaces, quotes, and operators are not interpolated into the
collector instance JSON.
Missing rendered services remain
`skipped` proof rows; rendered services with zero source facts fail the hosted
collector proof.

## Point CLI Commands At Compose

The API is available at `http://localhost:8080` by default. The MCP service is
available at `http://localhost:8081` by default.

For repository indexing from the host CLI, including the environment variables
needed to point `eshu scan` or `eshu index` at Compose stores, see
[Index repositories](../use/index-repositories.md#host-cli-into-compose-stores).

See [Connect MCP locally](mcp-local.md) for MCP client setup.
