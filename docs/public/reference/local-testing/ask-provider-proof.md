# Ask Eshu Provider Proof

Use this checklist before claiming Ask Eshu provider-backed local proof is
complete. It is intentionally redacted and local-first: committed evidence may
name provider kinds, model ids, route names, truth classes, and test commands,
but it must not include provider credentials, private endpoints, raw addresses,
private repository paths, prompts containing sensitive source excerpts, or
provider response bodies.

The durable local proof uses a fake OpenAI-compatible endpoint wired through an
Ask Eshu `deepseek` semantic provider profile. That proves the same DeepSeek
adapter path, credential-handle lookup, JSON Ask response, SSE Ask response,
governed narration, evidence handles, and publish-safety gate without requiring
external network access or storing operator-local configuration.

## Checklist

1. Prove Ask Eshu remains disabled by default and when no `agent_reasoning`
   provider profile is configured.
2. Prove a DeepSeek-compatible `agent_reasoning` profile can be selected through
   `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` using an environment-variable handle,
   not a committed credential value.
3. Prove `/api/v0/ask` JSON returns governed narration, deterministic truth,
   evidence handles, and `partial:false`.
4. Prove `/api/v0/ask` SSE emits only validated narration token events and a
   final governed answer event. Raw provider final text must not be streamed or
   echoed.
5. Prove `/api/v0/status/answer-narration` and the MCP
   `get_answer_narration_status` tool report the same redacted posture.
6. Prove missing provider, bad provider profile, disabled narration, scoped
   caller routing, and publish-safety failures stay fail-closed.
7. Run the answer-quality publish-safety scorecard gate for any captured
   redacted answer artifact. Do not mark hosted parity complete from local-only
   evidence.

## Local Provider Gate

Run the focused Ask wiring proof first:

```bash
cd go
go test ./internal/askwiring -run TestBuildAskHandlerProviderBackedJSONAndSSE -count=1
```

This test configures a local `deepseek` profile with:

- `source_classes:["agent_reasoning"]`
- `source_policy_configured:true`
- `model_id:"deepseek-chat"`
- an environment-variable credential handle
- a fake in-process endpoint supplied as `endpoint_profile_id`

Passing output proves that the runtime uses the configured provider adapter,
dispatches the bounded repository tool, accepts only governed narration,
preserves citation-backed evidence handles, and verifies both JSON and SSE
response paths.

## Negative And Status Gates

Run the focused fail-closed and status gates:

```bash
cd go
go test ./internal/askwiring ./cmd/mcp-server -run 'TestBuildAskHandler|TestMCPServerAsk' -count=1
go test ./internal/query -run 'TestAskSSE|TestBuildAskResponse|TestStatusHandlerAnswerNarration|TestAuthMiddlewareWithScopedTokensAllowsAnswerNarrationStatusRoute|TestOpenAPIAsk' -count=1
go test ./internal/mcp -run 'TestAnswerNarrationRuntimeToolRoutesToStatus|TestDispatchToolAnswerNarrationStatusAllowsScopedRoute' -count=1
go test ./internal/ask/engine -run TestMCPRunner_ScopedCaller_CannotReachNonScopedRoute -count=1
go test ./internal/answerguardrail ./internal/answerquality -count=1
go test ./cmd/eshu -run TestAnswerQualityScorecardCommand -count=1
```

These gates cover default-off behavior, no-profile behavior, MCP-server route
wiring, JSON and SSE safety, redacted status surfaces, scoped caller routing,
shared publish-safety guardrails, and the offline answer-quality scorecard.

## Optional Operator-Local Live Provider Proof

An operator may repeat the proof against a real DeepSeek-compatible endpoint in
their own shell by setting `ESHU_ASK_ENABLED=true`,
`ESHU_ASK_NARRATION_ENABLED=true`, and a provider profile whose
`credential_source.handle` names an environment variable that exists only in
that shell. Keep the credential value, endpoint, raw request, raw response, and
provider traces out of issues, commits, docs, and logs.

For public closeout, record only:

- the Eshu commit tested;
- the command names run;
- redacted pass/fail status for JSON, SSE, status, scoped caller, and
  publish-safety gates;
- whether the live-provider step was executed or intentionally skipped because
  no operator-local credential was available.

If the live-provider step is skipped, the local provider gate above is still the
committable regression proof for the runtime path. It must not be described as
external provider reachability or hosted parity.
