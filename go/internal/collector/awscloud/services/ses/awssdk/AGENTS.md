# AGENTS.md - internal/collector/awscloud/services/ses/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - SES SDK pagination, the get fan-out, and telemetry.
3. `mapping.go` - safe metadata mapping from the SDK responses into
   scanner-owned types.
4. `../scanner.go` - scanner-owned SES fact selection.
5. `../README.md` - SES scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep SES SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface to the six read operations only:
  ListEmailIdentities, GetEmailIdentity, ListConfigurationSets,
  GetConfigurationSet, GetConfigurationSetEventDestinations,
  ListDedicatedIpPools. Never add a send, template, contact, suppression-read,
  or any Create/Update/Delete/Put mutation API. The exclusion reflection test
  must stay green.
- Wrap each AWS list page or get point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe metadata: identity name/type/verification status, sending
  and feedback-forwarding flags, default configuration set name, DKIM
  enabled/status/origin enums, MAIL FROM domain/status/behavior, configuration
  set sending/reputation/TLS options, sending pool name, custom redirect domain,
  event-destination class and resolvable target ARNs, and dedicated IP pool
  names.
- Never map the `DkimAttributes.Tokens` signing tokens, the identity policy
  documents in `GetEmailIdentityOutput.Policies`, any signing-key material,
  destination secrets, HEC tokens, or access keys.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new SES metadata read by extending the scanner `Client` and this
  adapter, writing a scanner or adapter test first, then mapping the SDK
  response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not send email, mutate identities, configuration sets, event destinations,
  or pools, or call any SES mutation API.
- Do not widen the `apiClient` interface beyond the six read operations.
- Do not infer workload, environment, deployment, or ownership truth from SES
  names, domains, or tags.
- Do not write facts, graph rows, or reducer-owned state here.
