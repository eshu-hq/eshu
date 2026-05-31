# AGENTS.md - internal/collector/awscloud/services/verifiedpermissions guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Verified Permissions domain types.
3. `scanner.go` - policy store, policy, and identity source resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, Cognito user pool id extraction, and
   scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Verified Permissions API access behind `Client`; do not import the AWS
  SDK into this package.
- Never read or persist Cedar policy statement bodies, schema bodies, or policy
  template bodies. Never call `GetPolicy`, `GetSchema`, `GetPolicyTemplate`,
  `IsAuthorized`, `BatchIsAuthorized`, or any `Create*`, `Update*`, `Delete*`,
  `Put*` mutation API. Policies are emitted as id, type, and effect only.
- The policy store node publishes its resource_id as the policy store ARN
  (fallback to the policy store id). Key the policy-in-store and
  identity-source-in-store edges on that exact value so they join the store node.
- Policies and identity sources have no API ARN. Their node resource_id is the
  qualified `<policy-store-id>/<id>`; source each entity's own edge on that same
  value.
- Emit the identity-source-to-Cognito-user-pool edge only when AWS reports a
  user pool ARN AND the bare user pool id can be parsed out of it. The Cognito
  scanner publishes a user pool node's resource_id as the BARE user pool id, so
  key the target on the parsed id and leave `target_arn` empty (relguard rejects
  a bare target_resource_id paired with an ARN-shaped target_arn). A malformed
  ARN skips the edge rather than dangling it.
- Record the encryption configuration as a non-secret label (`DEFAULT` / `KMS`)
  only. Never persist the customer-managed KMS key ARN or the user-defined
  encryption context. Never persist application client id values; record only
  their count.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from policy store, policy, or
  identity source ids, or AWS tags.
- Keep Verified Permissions ARNs, ids, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new Verified Permissions metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a Cedar body, schema
  body, template body, or client secret, leave it out of the scanner contract.
- Add new relationship evidence only when the Verified Permissions API reports
  both sides directly and the target identity matches an existing scanner's
  published resource_id shape (the bare user pool id for Cognito, the store ARN
  for the parent store).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read Cedar policy statement bodies, schema bodies, or policy template
  bodies, evaluate authorization requests, or call any Verified Permissions
  mutation API.
- Do not resolve Verified Permissions ids or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
