# prod-ask-default-off — production validation

Validation-Slug: prod-ask-default-off
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: ask.natural_language_answer passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `ask.natural_language_answer` (tool `ask`).
Production profile: `required_runtime: deployed_services_plus_agent_reasoning_provider`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 15000`, `max_truth_level: derived`.

## Claim validated

Ask is default-off: the MCP tool and HTTP route return `unavailable` unless
`ESHU_ASK_ENABLED=true` and an `agent_reasoning` provider profile are configured. When enabled,
the engine plans bounded Tier-1 retrieval and returns evidence-backed answer packets
(`answer_prose`, `artifacts`, `truth_class`, `evidence_handles`, `query_trace`, `partial`,
`limitations`) without exposing provider prompts, raw provider bodies, or credentials.

## Committed reproducible evidence

**Default-off posture** — `go/cmd/mcp-server/ask_wiring_test.go`:
`TestMCPServerAskDefaultOffNoProfileConfigured` and
`TestMCPServerAskResponseBodyContainsUnavailableState` (proves the deployed MCP wiring returns
`unavailable` without a configured provider profile), and
`go/internal/query/ask_handler_test.go`: `TestAskHandler_Disabled` and
`TestAskHandler_DisabledNoEngineConstruction` (proves the HTTP route never constructs the engine
when disabled). Reproduce:

```bash
cd go && go test ./cmd/mcp-server -run TestMCPServerAsk -count=1
cd go && go test ./internal/query -run TestAskHandler_Disabled -count=1
```

**Evidence-backed response shape when enabled** — `go/internal/query/ask_handler_test.go`:
`TestBuildAskResponse_TruthClassFromPrimary`, `TestBuildAskResponse_LeakSafety` (no raw
provider prompt/response leakage), `TestBuildAskResponse_SuppressesUnsafeNarratedOutput`, and
`go/internal/query/ask_response_test.go`: `TestAskHandler_SuccessResponseShape` and
`TestAskHandler_PartialAnswer`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestBuildAskResponse|TestAskHandler_SuccessResponseShape|TestAskHandler_PartialAnswer" -count=1
```

**Engine failure handling** — `go/internal/query/ask_handler_test.go`:
`TestAskHandler_EngineError_Returns503`. Reproduce:

```bash
cd go && go test ./internal/query -run TestAskHandler_EngineError_Returns503 -count=1
```

**MCP tool registration** — `go/internal/mcp/tools_ask_test.go`: `TestAskToolIsRegistered` and
`TestResolveRouteMapsAsk`. Reproduce:

```bash
cd go && go test ./internal/mcp -run TestAskToolIsRegistered -count=1
```

## Notes

No private data: all cited tests use synthetic questions, fixture answer packets, and fake
provider stubs; no real provider credentials or prompts are committed.

Related: #5552 (burn-down).
