# AGENTS.md - internal/collector/awscloud/services/opensearchserverless guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned OpenSearch Serverless domain types.
3. `scanner.go` - collection, security-policy, and VPC-endpoint resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, encryption-policy pattern matching, and
   scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep OpenSearch Serverless API access behind `Client`; do not import the AWS
  SDK into this package.
- Never reach the OpenSearch HTTP data plane (index, search, bulk, document
  APIs). Never persist access-policy or security-policy document bodies,
  collection or dashboard endpoints, or saved-object bodies. Never call any
  `Create*`, `Update*`, `Delete*` mutation API.
- The collection node publishes its resource_id as the collection ARN (fallback
  to id then name). Source the collection-to-KMS edge on that value.
- Derive the collection-to-KMS edge from the matching encryption policy. The
  adapter parses `KmsARN` and collection patterns out of the policy body and
  discards the body; this package matches by name with "most specific rule wins"
  precedence. AWS-owned-key policies emit no edge. Set `target_arn` only for
  ARN-shaped key identifiers, matching the KMS scanner's published resource_id.
- The managed VPC endpoint reports VPC, subnet, and security-group references as
  bare EC2 ids. Key the endpoint-to-VPC/subnet/security-group edges on those bare
  ids, matching the EC2 scanner's published resource_ids. Never synthesize an ARN
  for these targets.
- A security policy node publishes its resource_id as the type-qualified name so
  encryption and network policies that share a name stay distinct.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from collection, policy, or
  endpoint names, or AWS tags.
- Keep collection ARNs, names, policy bodies, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new OpenSearch Serverless metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry data-plane content or a
  policy document body, leave it out of the scanner contract.
- Add new relationship evidence only when the OpenSearch Serverless API reports
  both sides directly and the target identity matches an existing scanner's
  published resource_id shape (ARN-equality for KMS keys, bare ids for VPC,
  subnet, and security group).
- Extend SDK pagination and policy-body parsing in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not reach the OpenSearch HTTP data plane, persist policy bodies, persist
  endpoints, or call any Serverless mutation API.
- Do not resolve OpenSearch Serverless names or tags into workload ownership
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
