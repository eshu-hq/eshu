# Service Intelligence Report Contract

The **service intelligence report** is an operator-ready investigation artifact
that composes Eshu's existing answer evidence — the service story, supply-chain
impact inventory, incident context, and evidence citation packets — into one
fixed, ordered document a responder can read top to bottom.

It is the Eshu-native answer to single-file graph reports. Instead of a single
LLM-written narrative, the report arranges already-produced, truth-labelled
answer evidence and ties every section to code-to-cloud evidence, preserved
truth labels, explicit missing-evidence reasons, and recommended bounded next
calls.

## When to use it

Use the report when you need a complete, trustworthy snapshot of one service
before acting on it — during an incident, a change review, an ownership
hand-off, or a dependency audit. Use the individual answer routes
(`get_service_story`, `get_incident_context`, `get_supply_chain_impact_inventory`)
when you only need one slice.

The report never runs an LLM-only interpretation path. It is generated
deterministically from existing API, MCP, and CLI evidence, so the same evidence
always produces the same report.

## Sections

A report always carries the same five sections in the same order, regardless of
which evidence was available:

| Section | Kind | Source route |
| --- | --- | --- |
| Service identity | `identity` | `get_service_story` |
| Code-to-runtime trace | `code_to_runtime` | `get_service_story` |
| Deployment and configuration influence | `deployment_config` | `get_service_story` / `compare_environments` |
| Supply-chain evidence | `supply_chain` | `get_supply_chain_impact_inventory` |
| Incidents, support, and runbook evidence | `incidents_support` | `get_incident_context` |

The `identity` section is the **truth anchor**: report-level `supported`,
`truth`, and `truth_class` are copied from it. A report for an unknown service
is `unsupported`.

## Generating a report from the CLI

`eshu service-report` composes a report offline from a captured `get_service_story`
response, with no query, store, or LLM path:

```bash
# capture the service story envelope, then compose the report
eshu service-report --from service-story.json          # human-readable
eshu service-report --from service-story.json --json    # machine-readable report
cat service-story.json | eshu service-report            # or pipe on stdin

# optionally fold in a captured supply-chain impact inventory response
eshu service-report --from service-story.json --supply-chain-from supply-chain.json
```

The input is the service story route response — the standard
`{"data": ..., "truth": ...}` envelope, or a bare dossier object. The command
maps the dossier into the `identity`, `code_to_runtime`, and `deployment_config`
sections and carries the captured truth envelope verbatim. `--supply-chain-from`
folds a captured `get_supply_chain_impact_inventory` response into the
`supply_chain` section. The offline CLI emits the `incidents_support` section
`unsupported` (with its fallback next call) because it has no store access. A
response with no truth composes an `unsupported` report rather than fabricating
confidence.

The live API/MCP route (`GET /api/v0/services/{service_name}/intelligence-report`,
`get_service_intelligence_report`) additionally sources `supply_chain` from
reducer-owned supply-chain impact inventory scoped to the resolved workload. That
section carries supply-chain-impact truth
(`supply_chain.impact_findings.aggregate`), not the service-story platform truth.
No inventory or a load error leaves the section `unsupported` with its fallback
rather than fabricating an empty supported section.

The live route also sources `incidents_support` from durable incident-routing
evidence: it resolves the workload's catalog service id, loads that service's
routed incidents, and the section carries incident-context truth
(`incident.context.read`). It attributes incidents only when the workload
resolves to exactly one catalog service; an unresolved or ambiguous workload, a
load error, or no routed incidents leave the section `unsupported` with its
fallback rather than fabricating a false "no incidents".

## Honesty under composition

Each section embeds a canonical [answer packet](answer-packets.md), so it
preserves the source truth envelope, evidence handles, limitations, and
recommended next calls without reclassifying them. A section's `status` is one
of:

- `supported` — backed by resolved, fresh evidence;
- `partial` — usable but truncated, stale, carrying unresolved evidence
  handles, or resolved with no supporting evidence;
- `unsupported` — the source route errored or returned no truth.

The report follows the same no-confident-summary rule as answer packets: an
empty or unsupported section **drops its summary** rather than presenting "no
rows" as a confident statement. Empty and unsupported sections stay visible and
carry:

- an explicit **limitation** explaining what is not resolved, and
- a bounded **recommended next call** naming a real tool, route, or
  [query playbook](query-playbooks.md) plus any non-sensitive subject arguments
  needed to gather the missing evidence.

A report is `partial` whenever any present section is partial or unsupported, so
it never reads as complete while sections are missing or unresolved handles
remain.

## Guided investigations

The report does not stop at "here is what we know" — it tells you what to look at
next. Composition derives a bounded list of **suggested investigations** from the
report's own signals. Each suggestion is grounded in one observable basis:

| Basis | When it fires | Recommended next call |
| --- | --- | --- |
| `missing_evidence` | requested evidence handles did not resolve | `build_evidence_citation_packet` (`/api/v0/evidence/citations`) |
| `stale_freshness` | a section is stale or building with a proven cause | the section's bounded freshness next check |
| `ambiguous_target` | the source route could not pick one subject | `resolve_entity` (`/api/v0/entities/resolve`) |
| `unsupported_lane` | a section's evidence lane is unavailable | the section's fallback call |
| `high_impact_relationship` | a high-impact relationship is flagged | `get_relationship_evidence` |

Every suggestion carries:

- a **reason** — one sentence on why it is suggested;
- an **evidence basis** — the concrete signal it was derived from (the
  unresolved handle keys, the freshness cause, the ambiguity message);
- a bounded **next call** naming a real tool, route, or
  [query playbook](query-playbooks.md); and
- an **expected truth class** — what the next call should yield, sourced from the
  section truth or the linked playbook, never invented.

Suggestions are **never speculative**: an ambiguous target produces a
disambiguation suggestion rather than a guessed winner, and a fully supported
report with no gaps produces no suggestions at all. The list is de-duplicated
and bounded so it stays scannable.

## Determinism

Report composition is a pure function. The same evidence yields a byte-identical
report: sections are emitted in catalog order, the first input for a section
kind wins, and aggregated limitations and next calls are de-duplicated in stable
encounter order. Reports are safe to diff, cache, and score in a dogfood gate.

## Dogfood scorecard

A polished report that quietly omits citations, hides truncation, or upgrades a
truth class is worse than no report. The answer-quality dogfood gate scores every
report against six criteria and fails the build when any is violated:

| Criterion | Rejects |
| --- | --- |
| `unsupported_claim_avoidance` | a confident summary on an unsupported or evidence-less section |
| `citation_coverage` | a supported claim with no evidence handle or citation |
| `truth_class_preservation` | an upgraded or invented truth class |
| `limitation_visibility` | a partial or unsupported section that hides why |
| `truncation_signaling` | truncation that is not marked partial and stated |
| `next_call_executability` | a recommended next call or investigation with no real tool, route, or playbook |

The gate ships a share-safe report fixture corpus — one honest happy path, one
honest partial report, and one fixture per failure mode — so a regression that
reintroduces any of these failures is caught in CI and local dogfood runs. See
the [Answer Quality Scorecard](local-testing/answer-quality-scorecard.md) for how
to run the gate.

## Truth preservation

The report introduces no new truth source. Every section's truth is the source
route's `TruthEnvelope`, classified by the same answer-packet rules documented
in the [Truth Label Protocol](truth-label-protocol.md). A stale source stays
stale in the report; an ambiguous source stays ambiguous. The report's job is to
arrange truth honestly, not to upgrade it.
