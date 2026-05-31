# AGENTS.md - internal/collector/awscloud/services/vpclattice guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned VPC Lattice domain types.
3. `scanner.go` - service network, service, target group, and listener resource
   and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep VPC Lattice API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist auth-policy bodies, resource-policy bodies, or any
  data-plane payload. Never call `GetAuthPolicy`, `GetResourcePolicy`, or any
  `Create*`, `Update*`, `Delete*`, `Put*`, `Register*`, or `Deregister*`
  mutation API.
- Service network, service, target group, and listener nodes publish their
  resource_id as the ARN (fallback to the id). Key each edge on that exact value
  so it joins the published node.
- The service-network-to-VPC and target-group-to-VPC edges target `aws_ec2_vpc`
  by the bare `vpc-` id and never carry a `target_arn`.
- The service-network-to-service, target-group-to-service, and
  listener-in-service edges target `aws_vpclattice_service` by the service ARN.
- Emit the service-to-ACM-certificate edge only when `GetService` reports a
  certificate ARN; target `aws_acm_certificate` by that ARN.
- Emit a target-to-resource edge only when the registered target id resolves to
  the shape the target group type implies (LAMBDA → function ARN, INSTANCE →
  bare `i-` id, ALB → load balancer ARN). Skip IP targets and any id that does
  not match; never key a dangling edge.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant or the documented `aws_ec2_instance`
  forward-reference allowlist anchor, and a `target_resource_id` matching how
  the target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from network, service,
  target group, or listener names, or AWS tags.
- Preserve stable identities across repeated observations in the same AWS
  generation.
- Keep VPC Lattice ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new VPC Lattice metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a policy body or
  data-plane payload, leave it out of the scanner contract.
- Add new relationship evidence only when the VPC Lattice API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (the bare `vpc-` id for VPCs, the service ARN for VPC
  Lattice services, the function/load-balancer ARN for Lambda/ALB targets, the
  bare `i-` id for instances, the certificate ARN for ACM).
- Extend SDK pagination and per-resource Get enrichment in the `awssdk` adapter,
  not here.

## What Not To Change Without An ADR

- Do not read auth-policy bodies, resource-policy bodies, or any data-plane
  payload, and do not call any VPC Lattice mutation API.
- Do not resolve VPC Lattice names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
