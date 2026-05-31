# AGENTS.md - internal/collector/awscloud/services/controltower guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Control Tower domain types.
3. `scanner.go` - landing-zone, enabled-control, and enabled-baseline resource
   and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - Organizations target ARN parsing and scanner-side cloning
   helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `../organizations/scanner.go` - how the organizations scanner publishes OU,
   account, and root resource_ids (bare id preferred), which every cross-service
   edge here must key against.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Control Tower API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist the landing-zone manifest JSON body, control parameter
  values, baseline parameter values, or any governance payload. Never call an
  enable, disable, reset, create, update, or delete API.
- The landing-zone, enabled-control, and enabled-baseline nodes publish their
  resource_id as their own ARN. Source each node's edges on that ARN.
- Control Tower reports a governed target as an Organizations ARN. The
  organizations scanner keys OU/account/root nodes by their **bare id**, so the
  control-governs-target and baseline-governs-target edges parse the bare id
  from the ARN and key the target by it. Do NOT set `target_arn` on these edges
  (it would mark the edge ARN-keyed and break the bare-id join); keep the ARN as
  a `target_arn` attribute for provenance only.
- A management account governs at most one landing zone. Key the
  baseline-for-landing-zone edge on that single landing-zone ARN.
- Skip an edge (return nil) when the target ARN is missing, is not an
  Organizations ARN, or names a family the organizations scanner does not
  publish. Never key a dangling edge.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Trim whitespace on any string used as an id or service_kind. Canonicalize the
  boundary service_kind on the trimmed value in `Scan` and write the canonical
  constant back.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from Control Tower
  identifiers or AWS tags.

## Common Changes

- Add a new Control Tower metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a manifest, parameter, or
  governance payload value, leave it out of the scanner contract.
- Add new relationship evidence only when the Control Tower API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (bare Organizations id for OU/account/root, the landing-zone
  ARN for the internal edge).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read the landing-zone manifest, control parameters, baseline
  parameters, operation results, or any governance payload, and do not call any
  Control Tower mutation API.
- Do not resolve Control Tower identifiers or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
