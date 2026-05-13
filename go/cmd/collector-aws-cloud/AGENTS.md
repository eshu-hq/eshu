# AGENTS.md - cmd/collector-aws-cloud guidance

## Read First

1. `README.md` - command purpose, configuration, and invariants.
2. `config.go` - collector instance selection and target-scope parsing.
3. `service.go` - claim-aware runner and runtime wiring.
4. `status_committer.go` - commit-side AWS scan status updates after fenced
   fact persistence.
5. `go/internal/collector/awscloud/awsruntime/README.md` - claim runtime
   contract.
6. Service `awssdk` README files under
   `go/internal/collector/awscloud/services/` - SDK adapter contracts.
7. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - security and
   runtime requirements.

## Invariants

- Do not accept static AWS credential fields.
- Require `ESHU_AWS_REDACTION_KEY` when ECS or Lambda is enabled so environment
  values cannot cross persistence boundaries in plaintext.
- Keep this command process-only. AWS credentials belong in `awsruntime`; AWS
  service pagination belongs in service `awssdk` adapters.
- Keep ELBv2 target health out of stable AWS collector facts; target health is
  live status, not routing topology.
- Keep Route 53 DNS names, hosted-zone IDs, and record values out of metric
  labels.
- Keep EC2 instance inventory out of the EC2 scanner; ENI attachment target
  evidence is metadata only.
- Keep Lambda function code and presigned package download URLs out of facts.
  Lambda image URIs, aliases, event-source mappings, execution roles, subnets,
  and security groups are reported join evidence only.
- Do not log credential values, trust policy JSON, resource ARNs, tags, or raw
  source payloads as metric labels.
- Preserve the split between scanner-side status in `awsruntime` and
  commit-side status in `status_committer.go`.

## Common Changes

- Add a new AWS service by extending target validation, adding scanner package
  tests, adding a service `awssdk` adapter, package docs, and branching in
  `awsruntime.DefaultScannerFactory.Scanner`.
- Add new command configuration with config tests first.
- Add SDK pagination in the service adapter so spans and AWS API counters stay
  complete.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim-aware commits.
- Do not cache cross-account credentials beyond the claim lease.
- Do not make this command write graph truth or reducer-owned rows directly.
