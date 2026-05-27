# AGENTS.md - internal/collector/awscloud/services/organizations/awssdk guidance

## Read First

1. `README.md` - adapter purpose, endpoint, telemetry, and invariants.
2. `client.go`, `policies.go`, `delegated.go`, and `helpers.go` - SDK
   pagination and metadata mapping.
3. `../scanner.go` - scanner-owned fact selection and redaction.
4. `../../../awsruntime/README.md` - runtime credential and scanner registry
   contract.
5. `docs/public/services/collector-aws-cloud-security.md` - credential and
   redaction rules.

## Invariants

- Use the AWS Organizations SDK only in this adapter package.
- Force Organizations API calls to `us-east-1` through `NewClient`.
- Do not add CreateAccount, MoveAccount, CloseAccount, AttachPolicy,
  DetachPolicy, EnableAWSServiceAccess, DisableAWSServiceAccess,
  RegisterDelegatedAdministrator, DeregisterDelegatedAdministrator,
  RemoveAccountFromOrganization, CreatePolicy, UpdatePolicy, or DeletePolicy.
- Do not call `DescribePolicy` or any API that returns policy document bodies.
- Keep account email/name values out of logs, spans, metric labels, and status
  messages. Pass them only to scanner-owned types so the scanner can redact
  before persistence.
- Treat `AccessDeniedException` and `AWSOrganizationsNotInUseException` on the
  org-aware preflight path as skipped snapshots, not broad retryable failures.

## Common Changes

- Add a new Organizations API read by defining it on `apiClient`, adding a fake
  test first, then mapping only metadata-safe fields into parent package types.
- Add pagination in this package and keep scanner tests focused on fact
  selection, not SDK page tokens.
- Add new API telemetry through `recordAPICall`; do not create per-resource or
  high-cardinality metric labels.

## What Not To Change Without An ADR

- Do not add policy body reads or policy persistence.
- Do not add Organizations mutation APIs.
- Do not add credential loading, STS AssumeRole, workflow claim parsing, graph
  writes, reducer logic, or query behavior here.
