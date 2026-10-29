# AWS Collector Security And Config

Use this page for AWS collector credentials, target scopes, IAM guardrails, and
redaction. The overview lives in [AWS Cloud Collector](collector-aws-cloud.md).

## Required Configuration (Satellite Activation)

The extracted satellite (`eshu-hq/eshu-collector-aws-cloud`) resolves
credentials, target scopes, and redaction through its activation config (see
the satellite repo's `config.example.yaml`), not through in-tree process
environment. It collects with ambient workload credentials only (IRSA,
container, or instance role); no credential key exists in config.

| Setting | Purpose |
| --- | --- |
| Satellite redaction key | Required for live Lambda and ECS scans. Lambda environment-variable values and ECS task-definition environment values can carry secrets; the satellite turns them into deterministic HMAC markers before persistence. |

For a Helm component activation with live Lambda or ECS, set
`config.source.redactionKeyEnv: ESHU_AWS_REDACTION_KEY` and point
`awsCloudCollector.redaction.secretName` and `keyKey` at an existing
Kubernetes Secret. The chart injects the key into the AWS activation worker's
environment through `secretKeyRef`; its process runner passes that environment
to the satellite. The activation ConfigMap carries only the environment
variable name. A missing or blank Secret key prevents collection; Helm also
refuses the live Lambda/ECS activation when the reference or Secret metadata
is absent.
Give the AWS activation its own `serviceAccount.create: true` and workload
identity annotations, or point `serviceAccount.name` at an existing dedicated
account. The coordinator's shared account must not receive AWS permissions.
Restart the AWS activation Deployment after rotating the Secret: running pods
keep their previous environment value, and a new key changes HMAC markers.
Helm checks collisions with enabled collector ServiceAccounts in this release
and an explicitly named schema-bootstrap account. It cannot inspect accounts
outside the release; verify that an existing named account is dedicated to the
AWS activation and that its IAM role is limited to the intended reads.

Claim polling, lease TTL, and heartbeat are satellite activation settings
(heartbeat must stay below the lease TTL).

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
set address lists, WAFv2 regex pattern bodies, WAFv2 rule `Statement` bodies,
Network Firewall rule group rule sources (Suricata signature bodies),
Inspector v2 finding details, Inspector v2 filter criteria, or Inspector v2 CIS
scan results.

The extracted satellite's live Lambda and ECS scans require the redaction key
before collecting those services. The key produces deterministic HMAC markers;
it is not stored in facts. The legacy direct collector also required a key for
CloudWatch, CodeBuild, Security Hub, Organizations, and IAM Identity Center
(`ssoadmin`) scans. Its Security Hub
action target descriptions and Organizations account email/name values pass
through the shared redaction helper, CloudWatch alarm metric dimension values
whose names look like customer tags pass through the same helper, CodeBuild
environment-variable PLAINTEXT values pass through the same helper, and Identity
Center principal display names resolved from the identity store pass through the
same helper.

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

Network Firewall rule group rule sources are not persisted. The rule source
holds Suricata signature bodies and stateless rule definitions, which are
threat-detection intelligence. The scanner reads rule group metadata through
`DescribeRuleGroupMetadata`, which returns only the rule group type
(`STATEFUL`/`STATELESS`), configured capacity, and identity; it never calls
`DescribeRuleGroup`, whose output carries the rule source. Firewall policy facts
carry the AWS-defined default-action names (for example `aws:forward_to_sfe`,
`aws:drop_strict`), consumed rule capacity, and rule group / TLS inspection
configuration reference ARNs only, never the full policy rule body. TLS
inspection configuration facts carry identity, status, and association count
only, never certificate bodies or TLS scope rule bodies. Firewall facts carry
identity, VPC id, subnet mapping ids, protection flags, and readiness status.
Do not grant Network Firewall mutation APIs to the collector role:
`CreateFirewall`, `DeleteFirewall`, `Update*` (delete protection, description,
encryption, policy/subnet/availability-zone change protection),
`AssociateFirewallPolicy`, `Associate`/`DisassociateSubnets`,
`Associate`/`DisassociateAvailabilityZones`, `Create`/`Update`/`Delete` for
firewall policies, rule groups, and TLS inspection configurations,
`PutResourcePolicy`, `UpdateLoggingConfiguration`, `TagResource`, or
`UntagResource`. A reflection test over the SDK adapter's API interface fails
the build path if a mutation method or `DescribeRuleGroup` is added, and a
struct reflection test fails if a scanner-owned type gains a rule-body field.

Inspector v2 finding details are not persisted. A CVE plus package version plus
affected host ARN reveals exploitation surface. Inspector v2 filter criteria
expressions, filter descriptions, and filter reasons, plus CIS scan results, are
also out of scope; filter facts carry non-criteria identity only (filter ARN,
name, action, and owner ID). The Inspector v2 scanner emits account status,
enabled scan features, member accounts, filter non-criteria identity, and CIS
scan configuration metadata, and makes no finding-listing or finding-aggregation
call.

Do not grant Inspector v2 mutation APIs to the collector role: `Enable`,
`Disable`, `EnableDelegatedAdminAccount`, `DisableDelegatedAdminAccount`,
`AssociateMember`, `DisassociateMember`, `CreateFilter`, `UpdateFilter`,
`DeleteFilter`, `CreateCisScanConfiguration`, `UpdateCisScanConfiguration`,
`DeleteCisScanConfiguration`, or `BatchUpdateMemberEc2DeepInspectionStatus`. Do
not grant `GetFilter`, `ListFindings`, `GetFindings`, `ListFindingAggregations`,
`BatchGetFindingDetails`, `BatchGetCodeSnippet`, `GetSbomExport`,
`GetCisScanReport`, or `GetCisScanResultDetails`.

OpenSearch is the collector's highest index/search-data exposure surface and is
metadata-only by construction. The scanner reaches the control plane through
`ListDomainNames`, `DescribeDomains`, `DescribePackages`,
`ListDomainsForPackage`, `ListTags`, `ListCollections`, `BatchGetCollection`,
`ListSecurityConfigs`, `ListVpcEndpoints`, and `BatchGetVpcEndpoint` only. It
never reaches the OpenSearch HTTP API (`_search`, `_msearch`, `_index`, `_doc`,
`_bulk`, and similar): that API is served by the domain HTTP endpoint, which the
scanner never constructs, and the SDK adapter interfaces carry no such method
and no `GetIndex` on either service. A reflection gate over the `domainAPI` and
`serverlessAPI` interfaces fails the build path if a mutation, inbound-connection
acceptance, index, search, or data method is added. Master user passwords are
never persisted: `DescribeDomains` does not return the password, and the
scanner-owned `Domain` type has no password-, secret-, token-, or
credential-shaped field, enforced by a struct-shape test. Domain endpoint
contents, the domain `Endpoints` map, the access policy body, custom package
bodies, and serverless saved-object bodies are out of scope; only IAM role ARNs
referenced by the access policy are resolved for relationship evidence and the
policy body itself is dropped. Do not grant OpenSearch mutation APIs to the
collector role: `CreateDomain`, `DeleteDomain`, `UpdateDomainConfig`,
`CreatePackage`, `DeletePackage`, `AssociatePackage`, `DissociatePackage`,
`AcceptInboundConnection`, `RejectInboundConnection`, `CreateCollection`,
`DeleteCollection`, `UpdateCollection`, `CreateSecurityConfig`,
`CreateVpcEndpoint`, or `DeleteVpcEndpoint`; do not grant `GetIndex` on either
service. OpenSearch does not require the satellite redaction key.

Do not persist credential material, bearer tokens, session tokens, presigned
query parameters, secret values, policy JSON payload bodies, queue messages, log
events, database rows, object contents, Lambda package contents, GuardDuty
finding bodies, GuardDuty filter criteria, GuardDuty list contents, Inspector v2
finding details, Inspector v2 filter criteria, Inspector v2 CIS scan results, or
raw AWS error payloads in facts or metric labels.

## Helm Notes

Helm renders collector instances, instance selector, owner ID, Postgres env,
OTEL env, probes, metrics Service, optional `ServiceMonitor`, NetworkPolicy,
and PodDisruptionBudget.

For the extracted activation, use its own `serviceAccount.create=true` and
workload-identity annotation, or its `serviceAccount.name` for an existing
dedicated account. `awsCloudCollector.serviceAccount` configures only the
legacy direct collector Deployment; it does not give the extracted worker a
workload identity. Never put the AWS role on the shared Eshu ServiceAccount.

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Scanner Coverage](collector-aws-cloud-scanners.md)
- [Helm Collector And Webhook Values](../deploy/kubernetes/helm-collector-and-webhook-values.md)
- [Collector Environment](../reference/environment-collectors.md)
