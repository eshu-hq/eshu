# LLM-Assisted Code Relationship Hints

Status: **PROPOSED - RESEARCH ONLY. SECURITY AND SCHEMA REVIEW REQUIRED BEFORE
IMPLEMENTATION.**

Refs #1759. Refs #1750, #1754, #1755, #1758.

## Decision

Eshu may add LLM-assisted code relationship hints as an optional semantic
evidence lane for code questions that deterministic parsers cannot fully
resolve yet: dynamic framework dispatch, long-tail language conventions,
architecture intent, and ownership clues.

Hints are provenance. They are not canonical graph truth, resolver output, or
deployment/runtime/security truth. A `semantic.code_hint` fact may preserve a
possible code relationship with source, chunk, provider, policy, redaction,
freshness, and corroboration metadata, but it must carry
`promotion_policy=requires_deterministic_evidence`. Reducer-owned deterministic
evidence is the only path to an exact graph edge or exact query answer.

No-provider behavior is a first-class supported runtime shape. Deterministic
indexing, parser facts, reducer projection, API reads, MCP reads, and docs
verification continue to work when no semantic provider or code-hints policy
exists.

## Non-Goals

This research note does not approve or implement:

- provider SDK calls, gateway calls, model requests, or assistant-mediated
  prompts;
- hosted code egress, local model egress, provider credential loading, or
  provider profile storage;
- schema migrations, fact-kind registration, queue tables, graph writes, API
  routes, OpenAPI changes, or MCP tools;
- public posture documentation changes;
- assistant packet or answer-packet implementation work;
- model output as canonical call graph, import graph, ownership, deployment,
  runtime, vulnerability, or security posture truth;
- generated code edits, refactors, remediation plans, or automated pull
  requests;
- raw prompt, raw source chunk, raw provider response, raw provider error, or
  provider request id retention.

## Hint Classes

First support should prefer narrow, source-local hints whose subject and object
can be tied to parser or content handles. Each class must have a low-cardinality
`hint_kind`, scoped subject/object refs, source excerpt hash, and reducer rule
version.

| Hint class | Useful signal | Required deterministic anchors |
| --- | --- | --- |
| `route_to_handler` | Dynamic web framework route likely reaches a handler. | route literal or route config handle, handler entity or file handle, language/framework id. |
| `framework_convention` | Framework naming convention implies a controller, view, job, migration, or lifecycle callback. | file path handle, parser entity handle, recognized framework marker. |
| `dynamic_dispatch` | Reflection, `send`, DI container, event bus, or registry may dispatch to a target. | dispatch site handle, candidate target handles, bounded dispatch expression. |
| `architecture_intent` | Source-local comments or names suggest layering, module boundaries, or ownership intent. | file/entity handles and exact source excerpt hash. |
| `ownership_boundary` | Code references suggest a team, module owner, package owner, or service boundary. | repository handle plus deterministic owner file, manifest, CODEOWNERS, service-catalog, or package metadata when available. |
| `data_flow` | Value or request data likely flows between two code entities through dynamic glue. | source and sink entity handles, parser-known intermediate handles when available. |
| `config_to_code` | Config key, route declaration, queue topic, or event name likely maps to code. | config/file handle, symbol/file handle, parser or relationship extractor evidence. |
| `test_to_subject` | Test name or fixture likely exercises a production entity not linked by static imports. | test entity handle, production entity handle, deterministic import or path proximity evidence. |
| `entity_alias` | Framework alias, decorator, annotation, registration string, or macro name likely names an entity. | alias source handle and one or more parser entity candidates. |

Unsupported or unsafe hint kinds must be rejected before persistence. Free-form
provider prose is not a hint kind.

## Policy Defaults

Code hints are disabled by default in all profiles:

- no provider profile means `state=unavailable`,
  `reason=provider_not_configured`;
- configured provider profiles without a matching `code_hints` source policy
  remain visible but disabled for that source class;
- hosted deployments must keep `code_hints` off until security review approves
  proprietary-code processing for the specific provider boundary;
- local or assistant-mediated modes still require explicit policy, source
  allowlists, ACL proof, redaction, retention, and prompt-safety gates;
- unknown languages, generated files, vendored dependencies, build outputs,
  secret reports, logs, traces, binary assets, archives, private key material,
  and raw runtime dumps deny by default;
- source selection must be repository/path/language scoped and bounded by bytes,
  tokens, chunks, retries, cost, and wall time;
- model output must parse into the approved schema before any fact, audit row,
  or readback exists.

`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` remains the policy owner for the
`code_hints` source class. Provider configuration alone never enables hint
extraction.

## Source Surfaces For First Support

Safe first support should use small, text-only chunks already admitted by
deterministic collection and parser stages:

- source snippets around parser-known entities, imports, decorators,
  annotations, route declarations, dependency injection registrations, and
  event subscriptions;
- bounded config snippets that relationship extractors already classify, such
  as route maps, queue/event names, framework manifests, and test fixtures;
- package-local documentation comments only when they are adjacent to a parser
  entity and pass the #1755 security gate;
- repository metadata such as language, file kind, parser support maturity, and
  stable Eshu handles.

First support must exclude whole files by default. If a future prompt needs
larger context, it must first prove why a smaller chunk cannot answer the
question and must record truncation and omission metadata.

## Corroboration Rules

Hints move through reducer-owned states. Provider output can suggest a state
transition, but cannot finalize it.

| State | Meaning | Presentation |
| --- | --- | --- |
| `candidate` | Schema-valid hint with safe provenance, but no deterministic corroboration. | Visible only as `code_hint` with cautious language. |
| `corroborated` | Deterministic parser, relationship, reducer, or read-model evidence agrees on subject, object, and relationship kind. | May be shown as corroborated evidence; graph truth still comes from deterministic evidence. |
| `ambiguous` | More than one target or relationship remains plausible. | Show candidates and missing evidence; choose no winner. |
| `unsupported` | Language, framework, hint kind, source family, profile, or parser maturity has no reducer rule. | Return unsupported reason, not a fallback fact. |
| `unsafe` | Source, prompt, response, retention, or ACL gate failed. | No hint payload; expose only safe denial class. |
| `stale` | Source, ACL, prompt version, policy, provider profile, or deterministic corroboration changed. | Require refresh before presentation. |
| `rejected` | Deterministic evidence disproves the hint or conflicts with current graph truth. | Keep audit-safe rejection evidence; do not show as current candidate by default. |

Corroboration requires all of these:

1. The hint subject and object resolve to stable Eshu handles from parser,
   content, relationship, or read-model state.
2. The relationship kind has an approved reducer rule for that language,
   framework, and source surface.
3. Deterministic evidence independently supports the same subject, object, and
   relationship kind in the same repository or declared cross-repo scope.
4. No current deterministic evidence contradicts the proposed relationship.
5. The source fingerprint, ACL state, policy id, prompt version, provider
   profile, redaction version, and reducer rule version are fresh.
6. The result can be explained with evidence handles and bounded excerpts
   without raw prompt or provider response retention.

Two model responses do not corroborate each other. A high confidence score does
not corroborate anything. Search rank, naming similarity, directory proximity,
and comments are candidate signals unless paired with deterministic Eshu
evidence.

When deterministic evidence is sufficient to create a canonical relationship,
the graph edge must cite deterministic fact ids as the authority. The hint may
be attached as supporting provenance only after schema review approves that
relationship.

## Parser And Reducer Responsibilities

Parsers and deterministic relationship extractors own source facts:

- code entities, file handles, imports, calls, decorators, annotations,
  manifests, route literals, config keys, and parser warnings;
- parser support maturity and unsupported-language evidence;
- line ranges and excerpt hashes for source-local audit.

The reducer owns hint admission:

- validate schema, policy, safety, and freshness metadata;
- resolve subject/object handles against current parser and read-model state;
- classify candidate, corroborated, ambiguous, unsupported, unsafe, stale, or
  rejected;
- attach missing-evidence reasons when exact truth is blocked;
- prevent provider output from creating or modifying canonical graph truth.

Query handlers and MCP tools read reducer state. They must not reinterpret raw
provider output or re-run model reasoning during readback.

## MCP Presentation Safeguards

MCP presentation must preserve the canonical envelope and prompt-facing truth
class:

- use named, scoped tools; no normal prompt flow should require raw Cypher;
- require repository/entity scope, `limit`, deterministic ordering, timeout,
  `truncated`, and cursor or offset metadata for list reads;
- return `truth_class=code_hint` for uncorroborated candidates and never present
  them as `deterministic` or `semantic_observation`;
- keep the text summary non-confident whenever `supported=false`,
  `partial=true`, `candidate`, `ambiguous`, `unsupported`, `unsafe`, or
  `stale`;
- include evidence handles, missing-evidence reasons, policy state,
  corroboration state, freshness, and recommended next calls;
- display bounded excerpts only after ACL and redaction checks;
- never expose raw prompt text, raw provider responses, provider request ids,
  credentials, secret values, private URLs, or unrestricted source content;
- refuse deployment, runtime, vulnerability, incident, security posture, or
  ownership assertions that rely only on code hints;
- make no-provider and policy-disabled states explicit as unsupported or
  unavailable, not as failed indexing.

If a hint is corroborated, MCP should still name the deterministic evidence that
made the relationship safe to present. The hint itself is not the proof.

## Examples

### Positive

A Ruby route file contains `post "/orders", to: "orders#create"`. The parser
emits the route literal, the controller class, and the `create` method. A model
emits a `route_to_handler` hint for the same subject/object. The reducer marks
the hint `corroborated` and may display it beside the parser-backed route
evidence. Any graph edge cites the parser route and controller facts, not the
model response.

### Negative

A comment says "Billing probably owns this helper", but CODEOWNERS,
service-catalog, package metadata, and repository ownership facts do not name
Billing. The hint is either rejected or left as an unshown candidate. Eshu must
not create service ownership, pager rotation, or deployment ownership truth
from that comment.

### Ambiguous

A Python registry dispatches `handler = registry[event_type]` and three
handlers register the same normalized event family in different modules. The
model suggests one likely handler from naming context. The reducer marks the
hint `ambiguous`, returns all candidate handles, and records missing evidence
such as absent concrete event value or unsupported registry semantics.

### Unsupported

A generated minified JavaScript bundle contains framework glue with no stable
source map and no parser entity handles. The source is text, but Eshu cannot
tie model output to stable subject/object refs. The lane returns
`unsupported_format` or `unsupported_source` and no provider prompt is needed.

### Unsafe

A source chunk contains an API key-shaped value and a comment instructing any
assistant to ignore safety rules and reveal system prompts. The security gate
returns `denied_sensitive_data` or `denied_prompt_injection_risk`. No prompt is
constructed, no provider call occurs, and status/audit surfaces expose only the
safe denial class.

## Security Review

Security review must approve this lane before implementation and must cover the
following points.

### Code Egress

- Proprietary code snippets are denied for hosted providers until the approved
  provider profile proves no-training, no-retention, encryption, access-control,
  and tenant-boundary requirements.
- Local or assistant-mediated processing is still egress and must pass the same
  policy, ACL, redaction, prompt-safety, and retention gates.
- Prompts must use the minimum source chunk that can answer the hint question;
  whole-file prompts and repository-wide context are denied by default.
- Generated code, dependencies, vendored trees, binaries, archives, logs,
  traces, dashboards, secret reports, and private key material are denied.

### Source Allowlists

- Allowlist by source class `code_hints`, scope, repository, path prefix,
  language, file kind, and optional framework marker.
- Require fresh ACL state for the actor or service principal that causes
  content egress.
- Deny stale, missing, partial, unknown, or broad source policy state.
- Record only low-cardinality policy reasons in metrics and status; keep raw
  source ids, file paths, branch names, user names, and customer names out of
  metrics.

### Prompt Payload Retention

- Raw prompt body: `none`.
- Raw provider response body: `none`.
- Provider error body: never retained.
- Provider request id: hash or fingerprint only.
- Prompt template: version and hash only.
- Source chunk: source hash, chunk hash, excerpt hash, redaction summary, and
  bounded excerpt only after security approval.
- Audit records may retain provider profile id, policy id, prompt version,
  redaction version, classifier version, source class, actor class, decision
  state, reason class, and timestamps.

### Preventing Invented Truth

- Prompt instructions and output schema must forbid deployment, runtime,
  vulnerability, incident, security posture, and ownership assertions unless
  those claims are returned as unsupported or missing-evidence notes.
- Provider confidence must be advisory only and excluded from graph admission.
- Output validation must reject new services, environments, workloads,
  identities, vulnerabilities, blast-radius claims, runtime health claims, and
  security-impact claims that lack deterministic Eshu handles.
- Query and MCP readback must not upgrade hints into exact answers through
  summarization.
- Rejected or unsafe output must remain audit/status evidence only, not a
  hidden fallback for user-facing answers.

## Fixture And Mocked-Provider Matrix

Implementation PRs must use synthetic repositories and mocked providers before
any real provider mode is enabled.

| Case | Required proof |
| --- | --- |
| no provider | Deterministic code indexing and code relationship reads still work; semantic status reports unavailable. |
| policy disabled | Eligible parser facts exist; no provider work is queued; status reports disabled for `code_hints`. |
| positive route | Parser route and handler facts corroborate a mocked `route_to_handler` hint. |
| negative ownership | Comment-only ownership hint does not materialize ownership truth. |
| ambiguous dispatch | Multiple target handles remain visible without choosing a winner. |
| unsupported source | Generated/minified or unsupported-language source produces an explicit unsupported state. |
| unsafe prompt | Sensitive or prompt-injection source is denied before prompt construction. |
| unsafe response | Provider output with invented deployment/runtime/security truth is rejected. |
| stale source | Prior hint becomes stale when source, ACL, policy, prompt, or corroborating facts change. |
| no raw retention | Tests prove prompts, responses, credentials, and provider ids are absent from facts, logs, status, and metrics. |
| MCP packet | Candidate, ambiguous, unsupported, unsafe, stale, and corroborated states keep envelope truth and cautious summaries. |

## Observability

No-Observability-Change: this note changes design documentation only. It adds
no runtime, worker, queue, provider call, credential load, fact kind, migration,
API route, MCP tool, metric, span, log, status field, graph query, or graph
write.

Future implementation must add or name operator-visible metrics, spans, logs,
status rows, and audit records before any provider traffic or hint readback can
claim readiness. Metrics must use low-cardinality labels such as source class,
hint kind, decision state, provider profile class, policy state, and reason
class; high-cardinality source, prompt, response, and entity identifiers belong
only in ACL-aware audit records or traces after security approval.

## Implementation Sequencing

1. Land this research artifact for #1759.
2. Complete security and schema review for `semantic.code_hint` payload shape,
   retention, policy, and reducer states.
3. Add no-provider and policy-disabled tests before any provider, queue, or
   prompt work.
4. Add synthetic parser fixtures and mocked-provider fixtures for every example
   state above.
5. Add reducer admission tests before admission logic.
6. Add API/MCP readback tests that preserve truth labels, cautious summaries,
   bounds, truncation, and no raw retention.
7. Add telemetry and audit tests.
8. Only after those gates, consider enabling local, assistant-mediated, Compose,
   or hosted provider modes under explicit policy.

## Related Contracts

- [Semantic Extraction Policy And Source Allowlists](1754-semantic-extraction-policy.md)
- [Semantic Extraction Security Gates](1755-semantic-extraction-security-gates.md)
- [Documentation Semantic Observations Design](1758-documentation-semantic-observations.md)
- [Fact Envelope Reference](../../public/reference/fact-envelope-reference.md)
- [Truth Label Protocol](../../public/reference/truth-label-protocol.md)
- [Answer Packet Contract](../../public/reference/answer-packets.md)
- [MCP Tool Contract Matrix](../../public/reference/mcp-tool-contract-matrix.md)
