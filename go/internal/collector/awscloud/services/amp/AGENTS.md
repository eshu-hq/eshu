# AGENTS.md - internal/collector/awscloud/services/amp guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned AMP domain types.
3. `scanner.go` - workspace, namespace, and scraper resource and relationship
   emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AMP API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read ingested time-series samples, query results, alert-manager
  definitions, rule-group definition bodies, or scrape-configuration bodies.
  Never call `DescribeRuleGroupsNamespace`, `DescribeWorkspaceConfiguration`,
  `GetDefaultScraperConfiguration`, or any `Create*`, `Put*`, `Update*`,
  `Delete*` mutation API. Rule-groups namespaces are recorded by NAME only.
- The workspace node publishes its resource_id as the workspace ARN (fallback to
  workspace id). Key the namespace-in-workspace and scraper-sends-to-workspace
  edges on that exact value so they join the workspace node.
- Source a namespace's own edges on the namespace ARN, the resource_id the
  namespace node publishes. Source a scraper's own edges on the scraper ARN.
- Emit the workspace-to-KMS-key edge only when AWS reports a customer-managed
  key. Set `target_arn` only when the identifier is ARN-shaped, matching the KMS
  scanner's published key resource_id.
- Emit the scraper-to-EKS-cluster edge keyed on the EKS cluster ARN
  (`aws_eks_cluster`), and the scraper-to-subnet / scraper-to-security-group
  edges keyed on the bare `subnet-` / `sg-` ids the EC2 scanner publishes
  (`aws_ec2_subnet` / `aws_ec2_security_group`). A non-EKS scraper source emits
  no EKS, subnet, or security-group edge.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from workspace, namespace, or
  scraper names, aliases, or AWS tags.
- Preserve stable workspace, namespace, and scraper identities across repeated
  observations in the same AWS generation.
- Keep AMP ARNs, names, aliases, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new AMP metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry rule definitions, scrape
  configuration, alert-manager definitions, or ingested samples, leave it out of
  the scanner contract.
- Add new relationship evidence only when the AMP API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for KMS keys, EKS clusters, and workspaces;
  bare ids for subnets and security groups).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read rule definitions, scrape configuration, alert-manager
  definitions, ingested samples, or query results, and do not call any AMP
  mutation API.
- Do not resolve AMP names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
