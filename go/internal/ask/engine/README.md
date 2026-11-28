# ask/engine

Package `engine` implements the **Ask Eshu Tier 1 agent loop**: a bounded,
read-only, evidence-backed orchestration layer that drives an LLM through the
Eshu query surface to answer natural-language questions about a user's software
stack.

## Overview

The engine accepts a question, drives LLM completions through an injected
`provider.Adapter`, dispatches tool calls in-process through a `Runner`, and
assembles a structured `Answer` containing canonical `AnswerPackets` and an
optional governed narration.

Tier 2 (LLM-authored sandboxed Cypher queries) is **not** implemented here; see
issue #3261.

## Loop Shape

```
[system + user messages]
        │
        ▼
  adapter.Complete()   ◄──────────────────────────────────────┐
        │                                                       │
        ▼                                                       │
  No tool calls? ──── yes ──► narrate? ──► Answer (Narrated)  │
        │                          └────► Answer (deterministic)
        │ tool calls (bounded by MaxToolCallsPerTurn)
        ▼
  runner.Run(tool, args)
        │
        ▼
  query.ResponseEnvelope
        │
        ▼
  NewAnswerPacket → []AnswerPacket (canonical truth)
        │
        ▼
  serialize packet → bounded JSON feedback message
        │
        └──────────────────────────────────────────────────────┘
             (next iteration, up to MaxIterations)
```

1. The engine sends a system instruction and the caller's question to the adapter.
2. Each completion turn either returns prose (final) or a list of tool calls.
3. Tool calls are dispatched through the `Runner` (up to `MaxToolCallsPerTurn`
   per turn).
4. Each `ResponseEnvelope` is wrapped into an `AnswerPacket` and fed back to the
   model as bounded JSON (capped at `maxToolResultBytes`).
5. The loop terminates on a tool-call-free completion, on an evidence-sufficiency
   stop (answer evidence held and a turn added no new distinct supported
   evidence), on the deterministic count route, or when `MaxIterations` is
   exhausted. Exhaustion marks the answer `Partial`. Every exit records a
   low-cardinality `Answer.TerminationReason`.
6. If narration posture is `Available`, the final prose is validated by
   `answernarration.Validate`; it becomes `Answer.Prose` only on a passing
   verdict. Failing validation drops prose and the answer falls back to
   deterministic packet summary.

When the primary packet carries a partial signal (`Partial`, `Truncated`, or
any limitation / missing-evidence / unsupported-reason entry) the narration
system prompt is partial-aware: `buildNarrationSystemPrompt` appends an
instruction to add one partial-signal sentence so the model can satisfy the
validator's partial-signal rule. Without this, narration of a partial packet
is deterministically rejected because the model is never told how to surface
the partial state.

## Injected Seams

| Seam | Type | Purpose |
|------|------|---------|
| `adapter` | `provider.Adapter` | LLM completion and tool-call generation |
| `runner` | `Runner` | In-process dispatch to the Eshu query surface |
| `tools` | `[]provider.Tool` | Tool definitions visible to the LLM |
| narration posture | `func() status.AnswerNarrationStatus` | Governs whether LLM narration is attempted |
| logger | `*slog.Logger` | Optional operator-facing structured logging (defaults to a discard logger) |

The `adapter`, `runner`, and `tools` seams are injected at construction via
`New`. The narration posture and logger are optional and injected after
construction via `SetNarrationPosture` and `SetLogger` before serving requests.
The engine holds no mutable session state; each `Ask` call owns its own
conversation thread.

## MCP-backed Runner

`NewMCPRunner(handler, authHeader, logger)` returns the production `Runner`. It
calls `mcp.RunReadOnlyTool` in-process: no network socket is opened.
`ContextWithCallerRequestCredentials` captures an immutable, filtered credential
snapshot from the accepted HTTP Ask request. Each inner call receives the same
Authorization header, Eshu session cookie(s), and CSRF header through a fresh
request adapter, then runs through the normal auth middleware again. Revocation,
route grants, and unsafe-method CSRF are checked for each tool call. A marked
request with no credentials never receives the runner's constructor token;
an authenticated context without matching request credentials fails closed.
The constructor token remains available only to an explicit legacy non-HTTP
runner invocation without an HTTP request marker or authenticated context.
Credentials and provider bodies are not included in runner diagnostics.

When `RunReadOnlyTool` returns `isError=true`, the envelope is returned rather
than converted to a Go error so the engine can wrap it in an `AnswerPacket` that
faithfully reflects the query surface's error verdict.

## Bounds

| Bound | Default | Config field |
|-------|---------|-------------|
| Max LLM rounds | 6 | `Options.MaxIterations` |
| Max tool calls per turn | 4 | `Options.MaxToolCallsPerTurn` |
| Tool-result feedback size | 4 096 B | `maxToolResultBytes` (const) |

`MaxIterations` and `MaxToolCallsPerTurn` are operator-tunable at the wiring
layer via `ESHU_ASK_MAX_ITERATIONS` and `ESHU_ASK_MAX_TOOL_CALLS_PER_TURN`
(see [HTTP API Reference](../../../../docs/public/reference/http-api.md) and the
`askwiring` package). The engine itself only enforces the resolved bound; it
does not read the environment.

## Invariants

- **Read-only**: the engine never writes to the graph, queue, or any store.
- **Leak-safe**: provider bodies, system prompts, and credentials are never
  exposed to callers. Tool-result feedback is a bounded packet serialisation, not
  raw provider output. Streaming never emits raw provider token deltas; token
  events carry validated narration prose only after `answernarration.Validate`
  accepts it.
- **Deterministic-canonical**: `AnswerPackets` are the authoritative answer truth
  regardless of narration. `Answer.Narrated` is true only when prose has passed
  the narration validator.
- **Bounded**: the iteration and tool-call limits are enforced unconditionally.

### Exact indexed-repository counts

An exact, unqualified indexed-repository count is a deterministic route. If the
provider selects an ecosystem overview or index-status tool for that sole
intent, the engine substitutes `list_indexed_repositories` with `limit=1` and
`offset=0`. It reads the authorized inventory's `total`, never the page
`count`, and publishes a deterministic packet summary that names
`list_indexed_repositories.total` as its evidence source. Missing, partial,
error, or internally inconsistent totals fail bounded instead of falling back
to provider prose. Qualified and compound questions stay on the provider's
selected route because a global inventory total cannot answer them exactly.
The packet embeds `{total}` and references the exact authorized inventory at
`eshu://api-result/repositories`; the ordinary JSON response and the SSE
`answer` event preserve that same `result_ref` and bounded result. This is an
aggregate-result reference, not a fabricated file/entity citation handle.
When a provider requests multiple tools in the same turn, packet and trace
order remain the actual dispatch order. The engine records an explicit primary
packet index for this deterministic intent so prose, truth, result, and evidence
all publish from the inventory packet even when another supported packet came
first.

### Bounded, useful answers (#5266)

An entity-oriented question can otherwise spend tens of seconds on poorly
bounded tool calls, exhaust the reasoning budget, and publish a circular
identity-only answer even when rich bounded evidence exists. Four deterministic
mechanisms keep such a session bounded and useful without raising any timeout,
response budget, or iteration count:

- **Pre-dispatch bounding** (`bounding.go`): a full-inventory list-all whose
  unbounded form blows the response budget (`list_indexed_repositories`) must
  carry a positive `limit`; an unbounded call is refused before dispatch with an
  executable narrowing hint, so the dispatch deadline and the response budget are
  never spent on a runaway list-all. The exact-count route bounds a bare
  inventory call to `limit=1` so it is never refused. Tools already bounded at
  the dispatch layer (`find_code`'s default limit; `list_relationship_edges`,
  bounded to 50 rows and forwarding only `verb`/`source_tool`/`limit`) are not
  pre-refused — a scope argument their route would silently drop is never treated
  as a bound; their runaway form is handled after dispatch by the continuation
  mechanism.
- **Oversized-result continuation** (`bounding.go`): when a dispatched tool
  returns an `mcp_response_over_budget` or `mcp_dispatch_timeout` envelope, the
  engine builds a bounded continuation packet — partial, with the narrowing
  guidance and a recommended bounded retry — instead of collapsing the outcome
  into an opaque unsupported packet.
- **Evidence-sufficiency termination** (`termination.go`): the loop tracks the
  distinct primary tools that have produced a supported, summary-bearing packet.
  Once answer evidence is held and a turn adds no new distinct supported
  evidence, the loop stops (`TerminationReason == evidence_sufficient`) rather
  than spinning to `MaxIterations`.
- **Relevance-ranked selection** (`packet_select.go`): the primary packet that
  backs deterministic prose, narration, and the handler's published answer is
  chosen by evidence tier, question relevance, truth strength, and completeness
  — not first-supported dispatch order. Selection is stable and dispatch-ordered
  on a tie; an explicitly bound `PrimaryPacketIndex` (the count route) always
  wins.

Every non-error exit records a low-cardinality `Answer.TerminationReason`
(`final_turn`, `evidence_sufficient`, `deterministic_route`, or
`max_iterations`). A circular, identity-only answer is withheld by the runtime
answer-substance guardrail in the query handler (shared by the JSON, SSE, and
MCP surfaces); the engine logs the usefulness verdict for operators.

New regression tests: `engine.TestAsk_RetainedEntityOverview_BoundedUsefulAnswer`,
`engine.TestBoundToolCall`, `engine.TestOversizedContinuationPacket`,
`engine.TestEvidenceProgress`, `engine.TestSelectPrimaryPacketIndex`,
`answerguardrail.TestIsCircularAnswer`,
`answerguardrail.TestValidateResultRejectsCircularAnswer`,
`query.TestApplyAskSubstanceGuardrailWithholdsCircularAnswer`, and
`answerquality.TestScoreRejectsCircularAnswer`.

Retained-stack proof on 2026-07-14 returned the same current authorized total,
`887`, through the HTTP API in `4.438 s` and the MCP server's shared Ask route
in `4.666 s`; the direct repository inventory also reported `total=887`. Both
responses named `list_indexed_repositories.total`, carried a deterministic
truth class, and recorded the same supported `list_indexed_repositories` query
trace. The canonical evidence-citation contract currently hydrates file/entity
handles only, so this aggregate result deliberately uses its addressable
canonical result plus packet truth provenance rather than fabricating an
unresolvable citation handle.
