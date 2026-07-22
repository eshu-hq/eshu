# Docker Compose

Use Docker Compose when you want the full local product stack: API, MCP,
ingestion, reduction, Postgres, and a graph backend. Use
[Local binaries](local-binaries.md) when you are editing source code and want
fast rebuilds without containers.

## Choose A Compose File

| Compose file or profile | What it starts | Use it when |
| --- | --- | --- |
| `docker-compose.yaml` | Default stack: NornicDB, Postgres, migration, workspace setup, bootstrap indexing, API, MCP, ingester, reducer, and an always-on projector. | You want the normal local product stack. |
| `docker-compose.neo4j.yml` | Neo4j compatibility stack. Includes the workflow-coordinator profile but not the default webhook-listener profile. | You need Neo4j compatibility checks. |
| `docker-compose.telemetry.yml` | Jaeger, OpenTelemetry collector, and OTLP export settings. | You need local traces or metrics export. |
| `--profile workflow-coordinator` | Adds the workflow coordinator to the default or Neo4j stack. | You are testing collector claims, scheduling, or control-plane behavior. |
| `--profile webhook-listener` | Adds the webhook listener to the default NornicDB stack. | You are testing webhook-driven freshness or external event ingestion locally. |
| `docker-compose.tier2-tfstate.yaml` | Layers MinIO, MinIO setup, active workflow coordination, and one Terraform state collector onto the default stack. | You are running the Tier-2 Terraform state drift proof. |
| `docker-compose.tier2-tfstate-v25.yaml` | Layers MinIO, two generation-specific MinIO setup jobs, active workflow coordination, and two Terraform state collectors. | You are running the v2.5 Terraform state drift proof across two fixture generations. |
| `docker-compose.remote-e2e.yaml` | Standalone remote proof stack with runtime services, preflight, workflow coordination, webhook listener, and cloud/package/registry collectors. | You are on an EC2 or VPN-attached host and need a full remote collector proof. |
| `docker-compose.demo.yaml` | Standalone, credential-free demo stack: corpus staging, one-shot cassette collectors, a deferred-relationship maintenance orchestrator, API, and MCP. Answers the five `specs/demo-first-answers.v1.yaml` questions. | You want a working correlated-graph demo with zero credential env, or you are proving the first-five-minutes onboarding path. |
| `docker-compose.e2e.yaml` | Standalone, minimal fresh-stack for the browser SSO auth E2E suite: NornicDB, Postgres, migration, workspace setup, the API, and a synthetic mock OIDC IdP. No ingester, reducer, projector, or collectors, and zero seeded local identities. | You are proving the SSO login flow against a fresh, zero-corpus stack (issue #4971). |

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
| `projector` | Always-on source-local projector, gated only on schema + workspace setup (NOT bootstrap-index). Drains stranded `retrying` projector work items that bootstrap-index leaves behind on a canonical-write failure, so a failed bootstrap cannot permanently wedge convergence (#4727). Matches the Helm ungated-ingester topology. |

On a fresh Compose database, `db-migrate` sets
`ESHU_DEFER_CONTENT_SEARCH_INDEXES=true`. It creates the content tables without
the two exact substring-search trigram GIN indexes, and `bootstrap-index`
restores those indexes after source-local content projection drains. API and
MCP all-repository substring searches return `503` while the durable lifecycle
is `not_built`, `building`, or `failed`; repository-scoped content reads remain
available. Existing indexes are never dropped, so preserved-volume restarts
and upgrades keep their steady-state read path. A failed or interrupted final
build is retried idempotently by the next `bootstrap-index` run.

## Bootstrap Admin Credential

`ESHU_AUTH_BOOTSTRAP_MODE` defaults to `generated` (#4963): on first boot, with
no local identities yet, the `eshu` service seals a freshly generated
one-time admin username/password/recovery-code bundle with
`ESHU_AUTH_SECRET_ENC_KEY`, a base64-encoded 32-byte data-encryption key (see
[Environment Variable Reference](../reference/env-registry.md)). Without a
configured DEK, `generated` mode fails closed and the API never starts.

The default and Neo4j Compose files ship a fixed, publicly-known, all-zero
`ESHU_AUTH_SECRET_ENC_KEY` placeholder so the stack boots without extra setup
and survives a `docker compose down`/`up` cycle (a randomly-generated key
would strand the sealed credential after every restart). This value is
**never** appropriate outside local development — set your own
`ESHU_AUTH_SECRET_ENC_KEY` (or `ESHU_AUTH_SECRET_ENC_KEY_FILE`) before
deploying anywhere the stack's data matters. Retrieve the generated
credential with `eshu admin initial-credential`, or set
`ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD` to seed a specific admin instead
(no DEK required: the operator-supplied password is never sealed, only
hashed, and the one-time MFA recovery code prints to the startup banner and
is never retrievable again), or set
`ESHU_AUTH_BOOTSTRAP_MODE=sso-only`/`disabled` to skip local admin seeding
entirely.

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

### Temporary exact NornicDB #261 default

Until further notice, the default Compose NornicDB service builds orneryd/
NornicDB#261 from full source commit
`1492458852588c884c32f70d27ea2ee07086769c`. Compose tags the local image
`eshu-nornicdb-pr261:149245885258`, records the full revision as an OCI image
label, and uses the default pull policy `build`. This makes a clean machine
build the proven same-UID commit-lock fix instead of trying to pull the local
tag from a registry.

Controlled backend comparisons retain the existing override contract. Set
`NORNICDB_IMAGE` and `NORNICDB_PULL_POLICY` together: use `always` for an
immutable published image, `never` for a prebuilt local tag, or `build` to build
the exact source below under a different local tag. Leaving both unset uses the
exact PR #261 source pin. Published-image and prebuilt-local comparisons must
run `docker compose up` without `--build`; `--build` deliberately rebuilds the
exact source and would defeat the image override.

Leave `NORNICDB_PLATFORM` unset for normal local runs so the build uses the host
architecture.

Normal `docker compose up --build` builds the pinned backend and Eshu services.
To cache the backend first, build the stack once and then start without
rebuilding either image:

```bash
docker compose build

docker compose up -d --no-build
```

Confirm the cached image carries the expected source revision before treating
the stack as evidence:

```bash
docker image inspect eshu-nornicdb-pr261:149245885258 \
  --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}'
```

The expected output is
`1492458852588c884c32f70d27ea2ee07086769c`. Replace this temporary source pin
only after a released NornicDB image containing #261 is pinned by digest and the
bounded/full-corpus proof is repeated on that artifact.

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
It keeps `ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS=4` for the retained
full-corpus proof and the proven NornicDB Compose default. The Neo4j
compatibility Compose file remains at `1` until that backend has equivalent
headroom proof. The dedicated lane accepts only `1`, `2`, or `4`, and
unsupported values fall back to the backend default. Fixed
source-repository acceptance-unit sharding keeps one repository's complete
retract-then-rewrite cycle serialized and ordered while unrelated repositories
can project concurrently. This is a repo-dependency-specific knob and does not
inherit `ESHU_REDUCER_WORKERS`. Repo-dependency cycles use a `45s` whole-cycle
deadline and a `5m` shard lease. The lease must exceed that deadline plus
`ESHU_CANONICAL_WRITE_TIMEOUT` and a `30s` margin. The remote profile's `120s`
canonical-write timeout therefore remains inside the default safety budget.
Any error, cancellation, or ambiguous commit quarantines only the affected
shard until lease expiry; other shards keep processing independent repositories.
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

## Demo Stack

Use `docker-compose.demo.yaml` for a working, credential-free correlated-graph
demo. It boots the same corpus family the B-7 golden-corpus gate proves
(`scripts/verify-golden-corpus-gate.sh`) — the manifest-declared subset of
fixtures and cassette families in `specs/demo-first-answers.v1.yaml` — so the
five demo-first-answers questions answer correctly over HTTP with zero
credential environment variables.

```bash
docker compose -p eshu-demo -f docker-compose.demo.yaml up --build -d --wait
```

The stack is standalone and defaults the Compose project to `eshu-demo`. The
root file is the operator entrypoint; it includes a corpus fragment
(`docker-compose.demo.corpus.yaml`) and a runtime fragment
(`docker-compose.demo.runtime.yaml`) that own the service groups.

| Service | Responsibility |
| --- | --- |
| `demo-corpus-staging` | One-shot: copies (not symlinks) each manifest-declared repo fixture into the shared source corpus dir `/data/corpus` (`ESHU_FILESYSTEM_ROOT`). bootstrap-index syncs from there into `/data/repos` (`ESHU_REPOS_DIR`); the two must differ because the non-direct filesystem mode cleans the repos dir before copying. The filesystem discovery walker does not follow symlinks, so a symlinked fixture collapses to a single scope and breaks cross-repo edges. |
| `collector-kubernetes-live`, `collector-oci-registry`, `collector-gcp-cloud`, `collector-package-registry`, `collector-pagerduty`, `collector-tempo`, `collector-prometheus-mimir`, `collector-grafana`, `collector-loki` | `-mode=cassette` replays of `testdata/cassettes/<family>/supply-chain-demo.json`. Each replays its recorded fixture, commits its facts, then keeps its status server up (cassette collectors run as a hosted service, not a one-shot). The orchestrator gates on `service_healthy` and then verifies every collector's facts landed in `ingestion_scopes` before running bootstrap, so a collector that is up but has not yet committed cannot advance the pipeline on a partial corpus. |
| `demo-corpus-orchestrator` | One-shot: `bootstrap-index` -> drain -> deferred-relationship maintenance pass x2 (`bootstrap-index` rerun + drain each), mirroring the golden-corpus gate's approximation of the ingester's continuous `RunDeferredRelationshipMaintenance` loop. Required for the cross-repo `DEPENDS_ON` (rc-3) and `RUNS_IMAGE` (rc-4) correlations Q2 and Q4 depend on to converge; a single bootstrap+reducer pass is not sufficient. |
| `eshu` | HTTP API, gated on `demo-corpus-orchestrator` completion. |
| `mcp-server` | MCP HTTP runtime, gated on `demo-corpus-orchestrator` completion. |

The demo corpus is a trimmed, manifest-declared subset of the golden-corpus
gate's proven 20-repo/17-cassette set: the union of repos and cassette
families each of the five questions' `artifacts` declares in
`specs/demo-first-answers.v1.yaml` (6 repos, 9 cassette families). Every
collector not needed by one of the five questions (for example
`collector-vault-live`, which the product Dockerfile does not build) is
omitted.

Host ports default off both the base stack (`8080`/`15432`/`7687`/`7474`) and
the golden-corpus gate's ports (`18080`/`18091`) to avoid a collision when both
run concurrently:

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_DEMO_PROJECT_NAME` | `eshu-demo` | Compose project name. |
| `ESHU_DEMO_BIND_ADDR` | `127.0.0.1` | Host address the API and MCP ports publish on. Loopback by default because the demo serves reads open (no auth header); set it to a specific address or `0.0.0.0` only for intentional network exposure. |
| `ESHU_DEMO_API_PORT` | `18080` | HTTP API port. |
| `ESHU_DEMO_MCP_PORT` | `18091` | MCP HTTP port. |
| `ESHU_DEMO_NORNICDB_HTTP_PORT` | `17474` | NornicDB HTTP port. |
| `ESHU_DEMO_NORNICDB_BOLT_PORT` | `17687` | NornicDB Bolt port. |
| `ESHU_DEMO_POSTGRES_PORT` | `18432` | Postgres port. |
| `ESHU_DEMO_DRAIN_TIMEOUT_SECONDS` | `600` | Per-drain-pass timeout the orchestrator waits before failing. |
| `ESHU_DEMO_DRAIN_POLL_SECONDS` | `3` | Orchestrator drain-residual poll interval. |
| `ESHU_DEMO_DRAIN_SETTLE_SECONDS` | `60` | Settle window before each drain's first poll, so a poll never reads a false 0/0 "drained" before the reducer's first emit; also absorbs contention from other heavy stacks running concurrently on the same host. |

No credential environment variable is required anywhere in the demo path:
`ESHU_GIT_AUTH_METHOD=none`, `ESHU_REPO_SOURCE_MODE=filesystem`, and every
collector replays a recorded cassette instead of calling a live provider. The
demo runtime also force-disables every external-provider knob the base stack
passes through: `DEEPSEEK_API_KEY`, `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`,
`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`, `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID`,
and the Ask Eshu settings are all overridden to empty or `false`, so the demo
never calls an external LLM even if an operator has those exported in their
shell. `scripts/verify-demo-compose-answers.sh` asserts this two ways: a
grep-level check (no `:?`-required env var, no `*_TOKEN`/external-provider
`*_API_KEY`/cloud-credential reference in the demo compose files or scripts)
and a runtime check that reads the actual env of the running `eshu` and
`mcp-server` containers and fails if any provider credential is present or Ask
Eshu is enabled. It also boots the stack, calls all five questions over HTTP,
and confirms `docker compose down -v --remove-orphans` leaves zero containers,
volumes, or networks for the run's project.

The demo API and MCP serve reads **without an auth header** by default. Unlike
the base stack, the demo runtime sets `ESHU_AUTO_GENERATE_API_KEY=false` and
leaves `ESHU_API_KEY` empty, so `ResolveAPIKey` returns an empty token and the
read auth middleware runs open. This is deliberate: a first-run demo should
answer the five questions with a plain `curl`, no key handling. Because the
read surface is open, the API and MCP ports publish on `127.0.0.1` by default
(via `ESHU_DEMO_BIND_ADDR`) so the unauthenticated surface is not reachable
from the network; set `ESHU_DEMO_BIND_ADDR` to a specific address or `0.0.0.0`
only for intentional exposure. To require a key instead, set `ESHU_DEMO_API_KEY`
before `up`; the API and MCP then reject unauthenticated reads and callers must
send `Authorization: Bearer <key>`.

For the proof gate and its no-regression / no-observability-change evidence,
see
[Demo Compose Stack Proof](../reference/local-testing.md#demo-compose-stack-proof).

## SSO Auth E2E Stack

Use `docker-compose.e2e.yaml` for the minimal fresh-stack the browser SSO auth
E2E suite (issue #4971, epic #4962 closer) needs: proving Eshu's OIDC login
flow against a real OIDC counterparty with zero corpus and zero seeded local
identities.

```bash
docker compose -f docker-compose.e2e.yaml up -d --build --wait
```

The stack is standalone and defaults the Compose project to `eshu-e2e`.
Reused services (`nornicdb`, `postgres`, `db-migrate`, `workspace-setup`,
`eshu`) extend their `docker-compose.yaml` definitions; only the ports,
volumes, and the Postgres/graph DSNs are overridden for stack isolation.

| Service | Responsibility |
| --- | --- |
| `nornicdb` | Graph database. |
| `postgres` | Facts, queues, status, content, and recovery state. |
| `db-migrate` | One-shot Postgres and graph schema bootstrap. |
| `workspace-setup` | One-shot `/data/.eshu` and `/data/repos` setup. |
| `eshu` | HTTP API runtime. |
| `mock-oidc-idp` | Synthetic OIDC Authorization Code identity provider (`go/cmd/mock-oidc-idp`): discovery, authorization, token, and JWKS endpoints backed by a static, non-secret RSA key and one configured synthetic `example.test` identity. |
| `mock-oidc-idp-admin` | A second `mock-oidc-idp` instance with an admin-mapped group, backing the env/file OIDC provider whose SSO sign-in records the `require_sso` guardrail's admin-proof precondition (a DB-backed group mapping can never mint an AllScopes session — see `apps/console/e2e/authE2EOidcFlow.ts`). |

There is no `ingester`, `resolution-engine`/reducer, `projector`, or any
collector in this stack: the suite this stack supports proves the login flow,
not corpus content, so it needs zero repo facts.

`ESHU_ADMIN_USERNAME`/`ESHU_ADMIN_PASSWORD` are never set, matching the base
stack. With the inherited default `ESHU_AUTH_BOOTSTRAP_MODE=generated`, the
API boots with `GET /api/v0/auth/setup-state` reporting `needs_setup: true`
until an operator or test claims the sealed one-time bootstrap credential (see
[Bootstrap Admin Credential](#bootstrap-admin-credential) above).

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_E2E_PROJECT_NAME` | `eshu-e2e` | Compose project name. |
| `ESHU_E2E_BIND_ADDR` | `127.0.0.1` | Host address the API and mock IdP ports publish on. |
| `ESHU_E2E_API_PORT` | `28080` | HTTP API port. |
| `ESHU_E2E_MOCK_OIDC_PORT` | `28090` | Mock OIDC IdP port. |
| `ESHU_E2E_MOCK_OIDC_ADMIN_PORT` | `28091` | Admin-mapped mock OIDC IdP (`mock-oidc-idp-admin`) port. |
| `ESHU_E2E_NORNICDB_HTTP_PORT` | `27474` | NornicDB HTTP port. |
| `ESHU_E2E_NORNICDB_BOLT_PORT` | `27687` | NornicDB Bolt port. |
| `ESHU_E2E_POSTGRES_PORT` | `28432` | Postgres port. |
| `ESHU_E2E_POSTGRES_PASSWORD` | `change-me` | Postgres password for this stack's isolated `postgres` container. |
| `ESHU_E2E_MOCK_OIDC_ISSUER_URL` | `http://mock-oidc-idp:8080` | The mock IdP's own issuer URL, as Eshu's server-side OIDC connector (on the same Compose network) reaches it. Reaching `/authorize` from a host-side or separately-networked browser needs its own resolvable path to this hostname; that wiring belongs to the browser-auth runner phase, not this foundation. |
| `ESHU_E2E_MOCK_OIDC_SUBJECT` | `member-user-1` | Synthetic identity's `sub` claim. |
| `ESHU_E2E_MOCK_OIDC_EMAIL` | `member.user@example.test` | Synthetic identity's `email` claim. |
| `ESHU_E2E_MOCK_OIDC_GROUPS` | `member` | Comma-separated group claim values for the synthetic identity. |

A browser-auth runner drives this stack end to end for all six #4971
acceptance items: `npm run console:e2e:auth`
(`apps/console/e2e/runAuthE2E.ts`, wrapped by `scripts/run-auth-e2e.sh`,
documented in `apps/console/README.md#browser-auth-e2e-gate`). On a fresh,
zero-identity stack it proves the first-run setup wizard and
bootstrap-credential consumption; configures a DB-backed OIDC provider config
pointing Eshu at the mock IdP through the real Admin UI (add → test → enable);
completes a full browser OIDC redirect → mock IdP → callback login as a
non-admin member and asserts `/admin` 403 gating; enables `require_sso` with
local break-glass; and runs a negative-secret-leakage scan
(`apps/console/e2e/authE2ELeakage.ts`). The `require_sso` guardrail-rejection
assertion passes (its earlier failure caught a real, since-fixed gap — missing
`GET`/`PATCH /api/v0/auth/admin/sign-in-policy` entries in
`go/internal/query/auth_scoped_routes.go`'s browser-session allowlist, fixed
in #5004/#5006). The gate also runs in CI as the `auth-sso-e2e` job in
`.github/workflows/frontend.yml`. Its wrapper builds the exact-source `eshu`
CLI once before launching the browser runner, then gives the direct credential
read its own 15-second timeout; cold Go compilation is not charged to the
Postgres operation. The mock IdP is reachable from both the API container
(Compose network) and the host browser (via Chromium
`--host-resolver-rules`); see `go/cmd/mock-oidc-idp/README.md` for its endpoint
contract.

## MCP-Identity Auth E2E Stack

The same `docker-compose.e2e.yaml` additionally defines an `mcp-server` and a
`mock-github` service that a SIBLING suite (F-9, issue #5170) uses to prove the
auth-identity story on the **MCP HTTP transport** (`GET /sse`,
`POST /mcp/message`, `/.well-known/oauth-protected-resource`). These services
are additive: the #4971 SSO suite above never starts them, so both suites can
run concurrently on one machine.

```bash
bash scripts/run-auth-mcp-e2e.sh
```

The runner (`apps/console/e2e/runAuthMcpE2E.ts`, wrapped by
`scripts/run-auth-mcp-e2e.sh`) owns the full stack lifecycle (fresh
`up --build --wait`, then `down -v`) on an **isolated Compose project
(`eshu-e2e-auth-mcp`) and 29xxx port block**, disjoint from the SSO suite's
`eshu-e2e-auth` / 28xxx block. It drives one stack through three sequential
org-shape phases plus a negative-leakage module:

| Phase | What it proves |
| --- | --- |
| Shape A (token-only) | Zero-provider login page and 404 discovery; a personal API token minted through the console UI; an authenticated MCP `tools/call`; the async allowed-read governance-audit event; the bare (non-OAuth) 401 challenge. |
| Shape C (GitHub, stubbed) | Admin-drawer GitHub provider CRUD against `mock-github`; login DENIED before a team-role mapping, then allowed after; discovery still 404 (GitHub is never a bearer issuer); MCP still works via the personal token; `eshu mcp setup` resolves token posture. |
| Shape B (OIDC via mock IdP) | Provider CRUD; the live discovery flip to 200; a scripted RFC 9728 + PKCE OAuth chain minting a JWT bearer; the F-2 challenge-precedence regression (valid token → 200 no challenge; expired/wrong-aud → bare `Bearer`; unknown-issuer → `resource_metadata`); the `require_sso` flip keeping tokens and break-glass working. |
| Negative-leakage | Credential-less probes → 401 with no tool/server/protocol leakage; distinct bad-credential denials (via the oidcbearer resolver's structured-log `outcome` and the F-2 challenge shape); a non-vacuous cross-scope repository row filter; a raw-token-absence scan across logs, audit rows, and the DOM. |

Two additional verifiers back the suite:

- `scripts/verify-auth-mcp-e2e-manifest.sh` compares the runner's report
  (`e2e-artifacts/auth-mcp-e2e-report.json`) against the named baseline
  `testdata/golden/auth-mcp-e2e-baseline.json` (exact ordered step-id list,
  per-step status, runtime bound). Recapture with
  `scripts/refresh-auth-mcp-e2e-baseline.sh`.
- `scripts/verify-auth-mcp-e2e-sensitivity.sh` proves the negative module is
  live by recreating the `mcp-server` with its credential-source gate disabled
  (`docker-compose.e2e.mutation.yaml`) and asserting the credential-less module
  flips from pass to fail.

Both run in CI as the `auth-mcp-e2e` job in `.github/workflows/frontend.yml`
(registered as the `auth-mcp-e2e` gate in `specs/ci-gates.v1.yaml`), and are
locally runnable with the single command above.

> Denial-reason distinctness is asserted from the oidcbearer resolver's
> structured-log `outcome` values and the RFC 9728 challenge shape, because
> denial-side governance-audit reason codes do not yet exist. Adding a
> first-class denial-reason to the governance-audit table is tracked in
> [eshu-hq/eshu#5567](https://github.com/eshu-hq/eshu/issues/5567).

| Variable | Default | Purpose |
| --- | --- | --- |
| `ESHU_E2E_MCP_PORT` | `29081` | MCP HTTP transport port (`mcp-server`). |
| `ESHU_E2E_MOCK_GITHUB_PORT` | `29092` | Stubbed GitHub OAuth2/REST counterparty (`mock-github`) port. |
| `ESHU_E2E_OIDC_STATIC_CONFIG_PATH` | `./apps/console/e2e/fixtures/oidc-static-config.json` | Path to the env/file OIDC provider fixture; the MCP suite points it at a port-5195 variant so its own dev server's callback resolves. |

See `go/cmd/mock-github/README.md` for the stubbed GitHub endpoint contract and
`apps/console/e2e/README-auth-mcp-e2e.md` for the suite's module map.

## Point CLI Commands At Compose

The API is available at `http://localhost:8080` by default. The MCP service is
available at `http://localhost:8081` by default.

For repository indexing from the host CLI, including the environment variables
needed to point `eshu scan` or `eshu index` at Compose stores, see
[Index repositories](../use/index-repositories.md#host-cli-into-compose-stores).

See [Connect MCP locally](mcp-local.md) for MCP client setup.
