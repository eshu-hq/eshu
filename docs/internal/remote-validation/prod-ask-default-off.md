# prod-ask-default-off — production validation

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
