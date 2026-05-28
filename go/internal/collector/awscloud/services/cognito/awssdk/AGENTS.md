# AGENTS.md - internal/collector/awscloud/services/cognito/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - Cognito SDK pagination, describe calls, and telemetry.
3. `mapper.go` - SDK-to-scanner mapping that drops secrets.
4. `../scanner.go` - scanner-owned Cognito fact selection and redaction.
5. `../README.md` - Cognito scanner contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - Cognito coverage.

## Invariants

- Keep Cognito SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- NEVER add ListUsers, AdminGetUser, AdminListGroupsForUser, ListUsersInGroup,
  or ListUserPoolClientSecrets to `userPoolClientAPI`. NEVER add ListIdentities,
  DescribeIdentity, GetId, GetCredentialsForIdentity, or GetOpenIdToken* to
  `identityPoolAPI`. Reflection tests enforce both lists.
- NEVER add a Cognito mutation (Create/Update/Delete/Set) to either interface.
- Map `DescribeUserPoolClient` without ClientSecret. Map identity providers
  without ProviderDetails.
- Drop custom sender Lambda configs and the custom-sender KMS key; emit only
  ARN-shaped trigger slots.
- Preserve AWS API telemetry for every SDK call via `recordAPICall`.
- Do not log or label pool IDs, client IDs, provider names, secrets, or tags.

## Common Changes

- Add a new Cognito API read by extending `userPoolClientAPI` or
  `identityPoolAPI` (within the forbidden-method constraints), writing adapter
  mapping tests, and wrapping the SDK call with `recordAPICall`.
- Add mapping fields only after confirming Cognito reports them directly and the
  field is safe for persistence (not a secret).
- Keep retry and throttling behavior in the AWS SDK and telemetry wrapper; do
  not add local retry loops here without an ADR.

## What Not To Change Without An ADR

- Do not infer workload, environment, deployment, or ownership truth from
  Cognito names or tags here.
- Do not add graph writes, reducer logic, or query behavior.
- Do not cache cross-account credentials or create SDK clients outside the
  claim-scoped factory path.
