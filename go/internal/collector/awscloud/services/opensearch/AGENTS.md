# AGENTS.md - internal/collector/awscloud/services/opensearch guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned OpenSearch domain types.
3. `scanner.go` - domain, package, collection, security config, and VPC
   endpoint fact emission.
4. `relationships.go` - domain, package, and collection relationship emission.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep OpenSearch API access behind `Client`; do not import the AWS SDK into
  this package.
- Never call CreateDomain, DeleteDomain, UpdateDomainConfig, CreateCollection,
  DeleteCollection, CreatePackage, DeletePackage, AssociatePackage,
  DissociatePackage, AcceptInboundConnection, or any other mutation API.
- Never reach the OpenSearch HTTP API (`_search`, `_msearch`, `_index`,
  `_doc`, `_bulk`, and similar). It is reachable only over the domain HTTP
  endpoint, which this package never constructs.
- Never persist master user passwords, domain endpoint contents, the
  `Endpoints` map, the access policy body, custom package bodies, or
  serverless saved-object bodies. `Domain` carries no password-shaped field.
- Every relationship must set a non-empty `TargetType` matching the target
  scanner's resource_id shape: `aws_ec2_vpc`, `aws_ec2_subnet`,
  `aws_ec2_security_group`, `aws_kms_key`, `aws_iam_role`,
  `aws_opensearch_domain`, and `aws_opensearch_serverless_vpc_endpoint`.
- Treat a KMS identifier as an ARN target only when AWS reports it in ARN
  shape. Never synthesize the aws partition.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from domain names, collection names,
  package names, or tags.
- Preserve stable identities (ARN preferred, then name/id) across repeated
  observations in the same AWS generation.
- Keep ARNs, domain names, package IDs, collection IDs, and tags out of metric
  labels.

## Common Changes

- Add a new OpenSearch metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Reject additions that would expose master
  passwords, endpoint contents, access policy bodies, package bodies, or
  saved-object bodies.
- Add new relationship evidence only when the OpenSearch API reports both
  sides directly and the target identity is not sensitive.
- Extend SDK pagination, batching, and access-policy role-ARN resolution in the
  `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not create, delete, or modify domains, collections, packages, security
  configs, or VPC endpoints.
- Do not call the OpenSearch HTTP index/search/data API.
- Do not resolve domain or collection names into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not widen `Domain` to carry password, secret, token, or credential
  material.
