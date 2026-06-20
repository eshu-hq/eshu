# Environment Variables

This is the route map for Eshu environment variables. Use it to find the
runtime that reads a value, the default Eshu uses when it is unset, and the
evidence that justifies changing it.

Do not tune from vibes. A queue backlog alone does not prove a worker count
should go up. A timeout alone does not prove the timeout should be longer.
First identify the stage, phase, label, row count, claim age, and latest failure
class from status output, structured logs, traces, or a discovery advisory.

## Start Here

| Need | Read |
| --- | --- |
| API, MCP, local service, auth, Postgres, Bolt, OTEL, pprof, memory, and installer variables | [Runtime And Storage Environment](environment-runtime-storage.md) |
| Repository discovery, parsing, projector, reducer, queue, graph-write, and NornicDB tuning variables | [Ingestion And Queue Environment](environment-ingestion-queues.md) |
| Workflow coordinator, Terraform-state, AWS, Vault live, OCI, package-registry, SBOM-attestation, security-alert, vulnerability-intelligence, Confluence, and webhook variables | [Collector Environment](environment-collectors.md) |
| Docker Compose ports, remote E2E, verifier, live-smoke, and proof-run variables | [Compose And Test Environment](environment-compose-tests.md) |
| NornicDB write-shape tuning decisions and evidence requirements | [NornicDB Tuning](nornicdb-tuning.md) |
| Compose service purpose and when to use each Compose file | [Docker Compose](../run-locally/docker-compose.md) |
| Local validation commands and gates | [Local Testing](local-testing.md) |

## Tuning Rules

| Rule | Why |
| --- | --- |
| Change the narrowest knob that names the failing stage. | Broad knobs hide root cause and make regressions harder to bisect. |
| Prefer input filtering before increasing graph-write budgets. | Larger batches of wrong or noisy input still produce wrong graph truth. |
| Keep claim windows near worker count for slow backends. | Pre-claiming more work than workers can start causes lease expiry and duplicate work. |
| Increase timeouts only after the statement shape is proven correct. | Longer timeouts can hide missing indexes, bad query routing, or row-shape bugs. |
| Record before/after evidence in the affected reference page, package README, or evidence note. | Performance tuning without provenance becomes folklore. |

## Common Operator Knobs

| Variable | Use |
| --- | --- |
| `ESHU_HOME` | Isolate local state, managed binaries, API keys, and local-authoritative workspaces. |
| `ESHU_GRAPH_BACKEND` | Select `nornicdb` or explicit `neo4j`. |
| `ESHU_QUERY_PROFILE` | Select runtime/query profile. Do not use it as a performance knob. |
| `ESHU_FACT_STORE_DSN`, `ESHU_CONTENT_STORE_DSN`, `ESHU_POSTGRES_DSN` | Configure Postgres-backed fact, queue, content, and query stores. |
| `ESHU_NEO4J_URI`, `NEO4J_URI` | Configure the Bolt endpoint for NornicDB or Neo4j. |
| `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` | Declare redacted semantic provider profiles using credential handles only; no provider keys. Search profiles include `source_classes:["search_documents"]`, model id, endpoint profile id, and `embedding_dimensions`. |
| `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` | Allowlist hosted semantic extraction and search embedding by provider profile, source class, source scope, limits, redaction posture, and retention posture. |
| `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER` | Force deterministic no-network local vector builds on the reducer and semantic/hybrid retrieval for API/MCP search with `hash` or `local_hash`; `auto_hash` selects one governed `search_documents` provider profile when configured and otherwise falls back to local hash; unset allows provider-only auto-selection. |
| `ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` | Select one governed `search_documents` provider profile when more than one eligible semantic-search profile is configured. |
| `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON` | Gate hosted active-mode collector scheduling before claimable work is planned. |
| `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` | Gate hosted component-extension scheduling before claimable work is planned. |
| `ESHU_GOVERNANCE_AUDIT_EVENT_COUNT` | Report the aggregate hosted governance audit event count without event bodies. |
| `ESHU_GOVERNANCE_AUDIT_DENIED_DECISION_COUNT` | Report the aggregate denied hosted governance audit decision count. |
| `ESHU_GOVERNANCE_AUDIT_UNAVAILABLE_DECISION_COUNT` | Report the aggregate hosted governance audit decisions blocked by unavailable policy or sinks. |
| `ESHU_PPROF_ADDR` | Enable opt-in pprof on a specific runtime for profiling. |
| `ESHU_DISCOVERY_REPORT` | Write discovery advisory JSON before changing ignored paths or size caps. |
| `ESHU_DISCOVERY_IGNORED_PATH_GLOBS` | Apply an operator-controlled generated/vendor/archive ignore overlay. |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | Set the canonical graph-write timeout after statement shape is proven correct. |
| `ESHU_REDUCER_WORKERS` | Tune reducer worker count after queue and graph-write evidence. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | Keep reducer claim batch size near worker count on slower graph backends. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Declare claim-capable collector instances for coordinator-owned workflows. |

## Secrets

Treat these variables as credentials or secret material:

- `ESHU_API_KEY`
- `ESHU_API_KEY_<PROFILE>`
- `ESHU_GIT_TOKEN`
- `GITHUB_TOKEN`
- `ESHU_GITHUB_APP_PRIVATE_KEY`
- `GITHUB_APP_PRIVATE_KEY`
- `ESHU_NEO4J_PASSWORD`
- `NEO4J_PASSWORD`
- `ESHU_POSTGRES_PASSWORD`
- `ESHU_TFSTATE_REDACTION_KEY`
- `ESHU_AWS_REDACTION_KEY`
- `ESHU_VAULT_LIVE_REDACTION_KEY`
- `ESHU_CONFLUENCE_API_TOKEN`
- `ESHU_CONFLUENCE_BEARER_TOKEN`
- webhook secrets and live-smoke registry passwords or bearer tokens

Prefer Kubernetes Secrets, local credential stores, or short-lived maintainer
shell exports. Do not commit secret values.

## Deprecated Or Unsupported

These names may appear in older docs, scripts, or historical configs. They are
not supported tuning surfaces for the current Go runtime:

- `ESHU_REPO_FILE_PARSE_MULTIPROCESS`
- `ESHU_MULTIPROCESS_START_METHOD`
- `ESHU_WORKER_MAX_TASKS`
- `ESHU_INDEX_QUEUE_DEPTH`
- `ESHU_WATCH_DEBOUNCE_SECONDS`
- `ESHU_COMMIT_WORKERS`
- `ESHU_MAX_CALLS_PER_FILE`

If one of these looks necessary, stop and identify which current Go stage owns
the behavior before adding a replacement knob.
