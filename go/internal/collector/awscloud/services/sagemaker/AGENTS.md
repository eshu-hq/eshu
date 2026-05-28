# AGENTS.md - internal/collector/awscloud/services/sagemaker guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` and `types_studio.go` - scanner-owned SageMaker domain types.
3. `scanner.go` - scan orchestration and the per-resource stage table.
4. `observations.go` / `observations_studio.go` - resource fact builders.
5. `relationships.go` - relationship fact builders.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and the SageMaker data boundary.

## Invariants

- Keep SageMaker API access behind `Client`; do not import the AWS SDK into
  this package.
- Never invoke endpoints. There is no InvokeEndpoint / InvokeEndpointAsync path
  and there must never be one.
- Never add a scanner-owned field for a forbidden payload: hyperparameter
  values (training or tuning), training/processing/transform input or output
  data references, notebook lifecycle-config script bodies, container
  environment maps, or pipeline definition bodies. Exclusion is by omission, so
  the secret has no field to land in.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from resource names or tags.
- Guard IAM-role and S3-artifact relationship targets so a free-form name is
  never emitted as an ARN. Emit a relationship only when AWS reports both ends.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep ARNs, names, tags, and image URIs out of metric labels.

## Common Changes

- Add a SageMaker metadata field by extending the matching scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through the
  `awscloud` envelope builders.
- Add new relationship evidence only when the SageMaker API reports both sides
  directly, with a guarded target.
- Extend SDK pagination or Describe fanout in the `awssdk` adapter, not here.
  Keep new Describe fanout bounded and recorded in the README performance note.

## What Not To Change Without An ADR

- Do not invoke endpoints or run inference.
- Do not call or persist any forbidden payload listed under Invariants.
- Do not resolve names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
