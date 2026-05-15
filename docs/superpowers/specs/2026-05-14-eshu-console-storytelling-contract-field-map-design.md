# Eshu Console Storytelling Contract Field Map

This checkpoint records the design decision that Eshu Console must design from
MCP/API contract meaning, not from raw field availability.

Evidence used for this pass:

- `get_service_story` for `api-node-boats` on the remote full-corpus stack
- `investigate_service` for `api-node-boats` on the remote full-corpus stack
- `get_repo_story` for `api-node-boats` on the remote full-corpus stack
- `investigate_code_topic` for `api-node-boats` route/deployment evidence
- `docs/docs/reference/http-api.md`
- `docs/docs/reference/mcp-tool-contract-matrix.md`
- `docs/docs/reference/relationship-mapping.md`
- `go/internal/query/service_story_overview.go`
- `go/internal/query/service_story_dossier.go`
- `go/internal/query/service_investigation.go`
- `go/internal/query/repository_story.go`
- `go/internal/query/impact_change_surface_investigation.go`
- `go/internal/query/impact_change_surface_response.go`
- `go/internal/collector/awscloud/services/cloudfront/README.md`

## Design Rule

Every story field must be assigned a product job before it appears in the UI.

- Story fields answer the user in plain language.
- Overview fields orient the user.
- Graph fields show structure and relationships.
- Evidence fields prove a selected claim.
- Coverage and limit fields explain trust, gaps, and truncation.
- Drilldown fields are handles, not primary content.

The console should follow the visualization sequence: overview first, zoom and
filter, then details on demand. For Eshu, overview means story plus first graph
layer. Details mean source files, resolved relationship IDs, exact evidence,
and code lines.

## Rebase Contract Update

The latest MCP contract work changes the console plan in four important ways.

1. Prompt-ready tools now have an explicit bounded-call matrix. The UI should
   treat `limit`, `offset`, `truncated`, and envelope truth as first-class
   controls, not hidden response metadata.
2. Entity-specific drilldowns should resolve before they expand. Use
   `resolve_entity` as the visual disambiguation step for graph nodes, symbols,
   workloads, and service-like names when the selected item is not already a
   canonical ID.
3. Inventory pages should prefer paged contracts such as
   `list_indexed_repositories` instead of whole-index assumptions. Catalog
   needs repository, service, workload, and evidence facets with visible paging
   state.
4. CloudFront runtime evidence introduces an edge layer between public
   entrypoints and runtime targets. The service atlas should show
   entrypoint -> CDN/edge -> origin or load balancer -> workload -> deployment
   source when the evidence exists. CloudFront is safe control-plane metadata;
   it must not be displayed as workload ownership unless a later reducer
   explicitly correlates it.
5. Deployment configuration influence is now a prompt-ready story contract.
   The UI should not bury image tags, runtime settings, resource limits, values
   layers, rendered targets, and read-first files inside generic evidence rows.
   These fields explain what a human should inspect or edit first when a
   deployment looks wrong.

Change-surface remains part of the plan, but the console must use it
conservatively. A remote proof showed exact service-name impact probes can
become too broad when target resolution misses or falls into a full-graph path.
Until the backend path is proven bounded on the full corpus, the UI should
favor service-story, service-investigation, entity-resolution, and code-topic
packets first, then use change-surface only from scoped selections such as a
canonical entity ID, a selected file set, or a narrowed topic.

## `get_service_story`

Primary user question: what is this service, where does it run, who depends on
it, what does it depend on, and what evidence proves it?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `truth` envelope | Freshness, profile, basis, and capability for the whole answer. | Can I trust this answer right now? | Persistent trust strip. | Show reason in a detail rail. |
| `service_identity` | Canonical workload and owning repository. | What am I looking at? | Page title and canonical identity. | Repository and service links. |
| `story` | Concise backend narrative. | What is the answer in English? | Top story summary, split if it grows too long. | Sentence anchors to sections. |
| `story_sections` | Ordered story index. | Which parts of the service story exist? | Story spine or section tabs. | Filter graph and rail by section. |
| `deployment_overview` | Counts and grouped deployment/runtime summary. | How large is the deployment surface? | Summary strip and section header stats. | Counts open exact lists. |
| `deployment_lanes` | Human grouping by deployment mechanism, such as `ecs_terraform` or `k8s_gitops`. | Is this single or multi-deployed? | Main lane map: service to lane to environment/source. | Lane, environment, source repo, or relationship type. |
| `deployment_evidence` | Raw artifacts and proof rows behind lanes. | What files or artifacts prove deployment? | Hidden behind selected lane or edge rail. | File/source evidence and resolved IDs. |
| `evidence_graph` | Durable relationship graph nodes and edges. | What is connected to what? | Interactive graph layer. | Edge to relationship evidence; node to repo/service. |
| `api_surface` | Endpoints, methods, specs, source paths. | What API does this expose? | Endpoint table plus method distribution. | Route to code-topic or source lines. |
| `entrypoints` | Hostnames, docs routes, and config-derived targets. | How do people or systems reach it? | Entrypoint cards grouped by public/internal/environment. | Entrypoint to network path or source file. |
| `hostnames` | Hostname subset of entrypoints. | What public names exist? | Inside the entrypoints view, not as a peer block. | Hostname references and config. |
| `network_paths` | Evidence-backed links from entrypoints to runtime targets. | How does traffic reach runtime? | Network path graph or expandable path rows. | Path to source evidence. |
| `edge_runtime_evidence` | CDN or edge-control-plane signals such as CloudFront distributions, aliases, origins, cache behaviors, viewer certificates, and WAF or ACM links. | Is there an edge layer before the workload? | Edge layer between entrypoints and runtime in the service graph. | Distribution, alias, origin, certificate, or WAF evidence. |
| `downstream_consumers` | Typed graph dependents plus content references. | Who uses or mentions this service? | Two buckets: typed dependents and content mentions. | Repository evidence paths. |
| `dependents` | Graph-derived dependent repositories. | What is typed dependency truth? | Stronger-trust bucket inside consumers. | Relationship evidence. |
| `consumer_repositories` | Content-based references, hostnames, and repo strings. | Who mentions it in code or config? | Weaker mention bucket inside consumers. | Sample paths and matched values. |
| `upstream_dependencies` | Deployment, provisioning, and dependency rows. | What deploys, provisions, or configures it? | Group by verb and source family. | Resolved ID or source repo. |
| `provisioning_source_chains` | Terraform/provisioning chain rows. | Which infrastructure chains shape this service? | Subgraph under deployment/provisioning lane. | Source repo/module drilldown. |
| `documentation_overview` | Docs, specs, and remote metadata. | What docs/specs exist? | Compact docs/spec affordance. | Spec file or docs route. |
| `support_overview` | Aggregate support context counts. | What support context exists? | Secondary summary only. | Relevant sections. |
| `investigation` | Embedded investigation packet. | How complete is this answer and what next? | Coverage/gap rail. | Next MCP calls. |
| `result_limits` | Limits and truncation for curated story fields. | Am I seeing all rows? | Showing-count and truncation badges. | Context route if needed. |
| `raw_context_limits` | Limits for raw embedded context fields. | What raw data was capped? | Developer/audit detail only. | Context route. |

Service story design rule: the service page should be an atlas. `deployment_lanes`
drives the deployment visual, `entrypoints` and edge evidence drive the traffic
visual, `story_sections` drives navigation, `evidence_graph` is the proof layer,
and raw evidence appears only after a selection.

## `investigate_deployment_config`

Primary user question: which repositories and files influence this service's
image tag, runtime settings, resource limits, values layers, or rendered
targets?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `story` | Plain-language summary of configuration influence. | What should I inspect first? | Configuration influence panel intro. | Section anchors. |
| `influencing_repositories` | Service owner, deployment source, and configuration artifact repos. | Which repos shape this deployment? | Repository trail beside the audit sections. | Repo story or source file. |
| `values_layers` | Helm, Kustomize, ArgoCD, Terraform, values, or application layers. | Which values layers can change runtime behavior? | Audit section grouped by repo/path. | `get_file_lines`. |
| `image_tag_sources` | Image tag evidence or fallback image refs. | What image tag is being deployed? | Audit section with alias/value/path visible. | Source lines or relationship evidence. |
| `runtime_setting_sources` | Env vars, replicas, probes, command, args, secrets, config references. | What runtime knobs shape behavior? | Audit section with setting name and source path. | Source lines. |
| `resource_limit_sources` | CPU, memory, requests, and limits. | What resource limits apply? | Audit section with setting name and value. | Source lines. |
| `rendered_targets` | Kubernetes resources and controller targets. | What runtime objects does this render to? | Target section tied back to deployment lanes. | Entity or deployment trace. |
| `read_first_files` | Portable file handles and next calls. | What should I open first? | "Read first" section with `get_file_lines` affordances. | File lines. |
| `recommended_next_calls` | Follow-up MCP calls. | What should I ask Eshu next? | Right rail action list. | Execute or prefill call. |
| `coverage` | Query shape, per-section limit, truncation, artifact/source counts. | Am I seeing the whole influence set? | Coverage/truncation label in the panel header. | Narrow by env or rerun with paging. |

Deployment configuration design rule: this is an audit trail, not a key/value
dump. It belongs between deployment/traffic and lower-level evidence because it
answers the operational question "what file or repo should I inspect first?"

## `investigate_service`

Primary user question: if I ask a broad service question, what did Eshu check,
what did it find, and what should I ask next?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `coverage_summary` | Completeness state, repository counts, limit, and truncation. | Is the investigation complete or partial? | Coverage status block or segmented ring. | State explanation. |
| `evidence_families_found` | Evidence categories present. | What evidence types exist? | Checklist of families. | Family filters findings/repos. |
| `repositories_considered` | Widened repository scope. | What repos did Eshu inspect? | Scope map/list grouped by role. | Repo story/context. |
| `repositories_with_evidence` | Repositories with non-empty evidence families. | Which repos mattered? | Prioritized repo list. | Repo-specific evidence. |
| `investigation_findings` | Family summaries and evidence paths. | What did Eshu find per family? | Story cards by family. | Evidence path drilldown. |
| `recommended_next_calls` | MCP follow-up calls with reasons. | What should I ask next? | Action rail with plain labels. | Execute or prefill call arguments. |
| `intent`, `question` | Caller context. | Why was this investigation run? | Small header context. | None. |

Investigation design rule: this is an investigation workbench, not the normal
service page. It should make partial evidence useful by showing exact gaps and
next calls.

## `resolve_entity`

Primary user question: which exact thing did Eshu think I meant?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `entities` / `matches` | Ranked canonical entity candidates. | Which service, repo, workload, or symbol is this? | Candidate picker before graph expansion. | Rerun the selected story or drilldown by canonical ID. |
| `count`, `limit`, `truncated` | Bounded resolution state. | Did the resolver return everything? | Result-count and truncation badge. | Narrow by repo, type, or exact name. |
| candidate `id`, `name`, `type`, `repo_id` | Canonical handles and context. | Is this the right entity? | Compact rows with type and repository context. | Entity context, service story, repo story, or code story. |

Resolution design rule: ambiguity should be visible before graph claims appear.
If a node click has only a display name, the rail opens as a resolver picker.
If it already has a canonical ID, the rail opens directly to the relevant
story, context, or evidence packet.

## Paged Inventory Contracts

Primary user question: what can I browse, filter, and open without waiting for
the whole graph?

| Contract | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `list_indexed_repositories` | Paged repository inventory with `limit`, `offset`, and `truncated`. | Which repos are indexed? | Catalog repository facet with paging. | Repo story/context. |
| service and workload catalog rows | Graph-backed service and workload handles. | What services and workloads exist? | Catalog tabs or segmented facets. | Service/workload story. |
| `get_ecosystem_overview` | Whole-index summary. | What does the corpus look like? | Dashboard overview graph and corpus health. | Catalog filtered by family. |
| runtime status tools | Ingester and index health. | Is Eshu still building or fresh? | Operational status strip. | Runtime diagnostics. |

Catalog design rule: catalog is a launch surface, not a table dump. It needs
facets for repositories, services, workloads, evidence families, freshness,
language, and deployment family. Every facet must keep paging state visible.

## `get_repo_story`

Primary user question: what is in this repository and what role does it play?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `story` | Backend narrative for the repository. | What does this repo contain or do? | Shortened story with expandable full narrative. | Sections. |
| `story_sections` | Ordered repository story index. | Which repo dimensions are known? | Section spine. | Filter overview. |
| `deployment_overview` | Workloads, platforms, delivery paths, and direct/topology stories. | How does this repo deploy or participate in delivery? | Repository deployment map. | Delivery path/artifact. |
| `relationship_overview` | Typed repository relationships. | What repos does this relate to? | Relationship graph/table. | Relationship evidence. |
| `semantic_overview` | Parser-derived semantic signals. | What code semantics exist? | Small code intelligence block. | Symbol/code tools. |
| `infrastructure_overview` | Infrastructure families and artifacts. | What infrastructure or config does it contain? | Infrastructure family chips and counts. | Artifact details. |
| `documentation_overview` | Repository metadata and doc handles. | Where are docs/specs? | Docs/spec affordance. | File/spec. |
| `gitops_overview` | GitOps signal summary. | Is it GitOps-shaped? | Badge plus target list. | GitOps details. |
| `support_overview` | Dependency/language support counts. | What support context exists? | Secondary summary. | Package/support tools. |
| `coverage_summary`, `limitations` | Coverage state and gaps. | What should I not over-trust? | Trust/gap strip. | Coverage route. |
| `drilldowns` | Context, stats, and coverage paths. | Where do I go next? | Route handles behind buttons. | Fetch route. |

Repository story design rule: repository story is not the same as service
story. It should lead with repo role, codebase, delivery/artifacts, and
relationship role. It should not pretend runtime truth is as strong as the
service dossier.

## `get_code_relationship_story`

Primary user question: for this symbol or function, who calls it, what does it
call, and is the answer ambiguous or truncated?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `target_resolution` | Exact or ambiguous symbol resolution. | Which symbol did Eshu mean? | Resolution banner; candidate picker if ambiguous. | Candidate reruns by entity ID. |
| `scope` | Repository, language, direction, relationship type, and depth. | What query am I looking at? | Query summary chips. | Edit/rerun controls. |
| `relationships` | Direct or transitive code edges. | Who calls, uses, or imports this? | Call graph or caller/callee columns. | Source handles/file lines. |
| `coverage` | Returned, available, and truncated counts by direction. | Is the graph complete? | Direction coverage bars. | Pagination. |
| `source_handle` fields | File/entity handles for exact source. | Where is the code? | Inline source buttons. | `get_file_lines` or entity content. |

Code relationship design rule: this belongs inside code drilldown, not the
initial service story. It becomes visible after a route, function, or code-topic
selection.

## `investigate_change_surface`

Primary user question: what could break, need review, or need follow-up if I
change this service, workload, infrastructure resource, topic, module, or file
set?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `scope` | Normalized request context: target, target type, repo, environment, changed paths, topic, depth, and paging. | What change question did Eshu answer? | Compact query header and editable filter chips. | Rerun with adjusted target, paths, topic, depth, or limit. |
| `target_resolution` | Whether the requested graph target was resolved, ambiguous, missing, or not requested. | Did Eshu understand what I meant? | Pre-graph resolution banner; candidate picker when ambiguous. | Candidate rerun by selected ID/type. |
| `code_surface` | Topic and changed-path evidence from content: changed files, matched files, touched symbols, evidence groups, and source backends. | What code surface is actually touched? | Left rail or first panel in change-review mode, grouped by file and symbol. | File lines, code relationship story, or topic investigation. |
| `code_surface.coverage` | Content query shape and returned symbol/path bounds. | Is the code evidence complete or capped? | Small trust marker beside the code surface. | Pagination or narrowed topic/path query. |
| `direct_impact` | First-hop impacted graph nodes from the resolved target or touched surface. | What is immediately affected? | Inner ring of the impact graph with readable labels. | Node detail rail, service/repo/workload story. |
| `transitive_impact` | Deeper affected graph nodes by depth. | What might be affected downstream? | Expandable outer graph rings grouped by depth. | Depth filter, relationship edge evidence, find-change-surface follow-up. |
| `impact_summary` | Direct, transitive, and total impact counts. | How big is the blast radius? | Section stat strip and graph legend. | Count opens filtered result list. |
| `coverage` | Overall query shape, depth, limits, truncation, and returned counts. | How much should I trust this impact view? | Persistent coverage/gap strip. | Explain query shape and truncation. |
| `recommended_next_calls` | Follow-up MCP calls, often code-topic, code-relationship, or focused change-surface queries. | What should I ask next to prove this? | Action rail with plain-language labels. | Prefill and run the next call. |
| `source_backend` | Whether the answer came from graph, content, or hybrid sources. | What kind of proof backs this? | Small provenance label, not primary content. | Backend-specific evidence detail. |

Change-surface design rule: this is the console's review lens. It should reuse
the service atlas visual grammar, but the center of gravity changes from "what
is this service?" to "what does this change touch?" Code surface anchors the
left side, impact depth rings anchor the graph, and target-resolution state
must appear before any graph so humans know whether the blast radius is based
on a precise target, an ambiguous match, or content-only evidence.

## Trace And Deployment Fact Contracts

Primary user question: how did Eshu decide this deployment path is real?

| Field | Meaning | Human question | UI treatment | Drilldown |
| --- | --- | --- | --- | --- |
| `deployment_fact_summary` | Mapping mode, confidence reason, thresholds, limitations. | Why does Eshu believe this maps to deployment? | Confidence explainer beside the deployment graph. | Fact summary detail. |
| `deployment_facts` | Normalized evidence-backed facts. | Which exact facts were emitted? | Fact timeline or grouped evidence rows after selection. | Relationship evidence or source file. |
| `controller_overview` | Controller-family evidence such as ArgoCD, Flux, Jenkins, or automation controllers. | What automation drives delivery? | Controller lane in deployment graph. | Controller source. |
| `runtime_overview` | Runtime/platform evidence such as EKS, Kubernetes, ECS, Lambda, or CDN edge. | Where does it actually run or route? | Runtime lane in deployment graph. | Runtime evidence. |
| `limitations` | Standard missing-evidence codes. | What does Eshu not know yet? | Gap badges with plain explanations. | Recommended next calls. |

Deployment fact design rule: deployment graphs should be readable as a story:
entrypoint, edge, controller, infrastructure source, runtime, workload, and
source repository. Relationship verbs and confidence belong on selectable
edges, not as tiny labels in the first view.

## Stitch Design Checkpoint

Stitch MCP produced two working mockup directions for this slice:

- Service atlas: `projects/1430014333992672349/screens/44a4f2aa6c3b402f87b3a6d655d6d2be`
- Resolver/change-review state: `projects/1430014333992672349/screens/f0ebd643587a4093b59a0265c7ae5769`
- Deployment-config influence refresh:
  `projects/18138865204006205672/screens/969e1e93c8734155b9360477340108a2`

Use the mockups as interaction direction, not a token source. The repo-owned
`PRODUCT.md` and `DESIGN.md` remain the design system. The useful Stitch moves
are the left story spine, central service atlas graph, right evidence or
resolver rail, explicit trust and truncation state, and change-review as a
mode. Do not import Stitch's Inter or JetBrains Mono choices, fake trust
scores, pill-heavy badges, or clipped graph labels.

## Product Decision

The Eshu Console story model should become:

1. Story spine from `story_sections`.
2. Primary graph from the strongest field for the selected section.
3. Plain-language right rail from the selected node, edge, lane, or gap.
4. Proof handles from `resolved_id`, source paths, and recommended next calls.
5. Coverage/gap strip from `truth`, `coverage_summary`, `result_limits`, and
   `raw_context_limits`.
6. Change-review lens from `investigate_change_surface` when the entry question
   is about a service, topic, resource, module, or changed file set.
7. Resolver-first drilldowns from `resolve_entity` before expanding ambiguous
   names into graph claims.
8. Edge-aware traffic graphs that place CloudFront/CDN evidence between
   hostnames and runtime targets when present.
9. Paged catalog and inventory views from prompt-ready bounded contracts, with
   visible `limit`, `offset`, and `truncated` state.
10. Deployment configuration influence from `investigate_deployment_config`,
    rendered as a repo/file audit trail with read-first handles.

Dashboard and catalog should become entry points into this story model. The
service atlas is where users understand the answer; the change-review lens is
where users understand the blast radius before they merge or deploy.
