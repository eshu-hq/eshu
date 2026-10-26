# Producer Grant DRAFT (`#8852`, `#6709`)

**DRAFT — not issued, not stored, not enforced.** Records the intended
first-party delegation for the extracted `aws-cloud` producer so review
can approve the exact kind/version/scope binding before cutover. Core
retains fact admission, canonical identity, projection, persistence, and
retraction; this grant authorizes emission only.

## Proposed grant identity

- Producer: `dev.eshu.collector.aws-cloud`
- Version: `0.1.0` (collector artifact version at cutover)
- Scope: `aws` (covers every `aws:<account>:<region>:<service_kind>`
  scope id; the grant binds identity + kind + schema + scope, never a
  single account or region)
- Kinds (all `1.0.0`; producer records already cutover-shaped with
  `AuthorityMode: core_issued_grant` and
  `PlannedRepository: eshu-hq/eshu-collector-aws-cloud`):
  `aws_resource`, `aws_relationship`, `aws_tag_observation`,
  `aws_dns_record`, `aws_image_reference`, `aws_security_group_rule`,
  `aws_iam_permission`, `aws_resource_policy_permission`, `aws_warning`
  (`go/internal/facts/cloud/aws.go:138`);
  `ec2_instance_posture` (`ec2_instance_posture.go:75`);
  `rds_instance_posture` (`rds_posture.go:70`);
  `s3_bucket_posture` (`s3_bucket_posture.go:71`);
  `s3_external_principal_grant`
  (`s3_external_principal_grant.go:70`)
- Lifetime: bounded `--expires-in` (rotation expected; no non-expiring grants)

## Issuance (operator commands at cutover)

One grant per core-owned kind (the CLI binds one kind per grant):

```bash
for kind in aws_resource aws_relationship aws_tag_observation aws_dns_record aws_image_reference aws_security_group_rule aws_iam_permission aws_resource_policy_permission aws_warning; do
  eshu component grant dev.eshu.collector.aws-cloud \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --schema-version 1.0.0 \
    --scope aws \
    --expires-in 2160h
done
for kind in ec2_instance_posture rds_instance_posture s3_bucket_posture s3_external_principal_grant; do
  eshu component grant dev.eshu.collector.aws-cloud \
    --component-home ~/.eshu/components \
    --version 0.1.0 \
    --kind "${kind}" \
    --schema-version 1.0.0 \
    --scope aws \
    --expires-in 2160h
done
eshu component grants --component-home ~/.eshu/components \
  --producer dev.eshu.collector.aws-cloud
```

Grant-first-then-install: authorize before activating the replacement,
which is the order cutover needs.

Cross-family note: the 13 kinds span five fact-family declarations
(`aws`, EC2/RDS/S3 posture, S3 external-principal grant), but every
record names the same producer identity (`collector-aws-cloud`) with
the same scope (`aws`). The grant binds producer identity + kind +
schema version + scope, so the extracted producer emits only under
`scope aws` and can never write under a sibling collector's authority.

## Enforcement (already core-owned, `#6709`)

- Admit only when producer identity, version, kind, schema version, and
  scope all match a live, unexpired, unrevoked stored grant; anything
  else fails closed at install, activation, and every emission.
- Registry read failures deny core-owned kinds rather than emitting
  under unknown authorization.
- Revocation (`component revoke-grant …`) fails the next emission
  terminal; absent-grant revocation returns `grant_not_found`, never
  silent success.

## First-party note

First-party status changes packaging trust, not authority: the extracted
producer still emits through the grant like any external producer.
Delegation does not transfer canonical ownership — no graph edges, no
identity choice, no queue or storage access. Credential routing travels
in `config_handle` (role ARN + external ID, or workload-identity mode);
raw credential material never crosses the boundary, and static access
keys stay refused (`config.go:223-227`).

## Pre-issuance checklist

- [ ] Satellite `go.mod` reconciled to the supported SDK path at the
  exact pins (`collector v0.1.0-rc.3`, `factschema v0.1.0-rc.5`).
- [ ] Parity proof green on the fixture pack (`#4047` gate input).
- [ ] Rollback rehearsed ([ROLLBACK_DRAFT](ROLLBACK_DRAFT.md)).
- [ ] Cutover order recorded: AWS restores independently of Jira
  (`#8858`) and PagerDuty (`#8863`) — new instance id, same
  `aws:<account>:<region>:<service_kind>` scope ids, in-tree producer
  disabled in the same values change.
- [ ] Expiry/rotation owner and alert recorded before issuance.
