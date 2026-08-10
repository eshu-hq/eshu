# prod-ask-default-off — production validation

Validation-Slug: prod-ask-default-off
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu5552-ask-aggregate-20260809c ESHU_POSTGRES_PORT=25432 NORNICDB_BOLT_PORT=27687 NORNICDB_HTTP_PORT=27474 GATE_API_PORT=28080 GATE_MCP_PORT=28091 GATE_PROMETHEUS_SOURCE_PORT=29090 GATE_ASK_PROVIDER_PORT=29191 bash scripts/verify-golden-corpus-gate.sh > /tmp/eshu-5552-b7-ask-aggregate-20260809c.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: ask.natural_language_answer returned non-empty answer prose and four evidence handles after the deployed engine completed one supported, bounded investigate_code_topic call.
B12-Assertion: ask.natural_language_answer -> mcp:ask

## Fresh deployed validation

The fresh credential-free B-7 run completed with 550 passes, zero required
failures, and zero warnings in 121 seconds. The positive `mcp:ask` assertion
observed four source evidence handles, non-empty answer prose, and the exact
supported `investigate_code_topic` trace bounded to `limit: 10`. The same run
closed every required drain at zero residual and zero dead-letter work.

Capability: `ask.natural_language_answer` (tool `ask`).
Production profile: `required_runtime: deployed_services_plus_agent_reasoning_provider`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 15000`, `max_truth_level: derived`.

## Claim under validation

Ask remains default-off as an operational posture. The supported production
claim is distinct: when enabled with an `agent_reasoning` profile, the deployed
engine must make the bounded `investigate_code_topic` call and publish a
non-empty answer, evidence handles, and query trace. The positive B-12 shape now
enforces that behavior.

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

**Local response-contract coverage when enabled** — `go/internal/query/ask_handler_test.go`:
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

No private data: the deployed fixture uses only the public golden corpus and a
credential-free local OpenAI-compatible endpoint. No provider credential,
private prompt, hostname, or machine-specific path is committed.

Related: #5552 (burn-down).
