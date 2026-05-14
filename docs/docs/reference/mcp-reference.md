# MCP Reference & Natural Language Queries

This page lists all available **MCP Tools** that your AI assistant (Cursor, Claude, VS Code) can use.

When you ask a question in natural language, the AI selects one of these tools behind the scenes.

For the bounded-call, envelope, pagination, and diagnostics-only contract for
each tool, see the [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md).

For documentation-oriented answers, use a simple orchestration rule:

- start with story or context when the user wants explanation, onboarding, support guidance, or deployment narrative
- use content reads and content search after the story identifies the exact artifacts worth citing
- keep all file-shaped answers portable through `repo_id + relative_path` or `entity_id`
- when the answer spans multiple repos, tell Eshu to scan all related repositories, deployment sources, and indexed documentation first

Repository-bearing results may include `repo_access` metadata. If Eshu is running remotely, treat repository identity as remote-first and use `repo_id`, `repo_slug`, and repo-relative paths before assuming any `local_path` exists on the user's machine.

Canonical-first query behavior now has one explicit rule:

- prefer canonical IDs at the tool boundary whenever the tool supports them
- treat name-based repository lookup as a supported compatibility alias, not the canonical contract
- use inspection-style tools when the caller wants evidence widening and coverage reporting, not a second truth model
- inspection results may report `complete`, `partial`, or `unknown` coverage based on indexed evidence; they should not imply certainty the graph does not have

Content-oriented tools use the same rule:

- file lookup uses `repo_id + relative_path`
- entity lookup uses `entity_id`
- deployed MCP/API runtimes prefer the PostgreSQL content store and report `unavailable` when a row is not yet indexed
- local helper flows may use workspace or graph-cache fallbacks when the content store is not the answering backend
- file and entity read responses include `source_backend` so the client can see whether Eshu answered from `postgres`, `workspace`, `graph-cache`, or `unavailable`
- content search tools require the PostgreSQL content store and return an error when it is disabled
- `repo_access` prompting is only for workflows that truly need the user's local machine

Prompt-suite and docs examples should stay portable too:

- use `repo_id + relative_path` for file-shaped answers
- prefer structured query tools before `execute_cypher_query`
- treat raw Cypher as a manual diagnostic tool, not a fallback path for prompt coverage
- avoid teaching prompt tests to depend on server-local filesystem paths

!!! tip "File Exclusion"
    You can control what gets indexed using `.eshuignore`.
    [**📄 Read the Guide**](eshuignore.md)

## Core Analysis Tools

These are the most commonly used tools for understanding code.

| Tool Name | Description | Natural Language Example |
| :--- | :--- | :--- |
| **`find_code`** | Search for code by name or fuzzy text. | "Where is the `User` class defined?" |
| **`find_symbol`** | Find exact or fuzzy symbol definitions with `source_handle`, `truncated`, and `ambiguity` metadata. | "Where is `process_payment` implemented?" |
| **`investigate_code_topic`** | Investigate broad behavior or implementation topics with ranked files, symbols, coverage, truncation, and exact next-call handles. | "Find the code paths responsible for repo sync authentication." |
| **`get_code_relationship_story`** | Resolve one symbol or entity id, return ambiguity candidates, then fetch bounded callers, callees, imports, or transitive CALLS edges with truncation metadata. | "Who calls `process_payment`, and show me if the name is ambiguous." |
| **`analyze_code_relationships`** | The swiss-army knife for call graphs and dependencies. | "Find all callers of `process_payment`." |
| **`calculate_cyclomatic_complexity`** | Measure function complexity. | "What is the complexity of `main`?" |
| **`find_most_complex_functions`** | List the hardest-to-maintain functions. | "Show me the 5 most complex functions." |
| **`find_dead_code`** | Identify derived dead-code candidates, optionally scoped by `repo_id` as a canonical ID, repository name, repo slug, or indexed path, with optional `limit`, default entrypoint, direct Go Cobra/stdlib-HTTP/controller-runtime signature, Go public-API, test, and generated-code exclusions, optional decorator-owned entry-point exclusions, and a `truncated` signal when the bounded result window is cut off. | "Find dead code in this repo, but ignore `@route`." |

## Story & Context

Use these tools when the user is asking for a narrative answer such as
"Internet to cloud to code" or "tell me everything about this service."

| Tool Name | Description | Natural Language Example |
| :--- | :--- | :--- |
| **`get_repo_story`** | Return a structured repository story with `subject`, `story`, `story_sections`, optional `semantic_overview`, evidence-oriented overviews, limitations, coverage, and drill-down handles. Accepts a repository selector: canonical ID, name, repo slug, or indexed path. | "Tell me the end-to-end story for payments-api." |
| **`get_workload_story`** | Return a narrative workload story using canonical workload identity, optionally scoped to one environment. Use `trace_deployment_chain` when you need the richer deployment-mapping fields such as `controller_overview` or `deployment_fact_summary`. | "Show me how payments-api is deployed in prod." |
| **`get_service_story`** | Return the one-call service dossier for service-shaped prompts, including identity, API surface, deployment lanes, upstream dependencies, downstream consumers, evidence graph, bounded counts, and drill-down handles. | "What can you tell me about payments-api in QA?" |
| **`investigate_service`** | Return the service investigation packet for coverage-first prompts, including repositories considered, repositories with evidence, evidence families found, coverage summary, findings, and recommended next calls. | "Scan the related repos and docs for payments-api before you explain it." |
| **`resolve_entity`** | Resolve fuzzy input into canonical entities before story or context calls, with an optional repository selector filter. | "What canonical entity matches `payments prod rds`?" |
| **`get_entity_context`** | Fetch full context for one canonical entity id. | "Show me the context for this resolved entity." |
| **`get_repo_context`** | Durable drill-down for repository details after the story answer. Accepts a repository selector: canonical ID, name, repo slug, or indexed path. | "Show me the full repo context behind that story." |
| **`get_relationship_evidence`** | Durable drill-down for a relationship evidence pointer returned as `resolved_id` in deployment evidence artifacts or evidence indexes. | "Why does this dependency/deployment edge exist?" |
| **`get_workload_context`** | Durable drill-down for workload details after the story answer. | "Show me the workload context behind that story." |
| **`get_service_context`** | Service alias drill-down for service-shaped prompts. | "Show me the service context behind that story." |

`trace_deployment_chain` now exposes the deployment-mapping fields that callers should use for deployment-specific answers:

- `controller_overview`
- `runtime_overview`
- `deployment_sources`
- `cloud_resources`
- `k8s_resources`
- `image_refs`
- `k8s_relationships`
- `deployment_facts`
- `controller_driven_paths`
- `delivery_paths`
- `deployment_fact_summary`

Repository and service deployment summaries may also expose grouped delivery-family fields inside
`deployment_overview`:

- `delivery_family_paths`
- `delivery_family_story`
- `delivery_workflows`
- `shared_config_paths`

When the deployment repos contain ArgoCD controller entities, `controller_overview`
also includes those concrete controller records under `entities`.

`deployment_facts` are normalized, evidence-backed facts such as:

- `MANAGED_BY_CONTROLLER`
- `PROVISIONED_BY_IAC`
- `USES_PACKAGING_LAYER`
- `DEPLOYS_FROM`
- `DISCOVERS_CONFIG_IN`
- `RUNS_ON_PLATFORM`
- `OBSERVED_IN_ENVIRONMENT`
- `EXPOSES_ENTRYPOINT`
- `DELIVERY_PATH_PRESENT`

Within `trace_deployment_chain`, `deployment_fact_summary` is the compact interpretation layer:

- `mapping_mode=controller` means explicit controller evidence was found
- `mapping_mode=iac` means explicit infrastructure-as-code evidence was found
- `mapping_mode=evidence_only` means only delivery/runtime evidence was found, and Eshu intentionally avoided guessing a controller family
- `mapping_mode=none` means the indexed context is too sparse to map deployment evidence truthfully yet
- `overall_confidence_reason` explains the reason code behind the top-level confidence
- `fact_thresholds` maps each emitted fact type to a stable threshold code
- `limitations` uses stable deployment-mapping limitation codes

Current threshold-code examples:

- `explicit_iac_adapter`
- `explicit_controller_signal`
- `explicit_packaging_signal`
- `explicit_automation_signal`
- `named_deployment_source`
- `named_config_source`
- `explicit_platform_match`
- `explicit_environment_evidence`
- `named_entrypoint`
- `delivery_path_present`

This keeps the contract portable across ArgoCD, Flux, Terraform, CloudFormation, plain Kubernetes manifests, ECS, Lambda, and environments that do not use a controller at all.

## Content Retrieval & Search

Tools for portable source retrieval and indexed content search.

Use these after story, context, or `investigate_code_topic` determines which
files, snippets, or docs matter most.

| Tool Name | Description | Natural Language Example |
| :--- | :--- | :--- |
| **`get_file_content`** | Read a file using `repo_id + relative_path`, where `repo_id` may be a canonical ID, repository name, repo slug, or indexed path. | "Show me `src/payments.py` from the payments repo." |
| **`get_file_lines`** | Read a specific line range from one repo-relative file using the same repository selector contract as `get_file_content`. | "Show me lines 20 to 40 from `src/server.py`." |
| **`get_entity_content`** | Read source for one content-bearing entity using its canonical `entity_id`. | "Show me the source for this resolved function." |
| **`search_file_content`** | Search indexed file text through the content store, optionally scoped by repository selectors. | "Find every file that mentions `shared-payments-prod`." |
| **`search_entity_content`** | Search cached entity snippets through the content store, optionally scoped by repository selectors. | "Find entities whose source mentions `process_payment`." |

## Runtime & Repository Status

Tools for runtime health, completeness, and repository inventory.

| Tool Name | Description | Natural Language Example |
| :--- | :--- | :--- |
| **`get_index_status`** | Show the latest checkpointed completeness state. | "Is indexing complete right now?" |
| **`list_ingesters`** | Show the latest persisted status for all configured ingesters. | "What ingesters are configured and what state are they in?" |
| **`get_ingester_status`** | Show detailed status for one ingester runtime, including retry timing and repo progress counts. | "What is the repository ingester doing right now?" |
| **`list_indexed_repositories`** | Show a bounded page of currently indexed projects with `limit`, `offset`, and `truncated`. | "What repos are indexed?" |
| **`get_repository_stats`** | Show counts of files, classes, and other repository stats, optionally scoped by repository selector. | "Show stats for the backend repo." |

## Bundles & Registry

| Tool Name | Description | Natural Language Example |
| :--- | :--- | :--- |
| **`search_registry_bundles`** | Search the bundle catalog view exposed by the query surface. | "Search for a `flask` bundle." |

## Advanced Querying

For complex questions that standard tools can't answer.

| Tool Name | Description | Natural Language Example |
| :--- | :--- | :--- |
| **`execute_cypher_query`** | Run a raw read-only database query. | "Find all recursive functions." |
| **`visualize_graph_query`** | Generate a Neo4j Browser link for a query. | "Visualize the class hierarchy of `BaseModel`." |

---

## Example Queries (Cookbook)

For a deep dive into exactly how to phrase questions and what JSON arguments look like, check out the Cookbook.

[📖 View the MCP Cookbook](mcp-cookbook.md)
