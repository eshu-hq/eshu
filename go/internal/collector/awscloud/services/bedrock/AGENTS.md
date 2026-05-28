# AGENTS.md - internal/collector/awscloud/services/bedrock guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Bedrock domain types and the `Client` interface.
3. `scanner.go` - scan orchestration and the per-resource stage table.
4. `observations.go` - resource fact builders.
5. `relationships.go` - relationship fact builders and S3-URL/partition parsing.
6. `redaction_test.go` - the struct-reflection gate proving no scanner type can
   carry a forbidden payload. Read it before adding any field.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and the Bedrock data boundary.

## Invariants

- Keep Bedrock API access behind `Client`; do not import the AWS SDK into this
  package.
- This is a high-redaction scanner. Never add a scanner-owned field for a
  forbidden payload: agent instructions (system prompts), prompt-override
  configurations, guardrail topic or content policy bodies, knowledge base
  ingested document content or chunks, action-group API schema bodies,
  custom-model hyperparameter values, or training input data. Exclusion is by
  omission, so the IP or sensitive value has no field to land in. The
  reflection gate in `redaction_test.go` fails the build if a forbidden field
  name appears.
- Never invoke a model or query an agent/knowledge base. The bedrock-runtime
  (InvokeModel, Converse) and bedrock-agent-runtime (InvokeAgent, Retrieve,
  RetrieveAndGenerate) modules are out of scope and must never be imported.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from resource names or tags.
- Every relationship must carry a non-empty `target_type` and a
  `target_resource_id` that matches the target scanner's resource id. Synthesize
  S3 bucket ARNs with the partition taken from the source ARN; never hardcode
  `arn:aws:`.
- Guard ARN-shaped relationship targets so a free-form name is never emitted as
  an ARN. Emit a relationship only when AWS reports both ends.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep ARNs, names, tags, and URLs out of metric labels.

## Common Changes

- Add a Bedrock metadata field by extending the matching scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through the
  `awscloud` envelope builders. Confirm the new field name is not a forbidden
  token (the redaction gate enforces this).
- Add new relationship evidence only when the Bedrock API reports both sides
  directly, with a guarded, typed target.
- Extend SDK pagination or Get fanout in the `awssdk` adapter, not here. Keep new
  Get fanout bounded and recorded in the README performance note.

## What Not To Change Without An ADR

- Do not invoke a model, query an agent, or query a knowledge base.
- Do not import bedrock-runtime or bedrock-agent-runtime.
- Do not call or persist any forbidden payload listed under Invariants.
- Do not resolve names or tags into workload ownership here; correlation belongs
  in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
