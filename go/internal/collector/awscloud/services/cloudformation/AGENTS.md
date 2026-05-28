# AGENTS.md - internal/collector/awscloud/services/cloudformation guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CloudFormation domain types and the `Client`
   security boundary.
3. `scanner.go` - stack, stack-set, change-set, drift, instance, and type
   emission.
4. `observations.go` - resource and relationship observation builders, output
   redaction.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `../../redaction.go` - `ClassifyStackOutput` and the AWS sensitive-key policy.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and data boundaries.

## Invariants

- Keep CloudFormation API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read a template body (`GetTemplate`, `GetTemplateSummary`), never read
  parameter values, never read change-set bodies (`DescribeChangeSet`), never
  persist drift property documents, and never call any mutation API. The
  forbidden surface is enforced by
  `TestClientInterfaceExcludesMutationAndTemplateAPIs`; keep that list current.
- Redact every stack output value whose key is secret-like through
  `awscloud.ClassifyStackOutput`. Never carry a raw output value into a fact
  payload without classifying it first.
- `Scanner` requires a non-zero `RedactionKey`. The runtimebind builder returns a
  typed error when the key is zero.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from stack names or tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep ARNs, names, tags, and output values out of metric labels.

## Common Changes

- Add a new metadata field by extending the relevant scanner type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when the CloudFormation API reports both
  sides directly without reading a template body.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or persist template bodies, parameter values, change-set bodies,
  stack policies, or drift property documents.
- Do not resolve stack names, tags, or relationships into workload ownership
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
