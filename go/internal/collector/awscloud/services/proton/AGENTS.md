# AGENTS.md - internal/collector/awscloud/services/proton guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Proton domain types.
3. `scanner.go` - environment, service, template resource emission and the
   service-in-environment placement derivation.
4. `observations.go` - resource observation builders (the metadata that is
   persisted; bodies are excluded here).
5. `relationships.go` - relationship emission rules and join keys.
6. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Proton API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read or persist service/environment spec manifest bodies (the
  `GetService` `Spec` field is intentionally never mapped), pipeline spec bodies,
  template version schema bodies, or deployment/service-instance input parameter
  values. From service instances, keep only the service-name/environment-name
  join keys.
- The environment node publishes its resource_id as the environment ARN
  (fallback to name). Key the service-in-environment edge on that exact value so
  it joins the environment node.
- Emit the environment-uses-role edge only when AWS reports a `ProtonServiceRoleArn`.
  Set `target_arn` only when the identifier is ARN-shaped, matching the IAM
  scanner's published role resource_id; a non-ARN value emits no edge.
- Skip, never dangle: a service placement that names an environment the scanner
  did not observe emits no edge.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from Proton names or AWS tags;
  correlation belongs in reducers.
- Keep Proton ARNs, names, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new Proton metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry a spec, schema, or input parameter
  body, leave it out of the scanner contract.
- Add new relationship evidence only when a Proton read API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (the role ARN for IAM roles, the environment ARN for the
  parent environment).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read spec/schema bodies, deployment outputs, provisioned resources, or
  service-instance input parameters, and do not call any Proton mutation API.
- Do not resolve Proton names or tags into workload ownership here.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
