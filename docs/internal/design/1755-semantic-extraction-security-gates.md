# Semantic Extraction Security Gates

Status: **PROPOSED - SECURITY REVIEW REQUIRED BEFORE IMPLEMENTATION.**

Refs #1755. Refs #1750, #1754, #1756, #1757, #1758, #1849.

## Decision

Hosted semantic extraction must pass a deterministic security preflight before
any prompt is constructed or any content leaves the Eshu runtime. The preflight
is separate from provider-profile configuration and source-policy allowlists:

1. `internal/semanticprofile` declares redacted provider handles.
2. `internal/semanticpolicy` decides whether a source class, scope, selector,
   limits, redaction posture, and retention posture are allowed.
3. The security gate classifies and redacts the concrete source chunk, rejects
   unsafe prompt or response material, and records audit-safe evidence.

The default is no provider traffic. Security review must approve this design and
any follow-up contract package before hosted, local, Docker Compose,
assistant-mediated, or gateway-backed extraction sends content to a model.

## Source And Threat Context

Local Eshu contracts already require fail-closed handling:

- `internal/redact` rejects blank keys, emits deterministic HMAC markers, drops
  unsafe composite values, and tells callers to count redactions by reason or
  source class rather than raw values.
- `internal/semanticpolicy` denies missing policy, unsupported source classes,
  missing provider profile, stale ACLs, and unallowlisted sources before queue
  or prompt work.
- semantic fact validation rejects raw provider keys, prompt payloads, bearer
  tokens, secret values, and private provider responses.

External guidance aligns with that posture. OWASP LLM01:2025 treats prompt
injection as direct or indirect content that can alter model behavior, disclose
sensitive data, call unauthorized functions, or manipulate decisions, and it
recommends input/output filtering, least privilege, human approval for risky
actions, and clear separation of untrusted content. OWASP LLM02:2025 calls out
personal data, business data, legal documents, credentials, proprietary code,
and model internals as sensitive, and recommends sanitization, input validation,
access controls, restricted data sources, tokenization, and redaction. The OWASP
Prompt Injection Prevention Cheat Sheet also recommends structured prompts,
remote-content sanitization, output validation, monitoring, and model-based
guardrails as defense-in-depth rather than replacements for deterministic gates.

References:

- [OWASP LLM01:2025 Prompt Injection](https://genai.owasp.org/llmrisk/llm01-prompt-injection/)
- [OWASP LLM02:2025 Sensitive Information Disclosure](https://genai.owasp.org/llmrisk/llm022025-sensitive-information-disclosure/)
- [OWASP LLM Prompt Injection Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html)

## Required Gate Order

The preflight must run in this order and stop at the first terminal denial:

1. **Provider and policy gate.** Require a configured provider profile, healthy
   provider status, explicit semantic policy allowlist, source scope match,
   source selector match, and available token/cost budget.
2. **ACL and actor gate.** Require fresh `allowed` ACL state for the service
   principal or actor class that will cause content egress. Missing, partial,
   stale, or denied ACL state is terminal.
3. **Extractor gate.** Accept only bounded text produced by an approved extractor
   version. Reject raw binaries, unsupported archives, macro-enabled Office,
   active HTML/SVG, hidden text, remote includes, skipped archive members, and
   OCR output whose extraction policy is incomplete.
4. **Data classification gate.** Classify source and chunk content before prompt
   construction. The classifier returns low-cardinality data classes and actions:
   `allow`, `redact`, `fingerprint`, `drop`, `deny`, or `needs_review`.
5. **Redaction gate.** Apply deterministic redaction or fingerprinting for every
   class allowed by policy. Re-scan the redacted chunk and deny if secret-like,
   credential-like, prompt-control, or disallowed personal/customer data remains.
6. **Prompt-safety gate.** Reject chunks with direct or indirect prompt injection
   indicators, hidden instructions, obfuscated commands, exfiltration markup,
   prompt/system-prompt requests, tool-use instructions, or cross-modal
   instruction artifacts. Model-based guardrails may add signal but cannot
   override a deterministic denial.
7. **Retention gate.** Require metadata-only retention. Prompt bodies default to
   `none`; prompt hashes are optional; raw provider responses and provider error
   bodies default to `none`; response hashes are optional.
8. **Response gate.** Parse provider output into the expected schema, classify
   the output, reject unsafe or sensitive material, and store only hashes,
   redaction summaries, safety summaries, and bounded excerpts explicitly
   allowed by security review.

## Data Classes And Default Actions

The security gate must classify at least these data classes:

| Data class | Examples | Default action |
| --- | --- | --- |
| `credential` | API keys, access tokens, private keys, OAuth/JWT material, cloud credentials | deny if raw; fingerprint/redact only for approved metadata |
| `secret_reference` | Secret names, Vault paths, Kubernetes Secret names, env-var handles | fingerprint unless an approved status surface already exposes a safe handle class |
| `private_url` | private hostnames, signed URLs, local paths, private Git remotes | fingerprint |
| `personal_data` | emails, names, phone numbers, addresses, personal IDs | deny unless tenant policy and ACL explicitly allow redacted processing |
| `customer_data` | customer names, tenant names, account ids, support cases | deny unless tenant policy and ACL explicitly allow redacted processing |
| `proprietary_code` | source snippets, private APIs, algorithms, repository paths | deny for hosted providers until security review approves a provider/data boundary |
| `incident_ticket_chat` | incidents, tickets, chat exports, support notes | deny by default |
| `raw_logs_traces` | logs, traces, request bodies, dashboard JSON, pprof, crash dumps | deny by default |
| `prompt_control` | system prompts, prompt templates, guardrail policies, tool instructions | deny |
| `active_or_hidden_content` | HTML/SVG script, hidden text, remote includes, macros, invisible Unicode | deny |
| `binary_or_archive` | raw PDF, Office, image, archive members before deterministic extraction | deny until extractor policy emits bounded text and safety metadata |

Unknown classes must deny. Explicit source policy may narrow the set further,
but it must not weaken these defaults without security signoff.

## Prompt Injection Handling

Prompt injection checks must cover direct, indirect, persistent, obfuscated, and
multimodal patterns:

- direct override language such as requests to ignore, reveal, replace, or
  elevate instructions;
- indirect instructions embedded in documentation, code comments, tickets,
  PDFs, images, HTML, Markdown, SVG, OCR text, or archive members;
- hidden or low-visibility content, including invisible Unicode, CSS-hidden
  nodes, white-on-white text, comment blocks, metadata fields, or annotation
  layers;
- encoding and obfuscation such as base64, hex, ROT13, reversed text, homoglyphs,
  typoglycemia, mixed-language attacks, and suspicious delimiter tricks;
- exfiltration instructions using links, images, Markdown, HTML, tool calls, or
  provider-native request fields;
- instructions to reveal system prompts, policy text, credentials, provider
  request ids, redaction keys, or hidden context;
- attempts to force tool use, file mutation, network requests, shell commands,
  queue manipulation, or privilege escalation.

Pattern checks alone are not enough. A future guard package may use a
purpose-built local or hosted classifier for higher-risk chunks, but the guard
classifier is itself untrusted and cannot permit content that deterministic
policy denied.

## Decision States

The gate should return one decision per source chunk:

| State | Meaning |
| --- | --- |
| `allowed` | All gates passed; prompt construction may proceed with redacted content only. |
| `denied_by_policy` | Provider profile, source policy, source class, scope, selector, or budget denied work. |
| `denied_by_acl` | ACL state is missing, partial, stale, or denied. |
| `denied_unclassified_source` | Source or chunk could not be classified safely. |
| `denied_sensitive_data` | Sensitive data remains or policy denies the data class. |
| `denied_prompt_injection_risk` | Prompt-control or injection indicators exceeded the approved threshold. |
| `denied_unsupported_format` | Extractor/chunker cannot produce bounded safe text. |
| `denied_oversized_chunk` | Chunk exceeded byte, token, section, link, or wall-time budgets. |
| `denied_retention_policy` | Requested retention exceeds metadata-only defaults. |
| `redacted_empty` | Redaction removed all useful content; no prompt is constructed. |
| `response_rejected` | Provider output failed schema, safety, or sensitive-output checks. |

Every denied state must carry a low-cardinality reason, policy id when present,
provider profile id, source class, actor class, ACL summary, classifier version,
redaction policy version, and retention posture. Raw paths, prompts, responses,
provider request ids, user names, customer names, credentials, and source record
ids must not appear in metric labels or status summaries.

## Redaction Summary

Use structured fields rather than a free-form string:

- `classifier_version`
- `redaction_policy_version`
- `redaction_mode`
- `redaction_state`
- `data_classes_seen`
- `redacted_counts_by_class`
- `fingerprinted_counts_by_class`
- `dropped_counts_by_reason`
- `prompt_injection_indicators`
- `unsafe_reason`
- `source_hash`
- `chunk_hash`
- `truncated`
- `retention_posture`

Markers and hashes are evidence handles, not metric dimensions.

## Retention And Audit

Default retention posture:

- raw prompt body: `none`;
- redacted prompt hash: optional;
- raw provider response body: `none`;
- provider response hash: optional;
- provider error body: never; store sanitized error class only;
- provider request id: hash or fingerprint only;
- prompt template: version and hash only;
- source content: source hash, chunk hash, extractor version, classifier
  version, redaction summary, and bounded excerpt only after security approval.

Audit rows or future facts may record:

- `preflight_id`, `job_id`, `policy_id`, `rule_id`, `provider_profile_id`;
- source class, public scope kind, safe source handle class, actor class;
- ACL summary, classifier version, redaction policy version, prompt version;
- source hash, chunk hash, prompt hash, response hash;
- decision state, reason, data classes seen, redaction summary, retention
  posture, budget decision, and timestamps.

Audit records must be queryable enough for operators to explain why semantic
evidence exists or does not exist without exposing source content.

## Evidence Representation

Do not add a new persisted fact kind until schema review approves it. Before
schema approval, denied and rejected work should be visible through job lifecycle
state, audit rows, status summaries, and low-cardinality metrics. If schema
review approves a fact later, prefer a narrow `semantic.extraction_warning` fact
that carries decision state, reason, versions, hashes, and safe handles only.

Provider errors should be represented as sanitized classes such as
`timeout`, `rate_limited`, `provider_unhealthy`, `schema_invalid`,
`unsafe_response`, or `retention_blocked`. Raw provider error messages are not
evidence-safe by default.

## Telemetry

Future implementation must add bounded signals before provider traffic:

- counter for preflight decisions by state, reason, source class, provider
  profile class, and retention posture;
- counter for redaction actions by data class and action;
- counter for prompt-injection indicators by indicator class;
- histogram for preflight duration;
- histogram or counter for chunk byte and token budgets;
- counter for provider response rejection class;
- structured audit log with safe ids and hashes, not raw content.

Metric labels must stay low cardinality. Source ids, document ids, page titles,
paths, tenant/customer/user names, prompt hashes, response hashes, provider
request ids, and credential handles belong in ACL-aware audit records or traces
only when approved.

No-Observability-Change: this document changes design guidance only. It adds no
runtime, queue, provider call, credential load, metric, span, log, status field,
fact kind, or schema.

## Review Questions

Security review must answer these before implementation:

1. Which provider profiles satisfy no-training and no-retention requirements?
2. Can any raw prompt or raw provider response be retained in operator-local
   storage, or is raw retention permanently denied?
3. Are hosted providers allowed to process proprietary code snippets, or must
   code hints use local/assistant-mediated modes only?
4. Are tickets, chats, incident notes, or customer data permanently denied, or
   tenant opt-in with redaction and ACL proof?
5. Who owns the stable data-class taxonomy and classifier versioning?
6. Does prompt-injection detection stay deterministic-only at first, or may a
   separate guard model add defense-in-depth signal?
7. Should rejected payloads remain job/audit/status evidence, or should schema
   review create `semantic.extraction_warning`?
8. What evidence is required before Docker Compose, local, assistant-mediated,
   or hosted provider modes can send real content?

## Implementation Sequencing

1. Land this design as the #1755 research artifact.
2. Use #1849 for the follow-up pure `internal/semanticguard` contract package
   after security review agrees on state names and data classes.
3. Implement #1849 with synthetic fixtures only; no provider calls.
4. Connect #1756 queue/fingerprint work to the guard decision before enqueueing
   provider jobs.
5. Add #1757 telemetry and audit storage before enabling hosted traffic.
6. Require security and schema review before observation facts, provider workers,
   OpenAPI routes, MCP tools, or Compose/local provider modes ship.
