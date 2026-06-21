# Agent Instructions: go/internal/ask/engine

## Read-First Order

Before editing any file in this package, read in this order:

1. `doc.go` — package contract and Tier 1 scope declaration.
2. `engine.go` — `Runner` interface, `Engine` struct, `Options`, `Answer`, `TraceEntry`.
3. `loop.go` — `Ask` bounded iteration loop, tool dispatch, packet assembly,
   bounded feedback serialisation.
4. `narration.go` — governed narration step, posture resolution, validator gate.
5. `toolset.go` — `Toolset` helper that maps the catalog + MCP definitions to
   `[]provider.Tool`.
6. `mcprunner.go` — `mcpRunner`, `NewMCPRunner`, in-process dispatch via
   `mcp.RunReadOnlyTool`.

Read the linked `README.md` for architecture context and bounds before any
structural change.

## Invariants (enforce continuously)

1. **Read-only.** The engine never writes to any store, queue, or graph. If a
   tool call touches a mutation route, the `mcprunner` will never route to it
   (only read routes are reachable via `mcp.RunReadOnlyTool`). Do not add write
   paths.

2. **Deterministic packet is canonical.** `Answer.Packets` is the authoritative
   truth. `Answer.Prose` is an optional validated view. Never present prose as a
   substitute for packets. Never set `Answer.Narrated = true` without a passing
   `answernarration.Validate` verdict.

3. **Narration is gated.** Prose enters `Answer.Prose` only when:
   - narration posture is `Available`, AND
   - `answernarration.Validate` returns `nil`.
   A failing validation must drop prose silently and fall back to deterministic
   packet summary. Do not bypass the validator. The narration prompt MUST stay
   in sync with the validator: when the packet carries a partial signal,
   `buildNarrationSystemPrompt` instructs the model to surface it, because the
   validator rejects partial packets narrated as complete. Keep
   `packetHasPartialSignal` here aligned with the validator's rule. Do not "fix"
   a partial-narration rejection by loosening the validator — fix the prompt.

4. **Bounds enforced unconditionally.** `MaxIterations` and
   `MaxToolCallsPerTurn` must be checked before every iteration and every
   batch of tool calls respectively. Do not add code that skips or extends
   these limits without updating `Options` and its documentation.

5. **Leak-safe.** Never expose provider request/response bodies, system prompts,
   or credentials to callers. Tool-result feedback to the LLM is a bounded JSON
   serialisation of an `AnswerPacket` (truth-labeled), capped at
   `maxToolResultBytes`. Raw `ResponseEnvelope.Data` must never be fed back
   directly.

6. **`isError` envelope preserved.** When `mcp.RunReadOnlyTool` returns
   `isError=true`, `mcpRunner.Run` returns the envelope (not an error). This
   allows the engine to build a proper `AnswerPacket` reflecting the query
   surface's error verdict. Do not convert `isError` envelopes to Go errors.

## How to Add a New Seam

1. Add the field to `Engine` and the parameter to `New`.
2. Add a sentinel error (`ErrNil<SeamName>`) returned by `New` when the seam is
   nil (if it is required).
3. Document the field with a Go doc comment.
4. Add a failing test for the nil-seam error path before implementing.
5. Update `README.md` seams table.

## How to Change a Bound

1. Add or update the field in `Options` with a Go doc comment and a zero-default
   sentinel.
2. Update `applyDefaults` to fill the zero value.
3. Update `DefaultOptions` if the default value changes.
4. Update `README.md` bounds table.
5. Write or extend a test that exercises the new bound.

## Anti-Patterns

- **No real network in tests.** Use in-process `http.Handler` mocks as in
  `mcprunner_test.go` and `run_readonly_test.go`. Never open a TCP socket or
  start an `httptest.Server` that listens externally.
- **Never fabricate prose.** The engine must not construct its own prose outside
  the governed narration path. Deterministic summaries derived from packet
  content are acceptable; LLM-style text invented in Go is not.
- **Never set `Narrated=true` without a passing Validate verdict.** A bypassed
  validator is a correctness violation, not a convenience.
- **Do not feed raw envelope `Data` to the LLM.** Serialise the `AnswerPacket`
  (which is truth-labeled and size-bounded) as the tool-result message.
- **Do not add mutation tool wiring here.** Mutation belongs to a separate
  surface. If you find yourself reaching for a write route, stop and ask.
