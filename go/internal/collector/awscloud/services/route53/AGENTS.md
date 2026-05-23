# AGENTS.md - internal/collector/awscloud/services/route53 guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `types.go` - scanner-owned Route 53 records and client contract.
3. `scanner.go` - fact selection and hosted-zone/record envelope mapping.
4. `awssdk/README.md` - AWS SDK pagination and response mapping.

## Invariants

- Do not call AWS APIs from this package. The `awssdk` adapter owns AWS SDK
  calls and telemetry.
- Preserve hosted-zone visibility from `HostedZone.Config.PrivateZone`.
- Preserve alias `DNSName`, alias `HostedZoneID`, and `EvaluateTargetHealth`.
- Emit DNS records as `aws_dns_record`, not `aws_resource`.
- Do not infer application ownership, environment, service identity, public
  exposure, or deployable-unit truth from DNS names or zone names.
- Keep DNS names, hosted-zone IDs, tags, and record values out of metric labels.

## Common Changes

- Add a new record attribute in `scanner.go` only when it supports DNS-to-cloud
  joining, freshness, or later correlation.
- Add new Route 53 routing policy fields in `types.go`, `scanner.go`, and
  `awssdk/mapper.go` together.
- Add a focused scanner test before changing which record types are emitted.

## What Not To Change Without An ADR

- Do not add Route 53 write APIs.
- Do not make the scanner write graph rows directly.
- Do not broaden collection to all record types without updating issue scope
  and downstream data-use expectations.
