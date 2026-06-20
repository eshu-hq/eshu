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

## Injected Seams

| Seam | Type | Purpose |
|------|------|---------|
| `adapter` | `provider.Adapter` | LLM completion and tool-call generation |
| `runner` | `Runner` | In-process dispatch to the Eshu query surface |
| `tools` | `[]provider.Tool` | Tool definitions visible to the LLM |
| narration posture | `func() status.AnswerNarrationStatus` | Governs whether LLM narration is attempted |

All seams are injected at construction time via `New`. The engine holds no
mutable session state; each `Ask` call owns its own conversation thread.

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

## Invariants

- **Read-only**: the engine never writes to the graph, queue, or any store.
- **Leak-safe**: provider bodies, system prompts, and credentials are never
  exposed to callers. Tool-result feedback is a bounded packet serialisation, not
  raw provider output.
- **Deterministic-canonical**: `AnswerPackets` are the authoritative answer truth
  regardless of narration. `Answer.Narrated` is true only when prose has passed
  the narration validator.
- **Bounded**: the iteration and tool-call limits are enforced unconditionally.
