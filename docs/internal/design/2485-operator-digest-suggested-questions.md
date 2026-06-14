# Operator Digest Suggested-Question Contract

Status: Proposed. Child of #2455. Implements the design-only suggested-question
gate for #2485.

## Decision

`operator_digest.v1` suggested questions are deterministic advisory metadata.
They route an operator toward bounded follow-up reads, but they do not execute
tools, perform graph queries, generate prose, or change canonical graph truth.

Every question must be derived from a digest section entry, limitation,
freshness state, missing-evidence marker, or unsupported state. A question that
cannot name its source signal is invalid.

## Invariants

- Questions preserve the source truth class, freshness state, limitation, and
  redaction status.
- Questions use fixed templates. They do not use model-generated prose.
- Stable IDs, ranking, and truncation depend only on digest content and request
  limits.
- A question never makes an unsupported, stale, fallback, ambiguous, or
  candidate-only signal sound authoritative.
- Missing evidence creates recovery questions; it must not be hidden behind a
  confident summary.
- Redacted source values stay redacted in the question text, arguments, and
  source references.

## Question Shape

Future renderers should emit this shape inside `suggested_questions`:

| Field | Meaning |
| --- | --- |
| `id` | Stable ID derived from schema, scope, template ID, source signal, and bounded arguments. |
| `template_id` | Closed template identifier from this document. |
| `question` | Fixed-template display text. |
| `why` | Fixed-template reason text derived from the source signal. |
| `source_signal` | Section entry ID, limitation ID, or source row ID that caused the question. |
| `source_section` | Digest section that contributed the signal. |
| `target` | Query playbook ID, MCP tool family, or HTTP route family. |
| `arguments` | Bounded arguments copied from safe source handles. |
| `truth_expectation` | Expected answer truth class, never stronger than the source allows. |
| `freshness` | Source freshness state. |
| `redacted` | True when any source argument or display value was redacted. |

`question` and `why` are presentation strings. Programmatic clients should use
`template_id`, `target`, `arguments`, `truth_expectation`, and `source_signal`.

## Signal Classes

Signal classes are closed for the first implementation. Later classes require a
new design update and fixture coverage.

| Signal class | Source section | Primary purpose |
| --- | --- | --- |
| `stale_recovery` | `freshness_and_drift` | Re-run or inspect indexing before acting on stale data. |
| `unsupported_recovery` | any section | Route to a supported profile or bounded alternative. |
| `ambiguous_selector` | `ambiguity_review_queue` | Ask the operator to disambiguate before graph-backed reads. |
| `missing_evidence` | any section | Request citation, relationship, or lifecycle proof for an incomplete result. |
| `hub_drilldown` | `hub_services` | Inspect the highest-impact service, repository, or workload. |
| `cross_domain_citation` | `cross_domain_connections` | Hydrate evidence for a notable code-to-cloud or code-to-incident link. |
| `orphan_owner_review` | `unmanaged_or_orphaned_resources` | Investigate missing ownership without claiming an owner. |

## Templates

Templates are fixed strings with named placeholders. Placeholders may only use
redacted-safe labels, stable handles, counts, truth class, freshness state, and
target family names.

| `template_id` | Question template | Why template | Target |
| --- | --- | --- | --- |
| `recover_stale_scope` | `Refresh or inspect {scope_label} before acting on stale digest entries.` | `{source_section} reports freshness={freshness}.` | `incremental_freshness_readiness` |
| `switch_supported_profile` | `Check which runtime profile can answer {capability_label}.` | `{source_signal} is unsupported in profile {profile}.` | `get_semantic_capability_status` |
| `disambiguate_selector` | `Resolve {entity_label} before asking graph-backed follow-ups.` | `{candidate_count} candidates matched the selector.` | `get_code_relationship_story` |
| `hydrate_missing_evidence` | `Hydrate citations for {entity_label}.` | `{source_signal} has missing evidence.` | `build_evidence_citation_packet` |
| `inspect_hub_service` | `Inspect the service story for {entity_label}.` | `{entity_label} ranked as a hub by deterministic source counts.` | `service_story_citation` |
| `cite_cross_domain_link` | `Cite the evidence for {relationship_label}.` | `The digest found a notable cross-domain connection.` | `build_evidence_citation_packet` |
| `review_orphan_owner` | `Investigate ownership for {resource_label}.` | `{resource_label} lacks a confirmed source owner.` | `find_unmanaged_resource_owners` |

If a placeholder value is missing or redacted, use the safe fallback label
`redacted value` or `unknown target` and set `redacted: true` when applicable.

## Ranking

Rank questions in this order:

1. `stale_recovery`
2. `unsupported_recovery`
3. `ambiguous_selector`
4. `missing_evidence`
5. `orphan_owner_review`
6. `cross_domain_citation`
7. `hub_drilldown`

Within the same class, sort by:

1. source severity, where failed or unavailable beats stale, stale beats
   building, and building beats fresh;
2. candidate count descending for ambiguity questions;
3. missing evidence count descending for missing-evidence questions;
4. source section order from the operator digest contract;
5. source signal ID ascending;
6. template ID ascending.

This order intentionally prioritizes recovery and trust repair before
exploration. It prevents a polished hub-service drilldown from hiding stale,
unsupported, or ambiguous evidence.

## Stable IDs

Question IDs use this normalized form:

```text
operator_digest.v1/question/{scope_type}/{template_id}/{source_signal_id}/{argument_hash}
```

`argument_hash` is a deterministic hash over sorted, redacted-safe argument
keys and values. It must exclude timestamps, transport metadata, hostnames,
absolute paths, random IDs, and any value redacted by the source model.

If hashing is unavailable in an offline golden fixture, fixtures may use
`argument_hash: fixed` only when every argument appears directly in the golden
file.

## Limits And Truncation

The default `question_limit` is 8 and the maximum is 25. A renderer must select
questions by ranking before applying the limit.

When questions are dropped:

- set `suggested_questions_truncated: true`;
- record the dropped count;
- record dropped counts by signal class;
- never drop all recovery questions while keeping lower-priority exploration
  questions.

If the input contains no valid question signals, emit an empty question list
and a limitation saying `no_question_signals`.

## Edge Cases

Future implementation fixtures must cover:

| Case | Expected behavior |
| --- | --- |
| Empty evidence | Emits no questions and records `no_question_signals`. |
| Equal-score ties | Sorts by source signal ID, then template ID. |
| Ambiguous selector | Emits `disambiguate_selector` before hub or citation questions. |
| Stale truth | Emits `recover_stale_scope` before exploratory questions. |
| Unsupported source | Emits `switch_supported_profile` and preserves unsupported truth. |
| Redacted values | Keeps redacted placeholders and sets `redacted: true`. |
| Missing source handles | Emits a limitation or recovery question, not a citation question with empty handles. |
| Section truncation | Emits a truncation limitation and does not ask about dropped entries. |

## Non-Goals

- No CLI, API, MCP, graph query, reducer, provider, hosted narration, model, or
  runtime implementation.
- No generated prose.
- No edits to active PR files for #2478, #2480, #2482, or #2484.
- No promotion of questions into graph truth, route truth, reducer evidence, or
  capability support claims.

## No Runtime Impact

This design adds no runtime reader, renderer, graph query, queue work, provider
call, model call, API route, MCP tool, CLI command, telemetry, metric, span, or
log.

No-Observability-Change: suggested-question design remains offline
documentation. Operators continue to diagnose current answers through existing
truth labels, status, telemetry, and query surfaces.

## Acceptance For #2485

- This design defines deterministic question templates, signal inputs, ranking,
  tie-breakers, stable IDs, and truncation behavior.
- It states suggested questions are advisory presentation metadata and preserve
  source truth and freshness.
- It covers empty evidence, equal-score ties, ambiguous or stale truth, redacted
  values, missing source handles, and section truncation.
- Verification runs the strict docs build and `git diff --check`.
