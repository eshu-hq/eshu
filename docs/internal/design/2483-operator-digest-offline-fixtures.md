# Operator Digest Offline Fixture And Golden Ordering Contract

Status: Proposed. Child of #2455. Implements the design-only fixture gate for
#2483.

## Decision

`operator_digest.v1` implementation work must start from an offline,
public-safe fixture suite before any CLI, API, MCP, graph query, or renderer
path ships. The fixture suite is the deterministic test oracle for digest
sections, suggested questions, redaction, unsupported states, and golden
ordering.

The fixture is synthetic. It models bounded Eshu read results and their truth
metadata, not live source systems. It must not contain real hostnames, IP
addresses, tokens, private paths, customer names, provider account IDs, or
machine-specific endpoints.

## Invariants

- The fixture represents presentation inputs only; it is not canonical graph
  truth.
- Every digest entry traces to a fixture source reference, limitation, truth
  label, freshness state, or missing-evidence marker.
- Unsupported, partial, stale, ambiguous, truncated, and redacted evidence stay
  visible in the expected output.
- Golden comparison ignores transport-only fields such as wall-clock generation
  time.
- Stable ordering uses only fixture content and documented tie-breakers.
- Redaction happens before expected digest model construction.

## Fixture Layout

Future implementation should place concrete files under
`tests/fixtures/operator_digest/`. This design reserves the shape without
creating runtime readers.

```text
tests/fixtures/operator_digest/
  README.md
  cases/
    service-hub-mixed-evidence.input.yaml
    service-hub-mixed-evidence.golden.yaml
    empty-scope.input.yaml
    empty-scope.golden.yaml
    redaction.input.yaml
    redaction.golden.yaml
```

Each case has one `*.input.yaml` and one `*.golden.yaml`. The input is the
bounded source material. The golden file is the exact digest model after
redaction, ordering, truncation, and suggested-question assembly.

## Input Schema

The input schema is intentionally close to Eshu answer packets and envelopes so
future tests can adapt live captures into fixture rows without changing digest
logic.

```yaml
schema: operator_digest_fixture.v1
case_id: service-hub-mixed-evidence
scope:
  type: service
  id: svc.checkout
  label: checkout-service
profile: local_authoritative
limits:
  per_section_limit: 2
  question_limit: 4
sources:
  - id: service_story.checkout
    family: service_story
    route: GET /api/v0/services/{service}/story
    truth:
      level: exact
      basis: authoritative_graph
      freshness: {state: fresh}
    supported: true
    partial: false
    truncated: false
    evidence_handles:
      - kind: entity
        handle: entity:svc.checkout
    data:
      entity: svc.checkout
      caller_count: 7
      deployment_count: 2
  - id: relationship.ambiguous-worker
    family: relationship_evidence
    route: POST /api/v0/code/relationships
    truth:
      level: derived
      basis: content_index
      freshness: {state: fresh}
    supported: true
    partial: true
    truncated: false
    limitations:
      - selector matched multiple candidates
    data:
      target_resolution:
        status: ambiguous
        candidates:
          - entity_id: func.worker.a
            file_path: src/worker_a.go
          - entity_id: func.worker.b
            file_path: src/worker_b.go
  - id: freshness.lifecycle
    family: generation_lifecycle
    route: GET /api/v0/status/generation
    truth:
      level: derived
      basis: runtime_state
      freshness: {state: stale}
    supported: true
    partial: true
    missing_evidence:
      - latest generation not projected
```

Required top-level fields:

| Field | Meaning |
| --- | --- |
| `schema` | Always `operator_digest_fixture.v1`. |
| `case_id` | Stable lowercase identifier for the fixture pair. |
| `scope` | Synthetic scope type, ID, and share-safe label. |
| `profile` | Runtime profile represented by the source rows. |
| `limits` | Explicit section and question limits used by the expected output. |
| `sources` | Ordered source rows before digest sorting. |

Source rows must include `id`, `family`, `truth`, `supported`, `partial`, and
`data`. Route, tool, evidence handles, limitations, missing evidence, and
truncation are optional only when the source family cannot produce them.

## Golden Output Schema

Golden files compare the digest model, not rendered Markdown or terminal text.

```yaml
schema: operator_digest.v1
scope:
  type: service
  label: checkout-service
profile: local_authoritative
truth:
  lowest_level: derived
  lowest_freshness: stale
sections:
  - id: hub_services
    truncated: false
    entries:
      - id: operator_digest.v1/hub_services/service/svc.checkout/service_story.checkout/centrality
        entity: svc.checkout
        truth_class: deterministic
        freshness: fresh
        source_refs: [service_story.checkout]
  - id: ambiguity_review_queue
    truncated: false
    entries:
      - id: operator_digest.v1/ambiguity_review_queue/service/func.worker.a/relationship.ambiguous-worker/ambiguous_selector
        status: ambiguous
        reason: ambiguous_selector
        source_refs: [relationship.ambiguous-worker]
  - id: freshness_and_drift
    truncated: false
    entries:
      - id: operator_digest.v1/freshness_and_drift/service/freshness.lifecycle/stale_generation
        status: stale
        reason: latest generation not projected
        source_refs: [freshness.lifecycle]
suggested_questions:
  - id: operator_digest.v1/question/service/relationship.ambiguous-worker/disambiguate_selector
    source_signal: relationship.ambiguous-worker
    reason: disambiguate_selector
    target: get_code_relationship_story
    truth_expectation: deterministic
limitations: []
source_refs:
  - service_story.checkout
  - relationship.ambiguous-worker
  - freshness.lifecycle
```

Golden files must omit wall-clock timestamps. If a future transport response
needs `generated_at`, the renderer test strips it before comparison.

## Ordering Rules

Section order is fixed:

1. `hub_services`
2. `cross_domain_connections`
3. `ambiguity_review_queue`
4. `freshness_and_drift`
5. `unmanaged_or_orphaned_resources`
6. `suggested_questions`

Within sections:

- `freshness_and_drift`: failed, unavailable, stale, building, fresh; then source
  ID.
- `ambiguity_review_queue`: ambiguous selector, missing evidence, unsupported,
  partial, truncated; then candidate count descending; then source ID.
- `hub_services`: signal count descending, deployment count descending, caller
  count descending, entity ID ascending.
- `cross_domain_connections`: truth level strongest first, confidence
  descending when present, relationship family, source ID.
- `unmanaged_or_orphaned_resources`: missing owner before missing repository,
  then source family, resource handle.
- `suggested_questions`: stale or unsupported recovery, ambiguity resolution,
  citation hydration, hub drilldown, onboarding handoff; then question ID.

Any entry dropped by a limit sets `truncated: true` on its section. The golden
must include a limitation naming the source family and dropped count.

## Required Edge Cases

Future fixture implementation must include at least these cases:

| Case | Required proof |
| --- | --- |
| `empty-scope` | Invalid empty scope fails before rendering; no ambient repo or profile inference. |
| `unsupported-section` | Unsupported source families produce visible unsupported sections or limitations. |
| `ambiguous-selector` | Multiple candidates remain visible and sort into the ambiguity queue. |
| `truncated-section` | Limit plus one source rows set `truncated: true` and preserve deterministic first entries. |
| `stale-freshness` | Stale or building freshness lowers artifact truth/freshness summary. |
| `missing-evidence` | Missing evidence creates a limitation and a recovery question. |
| `redaction` | Credentials, absolute paths, hostnames, and endpoint-like strings are redacted before golden comparison. |

## Public-Safe Synthetic Data Rules

Use synthetic IDs such as `svc.checkout`, `repo.payments`, `func.worker.a`, and
`resource.queue.orders`. Use example file paths like `src/worker_a.go` only
after confirming they are relative paths. Use placeholder routes, not concrete
hosts. Do not include IP addresses, real account IDs, real namespaces, private
repository paths, or token-shaped strings.

If an edge case needs an unsafe-looking value, use a symbolic token such as
`credential-like-value` and assert that the golden output contains `redacted`.

## No Runtime Impact

This design adds no runtime reader, renderer, graph query, queue work, provider
call, model call, API route, MCP tool, CLI command, telemetry, metric, span, or
log. It is a gate for later implementation issues.

No-Observability-Change: the fixture contract is offline documentation only.
Operators continue to diagnose current answer and graph behavior through the
existing envelope, truth label, status, telemetry, and query surfaces.

## Acceptance For #2483

- This design records the offline fixture schema and golden ordering rules.
- It describes one public-safe synthetic fixture shape.
- It names empty, ambiguous, truncated, stale, missing-evidence, unsupported,
  and redaction edge cases.
- It avoids active PR paths for #2478, #2480, and #2482.
- Verification runs the strict docs build and `git diff --check`.
