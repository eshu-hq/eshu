# AGENTS.md - internal/collector/awscloud/services/fms guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Firewall Manager domain types.
3. `scanner.go` - policy resource and member-account relationship emission.
4. `relationships.go` - the policy-to-member-account edge contract.
5. `awssdk/README.md` - AWS SDK API allowlist and metadata boundary.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   coverage table.

## Invariants

- Keep Firewall Manager API access behind `Client`; do not import the AWS SDK
  into this package.
- Never call or model mutation APIs: PutPolicy, DeletePolicy,
  PutNotificationChannel, DeleteNotificationChannel, AssociateAdminAccount,
  DisassociateAdminAccount, PutAdminAccount, PutAppsList, DeleteAppsList,
  PutProtocolsList, DeleteProtocolsList, PutResourceSet, DeleteResourceSet,
  BatchAssociateResource, BatchDisassociateResource, AssociateThirdPartyFirewall,
  DisassociateThirdPartyFirewall, TagResource, or UntagResource.
- Never persist the policy rule payload. The SecurityServicePolicyData managed
  service data document, account inclusion/exclusion maps, and resource tag
  selectors are out of scope. Do not add a GetPolicy read to resolve them.
- Record the security service type and in-scope resource type as labels only.
  They name the kind of resource a policy governs; this package does not own
  those resource nodes.
- Key the member-account edge on the bare 12-digit account id with no
  synthesized target ARN. It must match the `aws_organizations_account`
  resource_id the organizations scanner publishes.
- Never key a relationship identity on a list index or API response order. The
  member-account set is deduplicated and sorted before edges are emitted.
- Emit reported evidence only. Do not infer AWS Organizations hierarchy,
  workload ownership, deployment truth, or reducer-owned correlation here.
- Keep policy ARNs, policy ids, and member account ids out of metric labels.

## Common Changes

- Add a new policy metadata field by extending `Policy`, writing a focused
  scanner or adapter test first, then mapping it through `awscloud` envelope
  builders.
- Add a new relationship only when Firewall Manager reports both sides directly
  and neither side depends on the policy rule payload.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not persist the policy rule payload, account inclusion/exclusion maps,
  resource tag selectors, or any FMS mutation result.
- Do not introduce reducer admission, graph writes, or query behavior.
- Do not resolve member account ids, policy names, or resource types into
  ownership or deployment truth here; reducers own correlation.
- Do not add AWS credential loading or STS calls to this package.
