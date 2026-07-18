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
calls `mcp.RunReadOnlyTool` in-process: no network socket is opened. The caller's
scoped token is threaded via `authHeader` and `ctx`.

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

No-Regression Evidence (#5266 bounded useful answers): the failing-first
retained-shape regression `engine.TestAsk_RetainedEntityOverview_BoundedUsefulAnswer`
reproduces the reported run — the scripted model resolves the entity then issues
an unbounded list-all, an oversized service story, a broad timed-out search, and
redundant status calls, never emitting a final turn. Before the fix it ran the
full 6/6 iterations, dispatched all 8 calls (including the unbounded 256KB
inventory read), collapsed the oversized results into opaque unsupported
packets, and published the first-supported generic packet
(`{"indexed_repositories":42,...}`) with no `TerminationReason`. After the fix
the same script stops at 4 iterations (`TerminationReason == evidence_sufficient`),
refuses the unbounded `list_indexed_repositories` before dispatch (never spending
the response budget), converts the oversized service story and the timed-out
search into bounded continuation packets, and publishes the relevant
payments-service overview. Focused proof:

```bash
cd go && go test ./internal/ask/engine ./internal/answerguardrail \
  ./internal/answerquality ./internal/query -count=1
```

New regression tests: `engine.TestAsk_RetainedEntityOverview_BoundedUsefulAnswer`,
`engine.TestBoundToolCall`, `engine.TestOversizedContinuationPacket`,
`engine.TestEvidenceProgress`, `engine.TestSelectPrimaryPacketIndex`,
`answerguardrail.TestIsCircularAnswer`,
`answerguardrail.TestValidateResultRejectsCircularAnswer`,
`query.TestApplyAskSubstanceGuardrailWithholdsCircularAnswer`, and
`answerquality.TestScoreRejectsCircularAnswer`.

Performance Evidence (#5266): measured on the deterministic representative
harness above (LLM and backend abstracted through the `provider.Adapter` and
`Runner` seams, so iterations and dispatched tool calls are exact). Before →
after on the reproduced entity-overview scope: reasoning iterations 6 → 4,
dispatched tool calls 8 → 5 (the unbounded full-inventory read is no longer
dispatched), oversized/timeout results converted from 3 opaque unsupported
outcomes to 2 bounded continuations, termination reason none → `evidence_sufficient`,
and published answer generic-first-supported → relevant entity overview. The
change adds no timeout, response-budget, or iteration-count increase. The
remaining cost of a genuinely global (unscoped) search is inherent and now
yields a bounded continuation with a narrowing next action rather than an opaque
dead end; a live cold/warm wall-time rerun is the operator-local Ask proof
(`scripts/verify-ask-eshu-local-proof.sh --deepseek`).

Observability Evidence (#5266): the engine emits operator-facing structured logs
(no new metric labels, no hot path) through the injected `*slog.Logger`:
`ask: refused unbounded list/search call before dispatch` (field `tool`),
`ask: tool result runaway; bounded continuation offered` (fields `tool`,
`code`), `ask: evidence sufficiency stop` (fields `iteration`, `max_iterations`,
`packets`), `ask: primary packet selected` (fields `index`, `reason`,
`termination`), and `ask: usefulness verdict` (fields `circular`, `termination`).
These let an operator see the planner's bounding choice, budget rejections, the
packet-selection reason, the termination reason, and the answer usefulness
verdict without re-running the session. The change touches no Cypher, graph
write, worker, queue, lease, batch, Postgres write, or runtime stage.

No-Regression Evidence: #5246 exact-count routing, streaming parity, bounded
failures, hostile qualified questions, packet evidence, and rejection of an
unrelated provider-authored count pass with:

```bash
cd go && GOCACHE=/tmp/eshu-5246-gocache go test ./internal/ask/engine \
  -run 'Test(Ask.*IndexedRepository|IndexedRepositoryCount)' -count=1
```

Retained-stack proof on 2026-07-14 returned the same current authorized total,
`887`, through the HTTP API in `4.438 s` and the MCP server's shared Ask route
in `4.666 s`; the direct repository inventory also reported `total=887`. Both
responses named `list_indexed_repositories.total`, carried a deterministic
truth class, and recorded the same supported `list_indexed_repositories` query
trace. The canonical evidence-citation contract currently hydrates file/entity
handles only, so this aggregate result deliberately uses its addressable
canonical result plus packet truth provenance rather than fabricating an
unresolvable citation handle.

No-Observability-Change: #5246 keeps the existing Ask trace, packet limitations,
query trace events, HTTP/SSE events, and provider adapter logs. The trace names
the routed `list_indexed_repositories` tool. No metric, span name, log field,
runtime knob, queue, graph write, or Postgres write was added.

No-Regression Evidence: `go test ./internal/ask/engine -run '^TestAskStream'
-count=1` covers default-closed streams, rejected narration, validated
narration emission, tool-call events, and the no-streaming fallback. The
regression fixture proves an unsafe raw provider delta is not emitted before the
validated narration.

No-Observability-Change: the streaming safety change adds no worker, queue,
graph write, Postgres read, metric label, runtime knob, provider request, or
new span. Operators still diagnose Ask failures through the existing Ask engine
error path, query trace events, HTTP SSE events, and provider adapter logs.

No-Regression Evidence (#3356 budget + partial narration):
`go test ./internal/ask/engine ./internal/askwiring ./internal/answernarration
-count=1` (84 tests). New regression tests:
`engine.TestBuildNarrationSystemPromptPartialAware`,
`engine.TestNarratePartialPacketAccepted`, and
`askwiring.TestResolveEngineOptions*`. The two engine tests fail before the fix
with `narration rejected by validator` and pass after; the askwiring tests
prove default / override / invalid-fallback / ceiling-clamp behavior of the
budget knobs.

Observability Evidence (#3356): the engine now emits operator-facing structured
logs (no new metric labels, no hot path) through the injected `*slog.Logger`:
`ask: reached max reasoning iterations` (fields `max_iterations`,
`max_tool_calls_per_turn`, `packets`, `has_supported_evidence`),
`ask: tool calls truncated` (fields `requested`, `max_tool_calls_per_turn`,
`iteration`), `ask: narration rejected` (fields `reason`, `finding_reasons`,
`partial`, `truth_class`), and `ask: narration accepted` (fields `partial`,
`truth_class`). The wiring layer logs the resolved budget at startup
(`ask: engine budget resolved`) and clamps/invalid overrides at `WARN`. These
let an operator distinguish a too-tight budget from a narration format/binding
rejection without provider bodies leaking into logs. This change touches no
Cypher, graph write, worker, queue, lease, batch, or runtime stage, so
`scripts/verify-performance-evidence.sh` reports no hot files changed.
