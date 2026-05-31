# AGENTS.md - internal/collector/awscloud/services/resiliencehub guidance

## Read First

1. `README.md` - package purpose, resource/relationship surface, resource_id
   shapes, versioned-read behavior, and invariants.
2. `types.go` - scanner-owned Resilience Hub domain types and the `Client` port.
3. `scanner.go` - resource and relationship envelope orchestration.
4. `observations.go` - per-resource `aws_resource` observation builders.
5. `relationships.go` - relationship emission rules and join keys.
6. `helpers.go` - resource_id derivation, ARN-typed target mapping, and cloning.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Resilience Hub API access behind `Client`; do not import the AWS SDK into
  this package (the adapter lives in `awssdk/`).
- Metadata-only. Never read or persist assessment result bodies, drift detail,
  alarm/SOP/test recommendation contents, resolution status payloads, or any
  data-plane payload. Never call a mutation, resource-import, or assessment-start
  API.
- The app node publishes its resource_id as the app ARN (fallback to name). Key
  every app-sourced edge on that exact value.
- The policy node publishes its resource_id as the policy ARN (fallback to
  name). Key the app-uses-policy edge on the policy ARN.
- Components have no API ARN: key them by `<app-arn>/component/<name>`. Input
  sources prefer the source ARN, else `<app-arn>/input-source/<name>`.
- Assessment summaries carry the parent app ARN directly; key the
  assessment-for-app edge on that ARN.
- The app-protects-resource edge is emitted ONLY when Resilience Hub identifies
  the physical resource by an ARN AND that resource family's owning scanner is
  ARN-keyed (`mapProtectedResource` drops non-ARN identifiers;
  `protectedResourceTargetType` maps the ARN-keyed families). Resilience
  Hub-native identifiers are dropped, never keyed - the edge must never dangle.
- Never synthesize an ARN. Forward the ARNs AWS reports so GovCloud
  (`aws-us-gov`) and China (`aws-cn`) partitions are preserved.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or environment truth from app, policy, or resource names or tags.
- Keep ARNs, names, scores, and AWS error payloads out of metric labels.

## Common Changes

- Add a new protected-resource family edge only after verifying the owning
  scanner keys that family by the ARN Resilience Hub reports (open its
  `scanner.go` and read its `ResourceID`). Extend `protectedResourceTargetType`
  and add a focused test asserting the new edge joins.
- Add a new metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through the `awscloud`
  envelope builders. If the field can carry assessment-result or recommendation
  content, leave it out of the contract.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read assessment results, drift detail, recommendations, or resolution
  payloads, and do not call any mutation or assessment-start API.
- Do not synthesize ARNs or rewrite reported partitions.
- Do not resolve Resilience Hub names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
