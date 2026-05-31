# AGENTS.md - internal/collector/awscloud/services/servicecatalog guidance

## Read First

1. `README.md` - package purpose, exported surface, metadata-only policy, and
   invariants.
2. `types.go` - scanner-owned Service Catalog domain types and the `Client`
   read interface.
3. `scanner.go` - portfolio, product, provisioned-product, and relationship
   emission.
4. `relationships.go` - relationship emission rules and graph-join keys.
5. `helpers.go` - ARN service/resource segment parsing and identity helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Service Catalog API access behind `Client`; do not import the AWS SDK
  into this package.
- Never provision, update, or terminate products; never associate or
  disassociate principals or portfolios; never mutate constraints; never wire
  any `Provision*`, `Update*`, `Delete*`, `Create*`, `Terminate*`,
  `Associate*`, or `Disassociate*` API.
- Never persist provisioning-artifact template bodies, launch-constraint policy
  documents, provisioning parameter values, or record output values. The
  scanner-owned types carry no such field.
- Source a resource's own outgoing edges on the same identifier the scanner
  publishes as that node's `resource_id` (`firstNonEmpty(arn, id)`), or the
  edge dangles.
- Emit the provisioned-product-to-CloudFormation-stack edge only for `CFN_STACK`
  provisioned products whose physical identifier is a CloudFormation stack ARN
  (service segment `cloudformation`, resource segment `stack/...`), keyed by the
  stack ARN the `cloudformation` scanner publishes.
- Emit the portfolio-to-IAM-role edge only when AWS reports a fully defined IAM
  role ARN (service segment `iam`, resource segment `role/...`). Skip IAM users,
  groups, and `IAM_PATTERN` wildcard principals.
- Parse ARN service segments exactly (the third colon-delimited field), never as
  a substring of the whole ARN. A substring can match an unrelated ARN.
- Never hardcode `arn:aws:`. Synthesized ARNs derive their partition from the
  scan boundary or a source ARN. (This scanner keys every edge on an
  API-reported ARN, so no partition synthesis is needed today; keep it that
  way.)
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from portfolio, product, or
  provisioned-product names.
- Preserve stable portfolio, product, and provisioned-product identities across
  repeated observations in the same AWS generation. Never key a synthesized
  identity on a list index or API page order.
- Keep Service Catalog ARNs, names, parameters, tags, and AWS error payloads out
  of metric labels.

## Common Changes

- Add a new Service Catalog metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry parameter values,
  template bodies, or record outputs, leave it out of the contract.
- Add new relationship evidence only when the Service Catalog API reports both
  sides directly and the target identity matches an existing scanner's published
  `resource_id` shape. Verify the target shape by reading that scanner's
  `scanner.go`; never assume.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not provision, update, terminate, associate, disassociate, share, or
  constrain any Service Catalog resource.
- Do not call `DescribeProvisioningArtifact`, `DescribeRecord`,
  `GetProvisionedProductOutputs`, `DescribeProvisioningParameters`, or any API
  that returns template bodies, parameter values, or stack output values.
- Do not resolve Service Catalog names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
