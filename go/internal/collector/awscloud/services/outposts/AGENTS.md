# AGENTS.md - internal/collector/awscloud/services/outposts guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Outposts domain types.
3. `scanner.go` - outpost, site, and asset resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware asset id synthesis,
   and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Outposts API access behind `Client`; do not import the AWS SDK into this
  package.
- NEVER read or persist physical site street addresses, the ISO country code,
  city/state/region, free-form site notes, shipping or contact details, or rack
  physical-property logistics. The scanner-owned `Site` and `Asset` types must
  never grow a field for any of those values.
- Persist only operational identity: outpost identity/status/AZ/owner/site,
  site id/name/account, and asset id/type/rack/state plus rack-unit elevation.
- The outpost node publishes its resource_id as the outpost ARN (fallback to the
  short id). Key the asset-in-outpost edge on that exact value so it joins the
  outpost node.
- The site node publishes its resource_id as the site ARN (fallback to the short
  id). Key the outpost-in-site edge on that value.
- Assets have no AWS ARN. Synthesize the asset resource_id under the parent
  outpost ARN (`<outpost-arn>/asset/<asset-id>`) so it stays partition-aware;
  never concatenate a literal `arn:aws:`. Source the asset's own edge on that id.
- Set `target_arn` only when the join key is ARN-shaped, matching how the target
  node publishes its resource_id.
- This service is intentionally low-edge. Cross-references that name an Outpost
  from another service (subnet, EBS volume, load balancer carrying an
  `OutpostArn`) are reverse edges owned by the VPC/EC2/ELB scanners. Do not
  invent them here.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from outpost, site, or asset
  names, or AWS tags.
- Preserve stable outpost, site, and asset identities across repeated
  observations in the same AWS generation.
- Keep Outposts resource ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new Outposts metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry a physical address, shipping/contact
  detail, free-form note, or rack physical-property value, leave it out of the
  scanner contract.
- Add new relationship evidence only when the Outposts API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or persist site addresses, country codes, notes, shipping or
  contact details, or rack physical-property logistics.
- Do not call any Outposts mutation API, order/billing/connection read, or
  GetSiteAddress.
- Do not resolve Outposts names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
