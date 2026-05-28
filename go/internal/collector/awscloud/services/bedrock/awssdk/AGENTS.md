# AGENTS.md - internal/collector/awscloud/services/bedrock/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `exclusion_test.go` - the reflection gate proving every inference and every
   mutation is unreachable. Read it before touching either apiClient interface.
3. `client.go` - the two adapter read interfaces, `NewClient`, tags, telemetry.
4. `client_models.go` - the bedrock control-plane List and bounded Get reads.
5. `client_agents.go` - the bedrock-agent List and bounded Get reads.
6. `../scanner.go` - scanner-owned Bedrock fact selection.
7. `../README.md` - Bedrock scanner contract.
8. `../../../README.md` - AWS cloud envelope contract.

## Invariants

- Keep Bedrock SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- The `bedrockAPIClient` and `bedrockAgentAPIClient` interfaces hold only
  `List*`, `Get*`, and `ListTagsForResource`. Never add an inference call
  (InvokeModel, Converse, InvokeAgent, Retrieve, RetrieveAndGenerate) or any
  mutation. Never import `bedrockruntime` or `bedrockagentruntime`.
- Wrap each AWS paginator page or point read in `recordAPICall` (via `page`).
- Keep metric labels bounded to service, account, region, operation, and result.
- `Get*` reads copy only relationship- and reference-bearing fields. Never copy
  `Agent.Instruction`, `Agent.PromptOverrideConfiguration`, guardrail policy
  bodies, `AgentActionGroup.ApiSchema` / `FunctionSchema`, knowledge base
  document content, custom-model `HyperParameters`, or `TrainingDataConfig`
  references. The scanner-owned types have no field for those values.
- Never call `GetGuardrail`, `GetKnowledgeBaseDocuments`, or
  `ListKnowledgeBaseDocuments`: those are the operations that return policy
  bodies and ingested content.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Bedrock metadata read by extending `bedrock.Client`, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. Feed the forbidden SDK fields in the adapter test to
  prove they are dropped.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Keep new Get fanout bounded and update the README performance note.

## What Not To Change Without An ADR

- Do not call any inference API or any mutation API.
- Do not import bedrockruntime or bedrockagentruntime.
- Do not read or persist any forbidden payload (agent prompts, guardrail
  policies, KB document content, action-group schemas, hyperparameters,
  training data).
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
