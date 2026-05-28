# AWS Collector Security And Config

Use this page for AWS collector credentials, target scopes, IAM guardrails, and
redaction. The overview lives in [AWS Cloud Collector](collector-aws-cloud.md).

## Required Environment

| Env var | Purpose |
| --- | --- |
| `ESHU_POSTGRES_DSN` or split Postgres DSNs | Shared Postgres runtime loader. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances. Must include one enabled `aws` instance with `claims_enabled=true`. |
| `ESHU_AWS_REDACTION_KEY` | Required when any target scope enables CloudWatch, ECS, Lambda, Security Hub, Organizations, or IAM Identity Center (`ssoadmin`). CloudWatch alarm metric dimension values can be customer-tag-named and Identity Center principal display names are redacted before persistence. |

Optional knobs: `ESHU_AWS_COLLECTOR_INSTANCE_ID`,
`ESHU_AWS_COLLECTOR_OWNER_ID`, `ESHU_AWS_COLLECTOR_POLL_INTERVAL`,
`ESHU_AWS_COLLECTOR_CLAIM_LEASE_TTL`, and
`ESHU_AWS_COLLECTOR_HEARTBEAT_INTERVAL`. Heartbeat must stay below the lease
TTL.

## Target Scopes

Each scope must name:

- a 12-digit `account_id`
- concrete `allowed_regions`
- concrete `allowed_services`
- one credential mode

Wildcard regions and services are rejected. `allowed_services` must name a
supported scanner from [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md).
`max_concurrent_claims=0` or unset means one active claim per account; positive
values raise the collector-side per-account limit.

## Credential Modes

| Mode | Required | Rejected |
| --- | --- | --- |
| `central_assume_role` | `role_arn`, `external_id`; role ARN account must match `account_id` | static access keys |
| `local_workload_identity` | local AWS SDK chain | `role_arn`, `external_id`, static access keys |

Static access-key fields are rejected during configuration parsing.

## IAM And Redaction

Keep permissions read-only and service-scoped. Grant only metadata `List*`,
`Describe*`, and safe `Get*` calls required by enabled scanners. Do not grant
mutation APIs or data-plane reads for secret values, SSM parameter values, SQS
messages, DynamoDB items, log events, API execution payloads, S3 object
contents, database contents, Lambda packages, GuardDuty finding bodies,
GuardDuty filter criteria, GuardDuty threat intel/IP list contents, WAFv2 IP
set address lists, WAFv2 regex pattern bodies, or WAFv2 rule `Statement`
bodies.

CloudWatch, ECS, Lambda, Security Hub, Organizations, and IAM Identity Center
(`ssoadmin`) scans require `ESHU_AWS_REDACTION_KEY` before startup because
sensitive-derived fields are redacted before persistence. The key produces
deterministic HMAC markers; it is not stored in facts. Security Hub action
target descriptions and Organizations account email/name values pass through the
shared redaction helper, CloudWatch alarm metric dimension values whose names
look like customer tags pass through the same helper, and Identity Center
principal display names resolved from the identity store pass through the same
helper.

IAM Identity Center permission set inline policy bodies
(`GetInlinePolicyForPermissionSet`), permissions boundary bodies
(`GetPermissionsBoundaryForPermissionSet`), customer-managed policy bodies, and
application access-scope attributes (`GetApplicationAccessScope`,
`ListApplicationAccessScopes`) are not read or persisted. Do not grant Identity
Center mutation APIs to the collector role: `CreatePermissionSet`,
`UpdatePermissionSet`, `DeletePermissionSet`, `PutInlinePolicyToPermissionSet`,
`DeleteInlinePolicyFromPermissionSet`, `PutPermissionsBoundaryToPermissionSet`,
`DeletePermissionsBoundaryFromPermissionSet`, `AttachManagedPolicyToPermissionSet`,
`DetachManagedPolicyFromPermissionSet`,
`AttachCustomerManagedPolicyReferenceToPermissionSet`,
`DetachCustomerManagedPolicyReferenceFromPermissionSet`,
`CreateAccountAssignment`, `DeleteAccountAssignment`, `CreateApplication`,
`UpdateApplication`, `DeleteApplication`, or `ProvisionPermissionSet`.

Security Hub finding bodies and insight filters are not persisted. Finding
aggregate counts grouped by severity, standard, control, compliance status, and
workflow status are in scope. Finding resource identifiers, resource details,
remediation text, product fields, user-defined fields, note text, network
details, and process details are out of scope.

Do not grant Security Hub mutation APIs to the collector role:
`BatchUpdateFindings`, `BatchImportFindings`, `CreateInsight`, `DeleteInsight`,
`UpdateInsight`, `EnableSecurityHub`, `DisableSecurityHub`, `EnableStandards`,
`DisableStandards`, `CreateActionTarget`, `DeleteActionTarget`,
`UpdateActionTarget`, `BatchEnableStandards`, or `BatchDisableStandards`.

Organizations scopes must run with management-account or delegated-administrator
credentials and `allowed_regions=["us-east-1"]`. Non-org-aware credentials emit
bounded org-access-skipped warning/status/metric evidence instead of partial
organization facts.

WAFv2 IP set address lists, regex pattern set bodies, and rule `Statement`
bodies are not persisted. The scanner persists the IP set address count and IP
version, the regex pattern count, web ACL and rule group rule counts, managed
rule set references (vendor and name), and reference ARNs only. IP set
addresses, regex detection bodies, and `Statement` search strings
(`AndStatement`/`OrStatement`/`NotStatement`/`ByteMatchStatement`) reveal threat
hypotheses and are out of scope. Do not grant WAFv2 mutation APIs to the
collector role: `CreateWebACL`, `UpdateWebACL`, `DeleteWebACL`,
`AssociateWebACL`, `DisassociateWebACL`, `Create`/`Update`/`Delete` for rule
groups, IP sets, and regex pattern sets, `PutLoggingConfiguration`,
`PutManagedRuleSetVersions`, `PutPermissionPolicy`, `TagResource`, or
`UntagResource`. WAF Classic (v1) is out of scope by construction: the scanner
imports only the WAFv2 SDK, which cannot surface `waf` or `waf-regional` v1
resources. A reflection test over the SDK adapter's API interface fails the
build path if a mutation or data-plane method is added.

Do not persist credential material, bearer tokens, session tokens, presigned
query parameters, secret values, policy JSON payload bodies, queue messages, log
events, database rows, object contents, Lambda package contents, GuardDuty
finding bodies, GuardDuty filter criteria, GuardDuty list contents, or raw AWS
error payloads in facts or metric labels.

## Helm Notes

Helm renders collector instances, instance selector, owner ID, Postgres env,
OTEL env, probes, metrics Service, optional `ServiceMonitor`, NetworkPolicy,
and PodDisruptionBudget.

Use `awsCloudCollector.serviceAccount.create=true` for IRSA so AWS permissions
do not attach to API, reducer, ingester, or other pods in the same release.

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md)
- [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Collector Environment](../reference/environment-collectors.md)
