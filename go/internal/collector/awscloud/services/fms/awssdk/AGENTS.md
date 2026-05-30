# AGENTS.md - internal/collector/awscloud/services/fms/awssdk guidance

## Read First

1. `README.md` - package purpose, API allowlist, telemetry, and invariants.
2. `client.go` - Firewall Manager SDK client construction, pagination, and
   telemetry.
3. `exclusion_test.go` - the reflective metadata-only guard on `apiClient`.
4. `../scanner.go` - scanner-owned Firewall Manager fact selection.
5. `../README.md` - Firewall Manager scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector coverage.

## Invariants

- Keep Firewall Manager SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe Firewall Manager metadata.
- The `apiClient` interface exposes only `ListPolicies` and
  `ListComplianceStatus`. Do not add `GetPolicy`; it returns the rule payload.
- Reduce `ListComplianceStatus` responses to the deduplicated `MemberAccount`
  ids in memory. Never return or log compliance evaluation results, issue info,
  or violator resource details.
- Do not call mutation APIs: PutPolicy, DeletePolicy, PutNotificationChannel,
  DeleteNotificationChannel, AssociateAdminAccount, DisassociateAdminAccount,
  PutAdminAccount, PutAppsList, DeleteAppsList, PutProtocolsList,
  DeleteProtocolsList, PutResourceSet, DeleteResourceSet, BatchAssociateResource,
  BatchDisassociateResource, AssociateThirdPartyFirewall,
  DisassociateThirdPartyFirewall, TagResource, or UntagResource.
- Never synthesize a policy ARN. Use the AWS-reported ARN verbatim so the
  partition survives in GovCloud and China.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Firewall Manager metadata read by extending `Client` and the
  `apiClient` interface, writing a scanner or adapter test first, then mapping
  the SDK response into scanner-owned types. Keep new methods read-only and
  metadata-only; add forbidden mutation/rule-payload names to
  `forbiddenFMSOperations`.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not persist policy rule payloads, account inclusion/exclusion maps,
  resource tag selectors, or Firewall Manager mutation results.
- Do not add a `GetPolicy` read.
- Do not infer workload, environment, deployment, account hierarchy, or
  ownership truth from policy names, security service types, or member accounts.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
