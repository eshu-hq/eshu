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

## Honesty under composition

Each section embeds a canonical [answer packet](answer-packets.md), so it
preserves the source truth envelope, evidence handles, limitations, and
recommended next calls without reclassifying them. A section's `status` is one
of:

- `supported` — backed by resolved, fresh evidence;
- `partial` — usable but truncated, stale, or resolved with no supporting
  evidence;
- `unsupported` — the source route errored or returned no truth.

The report follows the same no-confident-summary rule as answer packets: an
empty or unsupported section **drops its summary** rather than presenting "no
rows" as a confident statement. Empty and unsupported sections stay visible and
carry:

- an explicit **limitation** explaining what is not resolved, and
- a bounded **recommended next call** naming a real tool, route, or
  [query playbook](query-playbooks.md) to gather the missing evidence.

A report is `partial` whenever any present section is partial or unsupported, so
it never reads as complete while sections are missing.

## Determinism

Report composition is a pure function. The same evidence yields a byte-identical
report: sections are emitted in catalog order, the first input for a section
kind wins, and aggregated limitations and next calls are de-duplicated in stable
encounter order. Reports are safe to diff, cache, and score in a dogfood gate.

## Truth preservation

The report introduces no new truth source. Every section's truth is the source
route's `TruthEnvelope`, classified by the same answer-packet rules documented
in the [Truth Label Protocol](truth-label-protocol.md). A stale source stays
stale in the report; an ambiguous source stays ambiguous. The report's job is to
arrange truth honestly, not to upgrade it.
