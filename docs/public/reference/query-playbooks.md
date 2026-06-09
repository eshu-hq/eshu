# Query Playbooks

A **query playbook** is a machine-readable, deterministic, versioned description
of a common starter-prompt or cookbook workflow. It names the ordered first-class
tool calls a workflow takes, their bounded parameters, the truth and evidence
expected at each step, and the failure modes a caller must handle. A playbook
describes **how to reach an [answer packet](answer-packets.md)** for a prompt
family; it does not execute anything itself.

The implementation lives in `go/internal/query/query_playbook.go` (contract and
resolution), `go/internal/query/query_playbook_validate.go` (structural
validation), and `go/internal/query/query_playbook_catalog.go` (the versioned
catalog). Read the [Truth Label Protocol](truth-label-protocol.md) and the
[Answer Packet Contract](answer-packets.md) first: a playbook reuses the
`AnswerTruthClass` taxonomy and the `recommended_next_calls` / evidence-handle
shapes already defined there rather than introducing new ones.

## Why this contract exists

Starter prompts and the [MCP Cookbook](mcp-cookbook.md) describe recurring
workflows ("tell me the story of this service and cite the evidence", "how does
this repository handle X") as prose. Prose workflows are easy for an agent to
drift on: it can guess a tool, skip a bound, or invent a parameter. A playbook
turns the workflow into data that is:

- **Deterministic** — resolving a playbook with the same inputs always yields the
  same ordered, fully specified call sequence. There is no `Date.now`, no
  randomness, and no read of external or live-backend state.
- **Bounded** — every list step declares a default limit, so a resolved call is
  never unbounded.
- **Versioned** — each playbook carries an explicit semantic `version`, and the
  catalog identity (`id` + `version`) is pinned by a golden test so it cannot
  drift silently.
- **First-class** — every step references a real read-only MCP tool, validated
  against the `ReadOnlyTools` registry. Raw Cypher tools (`execute_cypher_query`,
  `execute_language_query`, `visualize_graph_query`) are rejected by validation.

## Contract

A `QueryPlaybook` declares:

| Field | Meaning |
| --- | --- |
| `id` | Stable catalog identifier. |
| `name` | Human-readable playbook name. |
| `version` | Semantic version of the definition. |
| `prompt_family` | Canonical prompt family, aligned with `AnswerPacket.PromptFamily`. |
| `required_inputs` | Declared inputs (`name`, `type`, `required`). The only external state a playbook reads. |
| `steps` | Ordered bounded calls. |
| `failure_modes` | Declared truth/error conditions and recommended fallbacks. |

Each `PlaybookStep` declares a first-class `tool`, bounded `params` (each bound
either from a declared input or from a declared constant such as a default
limit), the `expected_truth` (an `AnswerTruthClass`), the `evidence_expected`,
and optional `drilldowns`. Each `PlaybookFailureMode` declares a `condition`,
its `meaning`, and a first-class `fallback`.

### Resolution

`(QueryPlaybook).Resolve(inputs)` is a pure resolver. It validates the playbook,
rejects any input the playbook does not declare, requires every `required`
input, binds each step's parameters, and returns a `ResolvedPlaybook`: the
ordered `ResolvedCall`s (tool name plus concrete bounded `arguments`, the
expected truth class, the expected evidence, and the drilldowns) plus the
declared failure modes carried forward. It reads no external state, so it is safe
to run in a test to prove a workflow without a live backend. "Execute" in the
test suite means exactly this: resolve to real tool names, bounded params, and
declared expectations — not a call to a graph backend.

Validation guarantees:

- Identity fields, at least one step, and at least one failure mode are present.
- Every step references a known, non-raw-Cypher tool with a declared truth class
  and expected evidence.
- Every `from_input` parameter references a declared input.
- Optional inputs that a caller omits are dropped from the resolved arguments
  rather than emitted as empty values.

## Catalog

The current catalog is returned by `PlaybookCatalog()`:

| ID | Version | Prompt family | Workflow |
| --- | --- | --- | --- |
| `service_story_citation` | 1.0.0 | `service.story` | `get_service_story` → `build_evidence_citation_packet`. Pull the one-call service dossier, then hydrate its evidence handles into a bounded citation packet. |
| `repository_code_topic_investigation` | 1.0.0 | `code.topic` | `investigate_code_topic` → `get_code_relationship_story`. Rank files and symbols for a topic, then read the graph-backed relationship story behind the top entity. |
| `documentation_truth_citation` | 1.0.0 | `documentation.truth` | `get_documentation_evidence_packet` → `check_documentation_evidence_packet_freshness`. Resolve a finding into a bounded evidence packet, then confirm it is still current before citing. |

Each catalog playbook declares its own failure modes — for example "service not
found" recommends `investigate_service`, and "citation packet truncated"
recommends raising the bounded limit or sending the next handle batch.

## API / MCP exposure

The playbook contract and catalog ship as an in-process Go contract. Exposure
over a read-only, bounded API/MCP surface is deliberate follow-up work: any such
surface must keep the truth labels intact, stay read-only, and never expose a
raw-Cypher step. Until then, prompt surfaces consume the catalog in-process and
the cross-check test in `go/internal/mcp` guarantees every referenced tool name
is a real read-only MCP tool.

## Verification

```bash
cd go && go test ./internal/query -count=1 -run Playbook
cd go && go test ./internal/mcp -count=1 -run QueryPlaybookTools
```
