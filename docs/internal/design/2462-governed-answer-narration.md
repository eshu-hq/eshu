# Governed Answer Narration Gate

Status: **ACCEPTED - OPTIONAL GOVERNED NARRATION ONLY.**

Refs #2462. Child epic: #2465. Related: #2410, #2455, #2456,
#1754, #1755, #1762, #1900, #1902, and design #430.

## Decision

Eshu should keep deterministic answer packets as the canonical answer surface
and may add optional governed narration as a non-canonical presentation layer
over an already-assembled, already-cited `AnswerPacket`.

This selects option B from #2462 with a narrow boundary:

- deterministic zero-key answer packets remain the default and complete path;
- narration is disabled unless governance, provider or assistant mode, budget,
  redaction, retention, citation, and publish-safety gates all allow it;
- narrated text is a convenience view, not canonical truth;
- every narrated sentence must trace to answer-packet evidence, limitations,
  unsupported reasons, or freshness state;
- no generated claim may become graph truth, route truth, or reducer evidence;
- denial, timeout, budget exhaustion, unsafe output, or missing citations fall
  back to the deterministic packet without changing its truth labels.

The child epic (#2465) owns future implementation decomposition. No
implementation may start until it preserves this gate, the
[Truth Label Protocol](../../public/reference/truth-label-protocol.md),
[Answer Packet Contract](../../public/reference/answer-packets.md), the
[Reading Eshu Answers](../../public/reference/reading-answers.md), the
[Semantic Extraction Policy](1754-semantic-extraction-policy.md), the
[Semantic Extraction Security Gates](1755-semantic-extraction-security-gates.md),
the [Hosted Governance Policy Model](1900-hosted-governance-policy-model.md),
the [Tenant And Workspace Isolation](1902-tenant-workspace-isolation.md) gate,
and the [Hosted Security Posture Gate](../../public/operate/hosted-security-posture.md).

## Why Not Deterministic Only

Deterministic-only packets are still correct, auditable, and aligned with
design #430: search, semantic observations, and answer composition are derived
read models, not canonical graph truth. They are also the only acceptable
zero-key baseline.

The competitive gap from #2462 is presentation, not truth. Users expect a short
answer that reads naturally, but Eshu must not let fluency outrank evidence.
Optional governed narration addresses that gap only if it is treated as a
bounded renderer over the packet the query layer already produced.

## Required Flow

The narration path must preserve this ownership order:

```text
query surface -> ResponseEnvelope -> AnswerPacket -> citation hydration ->
governance and safety gates -> optional narration -> validation -> presentation
```

The route envelope and answer packet stay canonical. Narration never performs
new graph reads, content reads, provider-source retrieval, reducer work, queue
work, or truth classification. If a caller needs more evidence, it must follow
the packet's existing `recommended_next_calls` and rebuild a new packet from
the canonical routes.

## Traceability Invariant

Every sentence in narrated output must carry machine-checkable provenance to at
least one of these packet-owned inputs:

- an evidence citation handle or hydrated citation;
- a `truth`, `truth_class`, or freshness field;
- a `limitation`, `missing_evidence`, `unsupported_reasons`, or truncation
  signal;
- a bounded recommended next call already present in the packet.

Validation must reject output that:

- asserts facts absent from the packet or citations;
- weakens `supported=false`, `partial=true`, stale freshness, truncation, or
  missing-evidence signals;
- converts fallback, code-hint, semantic-observation, or derived results into
  authoritative graph claims;
- omits a citation for a factual sentence;
- emits raw source, private paths, private hostnames, credentials, tokens,
  prompt text, provider responses, or unsafe excerpts.

## Governance Contract

Narration inherits the existing optional-provider posture:

- no provider or assistant exchange is enabled by default;
- provider profile configuration is inventory only, not permission;
- policy must explicitly allow the source class, scope, actor class, budget,
  redaction posture, retention posture, and egress class;
- hosted read authorization and tenant or workspace scope must be enforced
  before any narration work starts, not by filtering completed output;
- missing, invalid, stale, partial, or denied policy fails closed;
- hosted enforcement must surface only safe status fields and reason codes;
- raw prompts, raw provider responses, provider error bodies, and credential
  material are not retained by default.

Assistant-mediated narration may reuse the packet boundary from
[Assistant-Mediated Semantic Extraction Packets](1762-assistant-mediated-semantic-extraction-packets.md),
but the response is still untrusted presentation text. Hosted-provider narration
must pass the #1755 security preflight before content leaves the runtime.

## Observability And Audit Requirements

Before any runtime path sends a packet to a provider or assistant, it must add
operator-visible proof for:

- disabled, denied, budget-exhausted, unsafe, timeout, and provider-unavailable
  states;
- narration-request count, validation result count, and fallback count by
  bounded reason code;
- prompt, response, and citation-validator versions by safe revision hash;
- redaction and publish-safety rejection counts by low-cardinality class;
- audit-safe status that never exposes raw packet content, source ids, private
  endpoints, prompts, responses, provider request ids, or credential handles.

No-Observability-Change: this ADR changes design guidance only. It adds no
runtime path, provider call, queue, graph query, content read, API route, MCP
tool, metric, span, log, audit table, schema, or status field.

## Edge Cases Future Work Must Prove

Implementation issues under #2465 must test at least:

- no provider configured;
- provider configured but policy missing, stale, partial, or denied;
- packet lacks citations for a factual answer;
- packet is unsupported, partial, stale, or truncated;
- narrator tries to add an uncited fact;
- narrator tries to hide a limitation or unsupported reason;
- narrator downgrades publish-safety by emitting a private path, hostname,
  credential-like value, prompt text, or provider response;
- duplicate provider or assistant responses;
- timeout, rate limit, and budget exhaustion;
- stale packet or citation hash;
- API, MCP, CLI, and console surfaces preserving the canonical envelope and
  deterministic packet when narration is unavailable.

## Non-Goals

- No mandatory hosted provider dependency.
- No default provider traffic.
- No raw prompt or raw provider response retention.
- No new truth taxonomy.
- No unbounded retrieval, whole-graph search, or graph traversal inside
  narration.
- No promotion of generated text to facts, graph truth, search truth, or
  reducer evidence.

## Acceptance For This Gate

This gate is complete when:

- this ADR records the selected direction;
- #2465 exists as the child epic for the implementation contract;
- the current deterministic packet path remains the required fallback and
  canonical answer source;
- verification for the docs-only change runs the strict docs gate and
  whitespace checks.
