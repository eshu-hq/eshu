# askwiring

Shared engine-construction and narration-posture wiring for the Ask Eshu
feature. Both `cmd/api` and `cmd/mcp-server` import this package so the
default-off semantics and engine lifecycle are implemented once.

## Responsibilities

- `BuildAskHandler` — constructs the [query.AskHandler] with a live engine
  when `ESHU_ASK_ENABLED=true` and a valid `agent_reasoning` provider profile
  is present; returns a default-off handler (nil Asker → 503 unavailable)
  otherwise.
- `BuildNarrationPosture` — derives the governed narration-posture closure from
  `ESHU_ASK_ENABLED`, `ESHU_ASK_NARRATION_ENABLED`, and adapter readiness.
- `ResolveEngineOptions` — derives the engine budget (`MaxIterations`,
  `MaxToolCallsPerTurn`) from `ESHU_ASK_MAX_ITERATIONS` and
  `ESHU_ASK_MAX_TOOL_CALLS_PER_TURN`, starting from the engine defaults and
  clamping to documented ceilings. Invalid or out-of-range values keep the
  default; clamps and invalid values are logged at `WARN`.
- Helper predicates (`IsAskEnabled`, `IsNarrationEnabled`,
  `ResolveAgentReasoningProfile`) that both callers need.

## Tunable agent loop budget

`ESHU_ASK_MAX_ITERATIONS` (default 6, ceiling 32) and
`ESHU_ASK_MAX_TOOL_CALLS_PER_TURN` (default 4, ceiling 16) let operators widen
the loop budget for weaker providers without removing the hard safety cap. The
resolved budget is logged at startup (`ask: engine budget resolved`). See
[HTTP API Reference](../../../docs/public/reference/http-api.md#agent-loop-budget-tunable)
for the full contract.

## Default-off contract

A nil Asker on the returned `HandlerResult.Handler` means the feature is
disabled. Callers MUST NOT mount a non-nil Asker unless `AdapterReady()` is
true. The `SetPosture` field is always a valid func (no-op when off).

## Wiring order (callers must follow)

1. Build the mux / HTTP handler that will serve as the in-process runner
   target.
2. Call `BuildAskHandler(getenv, mux, apiKey, logger)`.
3. Call `BuildNarrationPosture(getenv, result.AdapterReady())`.
4. Call `result.SetPosture(posture)`.
5. Call `result.Handler.Mount(mux)`.
6. Inject `posture` into the status handler's `NarrationPosture` field.
