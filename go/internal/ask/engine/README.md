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
5. The loop terminates on a tool-call-free completion or when `MaxIterations` is
   exhausted. Exhaustion marks the answer `Partial`.
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

No-Regression Evidence: #5246 exact-count routing, streaming parity, bounded
failures, hostile qualified questions, packet evidence, and rejection of an
unrelated provider-authored count pass with:

```bash
cd go && GOCACHE=/tmp/eshu-5246-gocache go test ./internal/ask/engine \
  -run 'Test(Ask.*IndexedRepository|IndexedRepositoryCount)' -count=1
```

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
