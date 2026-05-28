# AGENTS.md - internal/collector/awscloud/services/cognito guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Cognito domain types and the read-only `Client`.
3. `scanner.go` - user-pool, app-client, provider, resource-server, group, and
   identity-pool emission.
4. `relationships.go` - user-pool-client, lambda-trigger, and identity-pool
   relationship observations.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - Cognito coverage.

## Invariants

- Keep Cognito API access behind `Client`; do not import the AWS SDK into this
  package.
- NEVER add user-record reads (ListUsers, AdminGetUser, AdminListGroupsForUser,
  ListUsersInGroup) to the `Client` interface. Cognito user records are PII. A
  reflection test enforces their absence.
- NEVER persist app-client ClientSecret. `UserPoolClient` has no secret field.
- NEVER persist identity-provider ProviderDetails (client_secret,
  google_client_secret, and similar). `IdentityProvider` has no details field.
- Route operator-supplied free text (identity-pool developer provider name,
  group description) through `awscloud.RedactString`; the scanner requires a
  non-zero redaction key.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from pool names, client names, providers, or tags.
- Keep secrets, provider details, free text, and tags out of metric labels.

## Common Changes

- Add a new Cognito resource by extending the scanner-owned type, writing a
  focused scanner test first, then mapping it through `awscloud` envelope
  builders.
- Add new user-pool or identity-pool fields only when the Cognito API reports
  them directly and the field is safe for persistence (not a secret).
- Extend SDK pagination and SDK-to-scanner mapping in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not resolve Cognito pools, clients, or providers to source repositories or
  services here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not drop the redaction-key guard; it is part of the security contract for
  this scanner.
