# AGENTS.md - internal/collector/awscloud/services/ds guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Directory Service domain types and the read-only
   `Client`.
3. `scanner.go` - directory, trust, and shared-directory emission and the
   per-directory fan-out.
4. `relationships.go` - VPC, subnet, trust, shared-directory, and owner-account
   relationship observations.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - Directory Service
   coverage.

## Invariants

- Keep Directory Service API access behind `Client`; do not import the AWS SDK
  into this package.
- NEVER add a mutation API (ResetUserPassword, Create/Delete/Update/Enable/
  Disable/Register/Accept/Reject/Share/...) to the `Client` interface. A
  reflection test in the `awssdk` adapter enforces the metadata-only SDK seam.
- NEVER persist the directory admin password, the RADIUS shared secret, or the
  AD Connector service-account credentials: the scanner-owned types have no field
  for them and must never gain one.
- The directory `resource_id` MUST stay the bare directory ID (`d-xxxxxxxxxx`).
  The FSx scanner's AD-directory edges target the bare directory ID, so changing
  this resource_id would dangle the merged FSx edge.
- Every relationship sets a non-empty `target_type` and a `target_resource_id`
  that matches the target scanner's `resource_id`. VPC -> `aws_ec2_vpc` (bare
  vpc-id), subnet -> `aws_ec2_subnet` (bare subnet-id), trust-to-directory and
  shared-directory-to-owner-directory -> `aws_ds_directory` (bare directory id),
  owner-account -> `aws_account` (bare 12-digit account id, never a synthesized
  ARN).
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from directory names, domain names, or tags.
- Keep secrets, domain names, and tags out of metric labels.

## Common Changes

- Add a new Directory Service resource or attribute by extending the
  scanner-owned type, writing a focused scanner test first, then mapping it
  through `awscloud` envelope builders.
- Add new directory fields only when the Directory Service API reports them on
  the describe path and the field is safe for persistence (not a secret).
- Extend SDK pagination and SDK-to-scanner mapping in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not resolve directories, trusts, or shared directories to source
  repositories or services here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not relax the secret-exclusion invariants; they are the security contract
  for this scanner (issue #827).
