# Operator Digest Contract

`operator_digest.v1` is the deterministic contract for a compact operator
report over Eshu's code-to-cloud graph. It is a presentation artifact, not a
canonical graph export. It may summarize existing evidence, route operators to
bounded follow-up calls, and show missing or ambiguous evidence, but it must not
promote hints into graph truth.

The digest exists for first-five-minutes onboarding and incident handoff: one
bounded artifact that tells an operator what Eshu knows, what it does not know
yet, and which questions are worth asking next.

## Boundary

The digest reads only bounded Eshu read surfaces. A renderer must not execute
raw Cypher, write graph state, enqueue reducer work, call external providers,
or generate prose from a model. Human-facing text is assembled from fixed
templates and structured fields.

The canonical sources remain the API, MCP, CLI, graph, content, status, and
truth-label contracts. The digest carries references back to those sources
through evidence handles, result references, playbook IDs, or route names.

## Inputs

An implementation must make these inputs explicit:

| Field | Required | Meaning |
| --- | --- | --- |
| `scope` | yes | Repository, service, workload, environment, or hosted project scope. |
| `profile` | yes | Runtime profile used for reads, from the capability matrix. |
| `max_sections` | no | Upper bound on rendered sections. Default: all supported sections. |
| `per_section_limit` | no | Maximum entries per section. Default: 10, maximum: 50. |
| `question_limit` | no | Maximum suggested questions. Default: 8, maximum: 25. |
| `freshness_floor` | no | Minimum acceptable freshness state. Default: include stale or building entries but mark them. |

The renderer must reject an empty scope. Optional inputs omitted by a caller use
the documented defaults; they are not inferred from ambient process state.

## CLI Usage

`eshu report` is the first deterministic CLI renderer for this contract:

```bash
eshu report --scope repo:owner/name --profile local_authoritative --json
```

The current CLI path is intentionally offline. It validates an explicit
share-safe scope, emits the `operator_digest.v1` shape, renders unsupported
sections as first-class limitations, and assembles suggested questions from
fixed templates. Each suggested question includes a source signal and a
human-readable `why` explaining which deterministic section signal caused the
question. It does not read raw Cypher, write graph state, enqueue reducer work,
call providers, or promote incomplete evidence into graph truth.

`--question-limit` defaults to `8` and must be between `0` and `25`. Scopes must
use share-safe labels such as `repo:owner/name`, `service:name`,
`workload:name`, `environment:name`, or `project:name`; URLs, absolute paths,
and machine-local values are rejected before the digest model is constructed.

Use `--artifact-out <path>` to write a shareable
`operator_digest_artifact.v1` JSON wrapper around the same digest:

```bash
eshu report --scope repo:owner/name --artifact-out operator-digest.repository.owner-name.local_authoritative.json
```

The artifact writer validates the digest before writing, writes the file with
owner-only permissions, deduplicates source references, records redaction and
validation metadata, and keeps stdout behavior unchanged. With `--json`, stdout
still carries the raw `operator_digest.v1` model while the write status goes to
stderr.

## Output Shape

The top-level artifact is:

| Field | Meaning |
| --- | --- |
| `schema` | Always `operator_digest.v1`. |
| `scope` | Redacted scope label and stable scope type. |
| `profile` | Runtime profile used to derive the digest. |
| `truth` | The lowest-authority truth or freshness state present in the included sections. |
| `sections` | Ordered digest sections, each with entries, limitations, and source references. |
| `suggested_questions` | Deterministic follow-up questions derived from section signals. |
| `limitations` | Artifact-level omissions, truncation, or unsupported surfaces. |
| `source_refs` | Route, tool, playbook, and evidence-handle references used by the renderer. |

`generated_at` may be emitted by a concrete CLI or API response as transport
metadata, but it is not part of ordering, IDs, scoring, or golden fixtures.

## Shareable Artifact

`operator_digest_artifact.v1` is the shareable local handoff wrapper around one
`operator_digest.v1` model. It is for onboarding, first-five-minutes support,
and incident handoff. It is not a graph export, cache backup, trace archive, or
prompt transcript.

Artifact writers must produce a single JSON document with this top-level shape:

| Field | Meaning |
| --- | --- |
| `schema` | Always `operator_digest_artifact.v1`. |
| `digest` | The embedded `operator_digest.v1` model. |
| `artifact` | Save metadata: stable artifact ID, writer kind, format, and validation status. |
| `redaction` | Redaction profile, version, applied rules, and any fields replaced with share-safe placeholders. |
| `source_refs` | Deduplicated route, MCP tool, CLI command, playbook, and evidence-handle references used by the digest. |
| `validation` | Determinism, schema, redaction, and public-safety validation results. |
| `limitations` | Artifact-level omissions, unsupported sections, truncation, stale data, or export-time warnings. |

Recommended local file names use only share-safe, deterministic components:

```text
operator-digest.<scope_type>.<scope_slug>.<profile>.json
```

`scope_slug` is a redacted label produced by the digest scope contract. It must
not contain raw repository paths, hostnames, account IDs, credentials, local
usernames, or absolute paths. If a safe slug cannot be produced, writers use an
opaque digest scope ID such as `scope-redacted`.

Artifact IDs are derived from schema version, digest scope, profile, redaction
profile, and source reference IDs. They must not include wall-clock time,
random values, local usernames, hostnames, or machine-specific paths. Writer
kind names describe the implementation path, such as `cli` or `api`, not the
operator or workstation that wrote the file.

### Artifact Safety

Artifacts must preserve enough evidence identity for a teammate to re-run
bounded reads, but must not embed private source material. The following are
allowed when they come from source contracts that mark them share-safe:

- route names and HTTP method/path templates, not concrete deployment hosts
- MCP tool names and bounded argument keys
- opaque evidence handles and result refs
- relative repository file paths already returned by Eshu read surfaces
- truth, freshness, limitation, truncation, and unsupported reason codes
- suggested-question IDs, templates, reasons, and next-call references

The following are never allowed in a shareable artifact:

- credentials, bearer values, session tokens, or secret material
- private hostnames, machine-specific endpoints, local absolute paths, or
  workstation usernames
- raw source excerpts unless the source route explicitly marks the excerpt
  share-safe
- prompts, provider responses, model traces, or tool transcripts
- unredacted cloud account IDs, customer names, or provider resource names that
  were not already converted to safe handles

If a disallowed value is the only evidence for an entry, the artifact keeps the
entry with `redacted: true`, a limitation, and a safe reason code. It must not
drop the entry silently or promote the remaining summary to stronger truth.

### Artifact Validation

Before writing a shareable artifact, implementations must validate:

1. `digest.schema` is `operator_digest.v1`.
2. `digest.scope`, `profile`, `truth`, `sections`, `suggested_questions`,
   `limitations`, and `source_refs` are present.
3. Section ordering, entry ordering, question ordering, and truncation flags
   match the deterministic ordering rules.
4. Every digest entry and question points to at least one source reference,
   limitation, missing-evidence marker, or unsupported reason.
5. Every emitted source, evidence, route, tool, or playbook reference resolves
   to an ID in the artifact-level `source_refs` set.
6. Redaction ran before artifact construction and records its profile/version.
7. No denied value class remains in `digest`, `source_refs`, `artifact`,
   `redaction`, `validation`, or `limitations`.
8. Transport-only fields such as wall-clock generation time do not affect
   artifact ID, digest ordering, or golden fixture comparison.

Validation failures must abort writing by default. A diagnostic mode may emit a
local-only failure report, but that report is not a shareable artifact and must
follow the same denied-value rules.

## Determinism

Given the same canonical read results and the same inputs, a renderer must emit
byte-stable section ordering and stable IDs.

Ordering rules:

1. Sort sections by the order listed in this document.
2. Within a section, sort higher-severity or higher-impact entries first when a
   section defines severity.
3. Break ties by stable entity handle, then route or tool name, then lexical
   reason code.
4. Mark `truncated: true` when a limit drops entries.

Entry IDs are derived from schema version, section ID, scope type, entity
handle, source route or tool, and reason code. They must not include wall-clock
time, random values, hostnames, or absolute local paths.

## Sections

### Hub Services

Summarizes services or repositories that appear central in the current scope.

| Field | Meaning |
| --- | --- |
| `entity` | Service, repository, workload, or package handle. |
| `signals` | Bounded counts such as callers, deployments, incidents, package edges, or citation handles. |
| `truth_class` | Derived from the source answer packet or truth envelope. |
| `freshness` | Freshness state from the source envelope. |
| `next_calls` | Follow-up route or MCP tool references. |

Current source mapping: service story, incident context, supply-chain impact,
relationship evidence, and bounded code relationship story surfaces can feed
this section. Implementations must mark unsupported source families rather than
inventing centrality from partial evidence.

### Cross-Domain Connections

Shows notable code-to-cloud, code-to-incident, dependency-to-service, or
repository-to-runtime links.

Each entry must include:

- source entity and target entity
- relationship family
- evidence handle or citation reference when available
- confidence or ambiguity signal when the source provides one
- truth class and freshness

Current source mapping: service story, incident context, supply-chain impact,
relationship evidence, and visualization packets. If a relationship lacks
canonical graph support, the entry stays a candidate or limitation and cannot be
rendered as a confirmed connection.

### Ambiguity Review Queue

Lists entries where an operator should disambiguate before acting.

Required reasons include:

- selector matched multiple candidates
- evidence family is present but does not reach admission
- freshness is stale or building for an otherwise useful answer
- missing evidence prevents a direct answer
- a result is partial or truncated

Current source mapping: answer packet `unsupported_reasons`, truth-label error
codes such as `ambiguous`, `missing_evidence`, and recommended next calls. A
ranked ambiguity queue is an implementation gap until a first-class bounded
surface exposes it.

### Freshness And Drift

Summarizes whether the scope is current enough to trust.

Required fields:

- generation or lifecycle reference when available
- freshness state
- stale, building, unavailable, or failed reason
- bounded recovery call or operator command

Current source mapping: generation lifecycle, semantic capability status,
index status, first-run evidence, and answer-packet freshness. The digest must
show stale or building state directly; it must not hide incomplete indexing
behind an otherwise confident summary.

### Unmanaged Or Orphaned Resources

Shows resources with evidence of runtime or provider presence but missing a
strong source-to-owner relationship.

Required fields:

- observed resource handle
- observed source family
- missing owner or missing repository signal
- evidence handle when available
- suggested ownership investigation

Current source mapping is partial. Some provider, registry, supply-chain, and
relationship evidence can identify candidates, but a canonical orphan-resource
read surface is an implementation gap. Until that surface exists, this section
must render as unsupported, partial, or candidate-only.

### Suggested Questions

Suggested questions are deterministic routing hints, not generated prose. They
must be assembled from fixed templates and current section signals.

Each question has:

| Field | Meaning |
| --- | --- |
| `id` | Stable ID derived from schema, scope, source signal, and template ID. |
| `question` | Fixed-template human prompt. |
| `source_signal` | Section entry or limitation that caused the question. |
| `why` | Human-readable justification tied to the source signal. |
| `reason` | Short reason code and display text. |
| `target` | Query playbook ID, MCP tool family, or HTTP route family. |
| `arguments` | Bounded arguments the operator can pass next. |
| `truth_expectation` | Expected answer truth class. |
| `evidence_refs` | Evidence handles, citation refs, or result refs when present. |

Question ordering is severity first, then unsupported or stale recovery, then
hub-service drilldowns, then cross-domain citations, then onboarding or support
packet follow-ups. Ties use the stable question ID.

## Redaction

Redaction happens before values enter the digest model. Renderers must:

- mask tokens, credentials, and bearer values
- avoid absolute local paths unless a route already redacted them
- reduce repository targets to share-safe labels when possible
- preserve evidence handles and opaque IDs only when the source contract marks
  them safe to share
- never include private hostnames or machine-specific endpoints in examples

If redaction would remove the only useful value, keep the entry with
`redacted: true` and a limitation instead of dropping the signal silently.

## Failure Handling

Unsupported and partial sections are first-class output:

- `unsupported` means the active profile or implementation cannot answer that
  section.
- `partial` means the section has useful bounded evidence but is stale,
  truncated, missing evidence, or candidate-only.
- `unavailable` means the source route or tool could not be reached.

The digest should still return other supported sections. A complete rendering
failure is reserved for invalid inputs or no available read surface at all.

## Verification Expectations

The implementation issue that adds a renderer must prove:

- stable output for identical inputs
- deterministic ordering and truncation
- redaction before model construction
- shareable artifact validation and denied-value rejection
- unsupported, partial, stale, and ambiguous cases
- no provider calls or graph writes
- source truth and freshness propagation into sections and questions
- source-backed `why` text for every suggested question

Docs-only changes to this contract require the strict docs build and
`git diff --check`.

No-Regression Evidence: `uv run --with mkdocs --with mkdocs-material --with
pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
covers Markdown, navigation, and cross-reference drift for this docs-only
artifact contract.

No-Observability-Change: the artifact contract adds no renderer, graph read,
graph write, queue, worker, CLI command, API route, MCP tool, metric, span, log
field, provider call, model call, or runtime knob. The implementation issue that
starts writing artifacts must add route- or command-specific no-regression and
observability evidence.

## Related

- [Reading Eshu Answers](reading-answers.md)
- [Answer Packet Contract](answer-packets.md)
- [Query Playbooks](query-playbooks.md)
- [First-Run Evidence](first-run-evidence.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Visualization Packet Contract](visualization-packets.md)
