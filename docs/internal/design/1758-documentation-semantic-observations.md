# Documentation Semantic Observations Design

Status: **PROPOSED - SECURITY AND SCHEMA REVIEW REQUIRED BEFORE IMPLEMENTATION.**

Refs #1758. Refs #1750.

## 1. Decision

Eshu may add LLM-assisted documentation semantic observations only as an
optional hosted semantic-evidence lane. The lane enriches collected
documentation with provenance-rich observations such as claims, decisions,
summaries, concepts, related services, diagram meanings, runbook steps, and
support-context hints.

Semantic observations are evidence. They are not canonical deployment, code,
service-ownership, infrastructure, vulnerability, or runtime truth. Existing
source-only documentation ingestion continues to work with no provider, no API
key, and no semantic extraction enabled. Git-hosted Markdown, Confluence, and
future documentation collectors may still emit `documentation_source`,
`documentation_document`, `documentation_section`, `documentation_link`,
`documentation_entity_mention`, and `documentation_claim_candidate` facts
without this lane.

Reducer-owned admission is the only path from a semantic observation to a
durable `documentation_finding`. API and MCP readback must expose the
observation basis, provenance, freshness, provider profile, prompt version,
source hash, and policy state so clients can distinguish model observations
from reducer-owned truth.

## 2. Non-Goals

This design does not approve:

- provider SDK calls, gateway calls, or hosted provider configuration;
- raw provider credential storage, CLI flags, logs, or query exposure;
- schema migrations, fact-kind registration, queue DDL, or OpenAPI changes;
- graph projection from model output;
- public API or MCP route implementation;
- prompt text that sends unrestricted source content to a provider;
- generated prose, documentation writing, or updater publication workflows;
- provider output becoming deterministic code-to-cloud truth without reducer
  corroboration.

## 3. No-Provider Behavior

No provider is the default-valid runtime shape. When no provider profile exists,
or when semantic extraction is disabled for the active profile:

- documentation collectors keep emitting source-only documentation facts;
- documentation fact and finding readback keeps returning existing facts and
  findings through current HTTP and MCP routes;
- API and MCP capability status reports semantic observations as unsupported or
  unavailable for that profile, not failed ingestion;
- reducers do not enqueue semantic observation work;
- source-only counts and `missing_evidence` metadata remain explainable without
  pretending observations were attempted;
- tests must prove source-only documentation ingestion and readback still pass
  when no provider exists.

The absence of a provider must never block repository indexing, documentation
source collection, reducer projection, graph queries, documentation fact reads,
or `eshu docs verify`.

## 4. Provider Profile Dependency Boundaries

Provider configuration belongs behind hosted-admin or local profile boundaries.
Documentation collectors and deterministic claim extraction must not accept raw
provider keys.

Provider profiles should carry only stable handles and policy metadata:

- `provider_profile_id` and display-safe profile name;
- provider family and model class, not raw endpoint secrets;
- credential handle or secret-manager reference;
- allowed source classes and maximum payload budgets;
- prompt-pack version and redaction policy version;
- retention policy for prompts, responses, and provider audit metadata;
- hosted, local, Compose, or assistant-mediated runtime profile.

Collectors may discover documentation sources and chunks without provider
access. A semantic worker may depend on an approved profile handle, but a
missing or disabled profile produces an explicit unsupported state rather than
falling back to user-supplied credentials.

## 5. Source Allowlists And Policy

Semantic extraction is opt-in per source family and scope. A source is eligible
only when all of these are true:

- the source family is allowlisted for semantic extraction;
- the source scope, repository, space, page tree, or path matches policy;
- the source ACL was evaluated and permits content egress for the active actor
  or service principal;
- the source format has a bounded extractor and chunker;
- the configured provider profile permits the source family and classification;
- the budget for bytes, chunks, tokens, retries, and wall time is available.

Policy must deny by default for unknown source families, unevaluated ACLs,
private or restricted pages, hidden or annotation content, archive members that
were skipped by extraction policy, generated secrets reports, raw logs, trace
payloads, dashboard JSON, database dumps, credential bundles, and private-key
material.

Allowed source decisions must be recorded as low-cardinality reason classes.
Raw source paths, page titles, user names, customer names, tenant names, and
provider-native record IDs belong in payload provenance only when allowed and
redacted; they must not appear as metric labels.

## 6. Redaction And Content-Egress Gates

The semantic lane must pass content through deterministic egress gates before a
provider sees it:

1. Load only bounded source-native documentation content already admitted by the
   documentation collector.
2. Reject content when ACL state is denied, partial, missing, or stale.
3. Remove or fingerprint secrets, tokens, private URLs, account identifiers,
   email addresses, raw hostnames, local filesystem paths, customer names, and
   provider-native private IDs unless policy explicitly permits them.
4. Reject chunks that still contain credential-shaped values after redaction.
5. Bound prompt input by bytes, token estimate, section count, link count, and
   wall time.
6. Attach redaction summary metadata to the observation job and final
   observation.

Provider prompts and responses are sensitive evidence. Durable storage may keep
prompt version, prompt template hash, input chunk hash, output hash, redaction
summary, and bounded excerpts. Raw prompt bodies or provider responses require
an explicit retention policy, ACL proof, and security signoff.

## 7. Chunk, Fingerprint, And Job Lifecycle

Semantic work should be chunk-oriented and idempotent:

- `source_fingerprint` identifies the source document revision, ACL snapshot,
  content hash, extraction policy version, and redaction policy version.
- `chunk_fingerprint` identifies normalized chunk text after redaction plus
  section ordinal, document revision, prompt-pack version, and provider profile.
- `job_id` is stable for one source fingerprint, chunk fingerprint, provider
  profile, prompt version, and policy version.
- duplicate delivery of the same job converges on the same observation identity.
- source deletion, tombstone, ACL revocation, or document revision change
  marks previous observations stale or retracted before new observations are
  admitted.

Lifecycle states should be explicit:

| State | Meaning |
| --- | --- |
| `pending` | Policy admitted a chunk and queued provider work. |
| `running` | A worker owns the fenced job. |
| `succeeded` | Provider output passed parse, safety, and provenance checks. |
| `skipped_policy` | Source, ACL, budget, or profile policy denied work. |
| `redacted_empty` | Redaction removed all usable content. |
| `unsupported` | Source, format, provider mode, or prompt class is unsupported. |
| `unsafe` | Safety gates rejected the prompt or response. |
| `failed_retryable` | Transport, rate limit, timeout, or provider health failure. |
| `failed_terminal` | Non-retryable provider or parser failure. |
| `stale` | Source fingerprint, ACL, provider profile, or prompt version is no longer current. |
| `retracted` | Source deletion or policy revocation invalidated the observation. |

Retries must preserve idempotency and fencing. They must not silently switch
provider profiles, prompt versions, or redaction policies.

## 8. Observation Payload And Provenance

A future observation payload should be source-specific enough for audit and
generic enough for reducer admission. It should include:

- observation identity: `observation_id`, `observation_kind`, schema version,
  and stable idempotency key;
- source provenance: `scope_id`, `generation_id`, `source_id`, `document_id`,
  `section_id`, optional `line_range`, `source_uri_hash`, and source
  fingerprint;
- chunk provenance: chunk ordinal, chunk fingerprint, content hash, bounded
  excerpt hash, truncation state, and redaction summary;
- provider provenance: provider profile id, provider family, model class,
  prompt pack version, prompt template hash, provider request id hash,
  response hash, and observed provider time;
- policy provenance: source policy id, allowlist decision, redaction policy
  version, retention policy, actor or service-principal class, and ACL summary;
- semantic content: normalized claim, decision, summary, concept, related
  entity hint, diagram meaning, runbook step, support-context hint, or
  unsupported class;
- confidence evidence: provider confidence when supplied, deterministic parser
  confidence, reducer-calculated confidence, and reason codes;
- freshness evidence: source observed time, provider observed time, admitted
  time, stale reason, and retraction reason;
- safety evidence: unsafe content class, redaction status, blocked output
  class, and policy denial reason;
- linked Eshu handles: repository, service, workload, environment, document,
  section, entity, or evidence refs when deterministically resolved.

Observations must keep provider-native identifiers and request ids hashed or
fingerprinted unless security review approves a display-safe form.

## 9. Reducer Admission States

The reducer owns admission from semantic observation evidence to documentation
findings. It must compare observations with deterministic Eshu evidence where
the observation claims operational meaning.

Accepted admission states:

| State | Meaning |
| --- | --- |
| `exact` | Observation is corroborated by deterministic source, graph, or read-model truth for the scoped claim. |
| `partial` | Some referenced handles or claim parts are corroborated, but the full claim is incomplete. |
| `ambiguous` | Multiple possible targets or meanings exist; no canonical truth is chosen. |
| `stale` | Source, ACL, prompt, provider profile, or corroborating Eshu truth is no longer current. |
| `unsafe` | Prompt, source, response, or payload failed safety or egress policy. |
| `unsupported` | Observation kind, source family, provider mode, or claim family has no admitted reducer rule. |

Only `exact` observations may become exact documentation findings. `partial`,
`ambiguous`, `stale`, `unsafe`, and `unsupported` states remain visible as
findings or missing-evidence classes when useful, but they must not promote
deployment, service ownership, runtime reachability, vulnerability impact, or
infrastructure truth.

## 10. Documentation Finding Relationship

Semantic observations can enrich documentation truth workflows in two ways:

- as provenance attached to a `documentation_finding` after reducer admission;
- as related source evidence visible beside raw documentation facts when no
  finding is admissible.

A resulting `documentation_finding` should preserve:

- the admitted state and reducer rule version;
- the originating observation ids and source fact ids;
- the source document and chunk fingerprints;
- the provider profile id, prompt version, redaction summary, and policy state;
- the deterministic Eshu evidence used for corroboration or rejection;
- permissions and freshness fields compatible with evidence packets.

Documentation findings emitted from semantic observations must remain
read-only evidence for updater workflows. Updaters may use evidence packets as
input, but Eshu does not draft, publish, or mutate documentation in this lane.

## 11. API And MCP Readback Shape

Future HTTP and MCP readback should follow the existing documentation truth and
truth-envelope contracts:

- list observations by source, document, section, observation kind, admission
  state, provider profile, freshness state, policy state, and updated time;
- require a bounded scope anchor or exact observation id for payload-heavy
  reads;
- support `limit`, deterministic ordering, `cursor` or `next_cursor`, and
  `truncated`;
- return the canonical envelope with `truth.level`, `truth.profile`,
  `truth.basis`, `truth.freshness`, and machine-readable errors;
- include per-item truth when one page mixes admitted and non-admitted states;
- deny content readback when source ACL is denied, partial, missing, or stale;
- expose hashes, fingerprints, reason codes, and bounded excerpts instead of
  raw provider prompts or unrestricted source content;
- keep aggregate count and inventory routes separate from list routes when
  operators need ecosystem totals.

Readback must make no-provider behavior explicit. A profile without semantic
support returns `unsupported_capability` for semantic observation routes while
existing `list_documentation_facts`, `list_documentation_findings`, evidence
packet, and freshness routes continue to work.

## 12. Audit And Telemetry

Operators need to diagnose why semantic observations did or did not exist
without reading private content. Future implementation must expose:

- job counts by lifecycle state, source family, observation kind, provider
  profile class, and low-cardinality reason;
- policy-denied counts by denial class;
- redaction counts by redaction class and rejected-after-redaction count;
- provider request duration, timeout, retry, rate-limit, and error class;
- token or byte budget consumption by source family and provider profile class;
- reducer admission counts by admission state and observation kind;
- stale and retracted counts by reason;
- audit logs for profile handle, policy id, source scope, actor class, job id,
  prompt version, request hash, response hash, and admission state.

Metric labels must stay bounded. Source ids, document ids, section ids, page
titles, file paths, provider request ids, prompt hashes, and customer-specific
values belong in structured logs, traces, or payloads under ACL-aware access.

No-Observability-Change: this note changes design documentation only. Future
runtime work under this design must add or name operator-visible metrics,
spans, logs, status fields, and audit records before it can claim readiness.

## 13. Fixture And Mocked-Provider Matrix

Implementation PRs must use synthetic fixtures and mocked providers. Do not
commit private documents, customer names, private organization names, real
provider payloads, credentials, personal data, or proprietary packets.

| Case | Required proof |
| --- | --- |
| no provider | Source-only documentation ingestion, fact readback, and findings readback still work. |
| disabled source policy | Eligible source facts exist, semantic work is skipped with `skipped_policy`. |
| allowed Markdown source | Chunk, redaction, mocked provider output, observation payload, and readback are deterministic. |
| denied ACL | No provider call occurs; denial is visible as policy state. |
| partial ACL | Content readback and provider work are denied by default. |
| redacted empty chunk | No provider call occurs; `redacted_empty` is recorded. |
| unsafe prompt input | No provider call occurs; unsafe class is recorded. |
| unsafe provider output | Observation is rejected or admitted as `unsafe`, never exact. |
| provider timeout | Retryable failure preserves job identity and fencing. |
| provider terminal error | Terminal failure is visible without blocking source-only docs. |
| exact observation | Reducer admits only after deterministic corroboration. |
| partial observation | Reducer preserves partial evidence without exact promotion. |
| ambiguous observation | Reducer reports ambiguity and all candidate handles stay provenance-only. |
| stale source revision | Prior observation becomes stale before a new revision is admitted. |
| unsupported observation kind | Unsupported state is explicit and queryable. |

## 14. Implementation Sequencing

1. Add no-provider tests that prove existing documentation source facts,
   documentation findings, evidence packets, and MCP/HTTP readback continue to
   work with semantic extraction disabled.
2. Add provider-profile and policy model tests with mocked profile handles only;
   do not wire provider SDKs or raw credentials.
3. Add chunking, fingerprinting, redaction, and job lifecycle tests over
   synthetic documentation sections.
4. Add mocked-provider observation parsing tests for allowed, unsafe,
   unsupported, timeout, and malformed outputs.
5. Add reducer admission tests for exact, partial, ambiguous, stale, unsafe,
   and unsupported states before writing admission logic.
6. Add API and MCP readback tests with bounded list, singleton, aggregate, ACL
   denial, no-provider unsupported, and envelope parity cases.
7. Add audit and telemetry tests before enabling any hosted worker path.
8. Run security review and schema review before any migration, hosted provider
   configuration, or provider call is merged.

## 15. Security And Schema Review Gates

Security review must explicitly approve:

- credential-handle storage and profile access boundaries;
- content-egress policy, source allowlists, ACL handling, and denial defaults;
- redaction policy for prompts, chunks, responses, excerpts, logs, metrics, and
  traces;
- provider prompt and response retention policy;
- audit log fields and access controls;
- handling of private URLs, personal data, tenant identifiers, source paths,
  provider request ids, and customer-specific strings.

Schema review must explicitly approve:

- any new fact kind, schema version, queue table, queue state, read model,
  migration, index, or capability-matrix row;
- compatibility behavior for stored observations when prompt, redaction,
  provider, or policy versions change;
- stale, retracted, and tombstone semantics;
- API/OpenAPI and MCP tool contract changes;
- upgrade, rollback, and reindex behavior for already persisted observations.

Until both reviews are recorded, semantic documentation observations must stay
design-only or test-only and must not be enabled in hosted, local, Compose, or
assistant-mediated runtime paths.
