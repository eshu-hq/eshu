# AWS Collector Scanner Coverage

Use this page for the AWS `service_kind` values backed by production scanner
adapters. Each scanner self-registers from
`go/internal/collector/awscloud/services/<svc>/runtimebind/init()`. The
collector-aws-cloud command pulls every binding through
`go/internal/collector/awscloud/awsruntime/bindings/bindings.go`, and the
runtime entry point `awsruntime.DefaultScannerFactory.Scanner` dispatches
through the resulting registry.

The collector is metadata-only. It emits reported facts for reducer admission.
It does not mutate AWS resources, read protected payloads, or write graph truth.

## Supported Service Kinds

`allowed_services` may include:

| Service kind | Coverage |
| --- | --- |
| `iam` | Roles, managed policies, instance profiles, trust relationships. |
| `ecr` | Repositories, lifecycle policies, image references, pagination checkpoints. |
| `ecs` | Clusters, services, tasks, relationships, redacted task definitions. |
| `ec2` | VPC, subnet, security group, security-group rule, ENI topology. The EC2 scanner owns the ENI surface and may carry instance target evidence on ENI attachments, but does not emit `aws_ec2_instance` resources; VPC network-fabric resources live in the `vpc` scanner. |
| `vpc` | Route tables, internet gateways, NAT gateways, network ACLs, VPC peering connections, VPC endpoints (gateway and interface), Elastic IPs, DHCP option sets, customer gateways, virtual private gateways, and site-to-site VPN connections plus relationships back to EC2-owned VPCs, subnets, ENIs, and instances. No VPN tunnel pre-shared keys or `CustomerGatewayConfiguration` XML bodies. |
| `transitgateway` | Transit gateways, transit gateway route tables, attachments (VPC, VPN, Direct Connect gateway, peering, Connect), peering attachments, multicast domains, and policy tables. Relationships: attachment-to-VPC/VPN-connection/Direct-Connect-gateway/peer, route-table-to-attachment, attachment-to-transit-gateway, route-table/multicast-domain/policy-table-to-transit-gateway, and peering-to-remote-transit-gateway. Cross-account peer transit gateway IDs, owner accounts, and Regions are surfaced as AWS reports them and flagged `cross_account` for downstream org-context joins; the scanner never resolves the remote account identity. No transit gateway routes, multicast group memberships, or policy table rule bodies. |
| `networkfirewall` | Firewalls (name, ARN, VPC id, subnet mapping ids, delete/subnet/policy-change protection flags, readiness status, configuration sync state), firewall policies (name, ARN, status, stateless/stateless-fragment/stateful default-action names, consumed rule capacity, association count - never the full policy rule body), rule groups (name, ARN, type STATEFUL/STATELESS, configured capacity, association count - never the rule source / Suricata signature bodies), and TLS inspection configurations (name, ARN, status, association count - never certificate bodies or TLS scope rule bodies). Relationships: firewall-to-VPC (bare vpc-id, targets `aws_ec2_vpc`), firewall-to-subnet (bare subnet-id, targets `aws_ec2_subnet`), firewall-to-firewall-policy, policy-to-rule-group, and policy-to-TLS-inspection-configuration. Rule group metadata is read through `DescribeRuleGroupMetadata`, not `DescribeRuleGroup`, so the rule source (threat intelligence) is unreachable by construction; the SDK adapter read surface excludes every mutation and the rule-body read by a reflection gate. No Network Firewall mutation APIs. |
| `directconnect` | Direct Connect connections (id, name, location, bandwidth, state, partner/provider name, MACsec capability flag), virtual interfaces (VLAN, BGP ASN, type private/public/transit, state), Direct Connect gateways, and link aggregation groups (LAGs). Relationships: virtual-interface-to-Direct-Connect-gateway, virtual-interface-to-connection, connection-to-LAG, Direct-Connect-gateway-to-transit-gateway, and Direct-Connect-gateway-to-virtual-private-gateway (the last two derived from gateway associations). The Direct Connect gateway resource uses `resource_type=aws_direct_connect_gateway` with the bare gateway id, which is the target the `transitgateway` scanner already emits, so the transit-gateway-to-Direct-Connect-gateway edge resolves once this scanner runs; gateway associations target `aws_ec2_transit_gateway` and `aws_vpc_vpn_gateway` by bare AWS id. Never reads or persists the BGP authentication key (`authKey`) on a virtual interface or BGP peer, never persists MACsec connectivity association key names (CKN) or secret ARNs (only the capability flag), and never calls `DescribeRouterConfiguration` (which renders the auth key). No Direct Connect mutation APIs. |
| `elbv2` | Load balancers, listeners, listener rules, target groups, routing relationships. |
| `lambda` | Functions, aliases, event-source mappings, image URIs, execution roles, network joins, redacted environment values. |
| `eks` | Clusters, node groups, add-ons, OIDC providers, IAM roles, network join evidence. |
| `elasticbeanstalk` | Applications (name, ARN, description, configuration template names, version labels), environments (name, ARN, environment id, status, tier name/type, platform ARN, solution stack, CNAME, endpoint URL, health, health status, deployed version label, template name), and application versions (label, status, source S3 bucket/key, source repository, build ARN). Relationships: environment-to-application, environment-to-VPC (from the `aws:ec2:vpc/VPCId` option setting, targets `aws_ec2_vpc`), environment-to-instance-profile (from `aws:autoscaling:launchconfiguration/IamInstanceProfile`, targets `aws_iam_instance_profile`), environment-to-service-role (from `aws:elasticbeanstalk:environment/ServiceRole`, targets `aws_iam_role`), environment-to-load-balancer (from DescribeEnvironmentResources; an ELBv2 ALB/NLB ARN targets `aws_elbv2_load_balancer` with a real `target_arn`, while a bare Classic Load Balancer name falls back to `aws_resource` without a fabricated ARN), environment-to-Auto-Scaling-group (targets `aws_autoscaling_group`), environment-to-launch-template (targets `aws_ec2_launch_template`), and environment-to-application-version. Every environment option-setting value routes through the shared redact library: option names and namespaces are kept, values never are, because they can carry secret environment variable values. The scanner never reads application-version source bundle object contents or environment-info presigned-URL bundles, and never mutates any Elastic Beanstalk resource (no Create/Update/Delete/Terminate/RebuildEnvironment/SwapEnvironmentCNAMEs); the accepted SDK surface excludes those operations by construction. Requires `ESHU_AWS_REDACTION_KEY`. |
| `emr` | EMR on EC2 clusters (running + recently terminated), uniform instance groups, instance fleets, security configurations (name only), EMR Serverless applications, EMR Studios, and Studio session mappings under one `service_kind`; resource types distinguish the surfaces. Relationships: cluster-to-subnet/security-group/IAM-role/instance-profile/security-configuration/KMS-key and cluster-to-instance-group/fleet; application-to-subnet/security-group/KMS-key; studio-to-VPC/subnet/security-group/IAM-role/KMS-key and studio-to-session-mapping. The cluster-to-VPC and application-to-VPC joins are derived from subnet membership downstream because the EMR cluster and EMR Serverless APIs do not report a VPC id; EMR Studio reports its VPC id directly. No step command lines (`Args`), bootstrap action script bodies, security configuration policy bodies, or EMR Serverless job-run `SparkSubmit.entryPointArguments`: the scanner never calls ListSteps, DescribeStep, ListBootstrapActions, DescribeSecurityConfiguration, or any EMR Serverless job-run reader. |
| `batch` | Compute environments (name, type, state, service role ARN, orchestration type, ECS/EKS cluster ARN, instance-profile role, subnets, security groups, launch template), job queues (name, state, priority, compute-environment order), job definitions (name, type, container image URI, job/execution IAM role - never the container command list, never environment values in clear text), scheduling policies (name and ARN only - never fair-share weight state), and recent active jobs (id, status, job-definition reference - never job parameters or container overrides). Relationships: job-queue-to-compute-environment, compute-environment-to-subnet, compute-environment-to-security-group, compute-environment-to-launch-template, compute-environment-to-IAM-role, job-definition-to-IAM-role, job-definition-to-container-image, and job-definition-to-secret-reference (Secrets Manager / SSM ARN ref only). The compute-environment-to-VPC join is reached transitively through the subnet edge. The scanner never submits, cancels, or terminates jobs, never registers or deregisters job definitions, and never mutates any Batch resource; the adapter read surface excludes those operations by construction. Container environment values route through the shared redact library. Requires `ESHU_AWS_REDACTION_KEY`. |
| `autoscaling` | EC2 Auto Scaling groups (name, ARN, min/max/desired capacity, Availability Zones, health-check type and grace period, status, capacity-rebalance, default cooldown, termination policies, service-linked role ARN, launch template/configuration reference, subnets, target groups, load balancer names), launch configurations (name and ARN only - never UserData, which can hold bootstrap secrets, and never block device mappings, security groups, key name, or instance profile), scaling policies (name, ARN, type, adjustment type, enabled, owning group - never step adjustments, target-tracking configuration, or CloudWatch alarm bindings), lifecycle hooks (name, transition, default result, timeouts, notification target ARN, role ARN - never NotificationMetadata free-form data), and scheduled actions (name, ARN, recurrence, time zone, target capacity, start/end time). Relationships: group-to-launch-template (`aws_ec2_launch_template` by launch-template ID, falling back to name), group-to-launch-configuration, group-to-subnet (`aws_ec2_subnet` by bare subnet ID parsed from `VPCZoneIdentifier`), group-to-target-group (`aws_elbv2_target_group` by target-group ARN), group-to-service-linked-IAM-role (`aws_iam_role` by role ARN), and policy/lifecycle-hook/scheduled-action-to-group (`aws_autoscaling_group` by group name). The Auto Scaling group resource_id is the bare group name, which closes the CodeDeploy `codedeploy_deployment_group_targets_auto_scaling_group` dangling edges. The scanner never creates, updates, or deletes an Auto Scaling resource, never sets desired capacity, and never terminates instances; the adapter read surface excludes those operations by construction, proven by a reflective guard test. No redaction key required. |
| `route53` | Hosted zones and A, AAAA, CNAME, ALIAS DNS record facts. |
| `route53resolver` | DNS resolution and DNS Firewall metadata, distinct from the `route53` hosted-zone scanner. Resolver endpoints (id, name, direction INBOUND/OUTBOUND, status, IP count), resolver rules (name, domain name, rule type FORWARD/SYSTEM/RECURSIVE - never forwarded target query data), resolver rule associations, DNS Firewall rule groups (name, AWS-reported rule count - NEVER the rule bodies), DNS Firewall domain lists (name, AWS-reported domain count - NEVER the domain entries), DNS Firewall rule group associations, and Resolver query log configurations (destination ARN only - never query log records). Relationships: endpoint-to-VPC and endpoint-to-subnet, rule-to-endpoint, rule-association-to-VPC and rule-association-to-rule, firewall-rule-group-association-to-VPC and firewall-rule-group-association-to-rule-group. Firewall rule and domain counts come from the per-resource Get reads, which return only metadata and an aggregate count; the scanner never calls ListFirewallDomains or ListFirewallRules and the adapter read surface excludes them by construction. Resolver endpoint IP address strings are never persisted; only the subnet placement and IP count survive. The scanner never mutates any resource. Requires no `ESHU_AWS_REDACTION_KEY`. |
| `sqs`, `sns`, `eventbridge` | Queue/topic/bus metadata and ARN-addressable relationships. |
| `guardduty` | Detectors, member accounts, filter names, publishing destinations, threat intel/IP set metadata, and aggregate finding counts. |
| `inspector2` | Account scan status, enabled scan features (EC2, ECR, Lambda, Lambda code) carried as account attributes, member accounts (org-admin view), findings filter non-criteria identity (name, action, owner ID), and CIS scan configuration metadata with member-to-administrator and CIS-configuration-to-target-account relationships. No finding details, no filter criteria expressions, no CIS scan results. |
| `macie2` | Highest-redaction scanner in the collector. Macie account session status (enabled/paused, finding publishing frequency, service-linked role ARN), member accounts (org-admin view, email-free), classification job metadata (id, name, type, status, and an aggregate bucket-criteria summary of `target_bucket_count` + `target_account_count` + `uses_bucket_criteria` — never the bucket list or criteria expressions), allow list identities (id, name only), custom data identifier identities (id, name only), findings filter identities (id, name, action only), and aggregate finding counts by severity carried on the session resource. Relationship: member-to-administrator (targets the administrator account's Macie session). Never persists sensitive-data findings (the PII detection results), custom data identifier regex bodies (they are the PII patterns), allow list contents, findings filter criteria, or classification-job bucket criteria. The scanner never calls `GetSensitiveDataOccurrences`, `GetSensitiveDataOccurrencesAvailability`, `GetFindings`, `ListFindings`, `GetCustomDataIdentifier`, `GetAllowList`, `GetFindingsFilter`, `DescribeClassificationJob`, or `DescribeBuckets`; the accepted SDK surface excludes them by construction, proven by a reflective guard test on the SDK adapter `apiClient` interface and struct-reflection tests on the scanner-owned types. |
| `s3` | Bucket metadata and server-access-log target bucket relationships. |
| `rds` | DB instances, clusters, subnet groups, and reported security/KMS/role/group relationships. |
| `docdb` | DocumentDB DB clusters, cluster instances, cluster parameter groups (name + family + parameter count only - NOT parameter values), cluster snapshot metadata, subnet groups, global clusters, and event subscription metadata with cluster-to-VPC, cluster-to-subnet-group, cluster-to-KMS-key, instance-to-cluster, and global-cluster-to-cluster relationships. No master user passwords, master user secrets, database document contents, collections, indexes, cluster parameter values, or snapshot contents. |
| `ds` | AWS Directory Service directories across all three types (AWS Managed Microsoft AD, Simple AD, AD Connector) including directory type, edition, size, stage/status, short name, alias, access URL, single-sign-on state, and VPC placement; trust relationships (id, direction, type, state, remote domain name, selective auth); shared-directory invitations (owner account, owner directory, shared account, shared directory, share method, share status); and LDAPS client-side settings status (Managed Microsoft AD only). The directory `resource_id` is the bare directory ID (`d-xxxxxxxxxx`), which is the join key the FSx scanner's AD-directory edges target. Relationships: directory-to-VPC (bare `vpc-id`, targets `aws_ec2_vpc`), directory-to-subnet (bare `subnet-id`, targets `aws_ec2_subnet`), trust-to-directory (bare directory id, targets `aws_ds_directory`), shared-directory-to-owner-directory (bare owner directory id, targets `aws_ds_directory`), and shared-directory-to-owner-account (bare 12-digit account id, targets `aws_account`, never a synthesized ARN). No directory admin passwords, RADIUS shared secrets, or AD Connector service-account credentials; the scanner never calls ResetUserPassword or any mutation API. |
| `neptune` | Both Neptune (provisioned) and Neptune Analytics share `service_kind=neptune`; resource types distinguish the two surfaces. Neptune (provisioned): DB clusters, cluster instances, cluster parameter groups (name + family only - NOT parameter values), cluster snapshot metadata, subnet groups, and global clusters. Neptune Analytics: graphs (name, status, vector-search embedding dimension, provisioning shape) and graph snapshot metadata. Relationships: cluster-to-VPC (derived from the subnet group VPC), cluster-to-subnet-group, cluster-to-KMS-key, cluster-to-IAM-role, instance-to-cluster, global-cluster-to-cluster, and graph-to-KMS-key. No master user passwords, master user secrets, cluster parameter values, graph vertex or edge contents, `ExecuteQuery` results, or snapshot contents. The Neptune Analytics graph data-plane (ExecuteQuery/CancelQuery/GetQuery/ListQueries) is unreachable from the scanner by interface construction. |
| `redshift` | Provisioned clusters, cluster parameter groups, cluster subnet groups, cluster snapshot metadata, scheduled action metadata, Serverless namespaces, Serverless workgroups, and reported VPC/subnet/security-group/KMS/IAM/snapshot/scheduled-action/namespace-workgroup relationships. Provisioned and Serverless share `service_kind=redshift`; resource types distinguish the two surfaces. |
| `dynamodb`, `cloudwatchlogs` | Table or log-group metadata and KMS relationships. |
| `efs` | File system metadata (performance mode, throughput mode, encryption status, lifecycle policy transition summary), access points, mount targets, and replication configurations with mount-target-to-subnet, mount-target-to-security-group, file-system-to-KMS-key, access-point-to-file-system, and replication-to-target-file-system relationships. No NFS file system policy bodies and no file contents. |
| `fsx` | File system metadata across all flavors (Windows File Server, Lustre, NetApp ONTAP, OpenZFS) including file system type, deployment type, storage type, storage and throughput capacity, lifecycle, and AWS Managed Microsoft AD directory ID; backups (type, lifecycle, size, source file system, KMS key); volume snapshots; NetApp ONTAP storage virtual machines; and NetApp ONTAP and OpenZFS volumes. Relationships: file-system-to-VPC, file-system-to-subnet, file-system-to-KMS-key, file-system-to-AD-directory, backup-to-file-system, SVM-to-file-system, SVM-to-AD-directory, volume-to-SVM, and volume-to-file-system. No Active Directory self-managed credentials (Windows/ONTAP self-managed AD password, service-account user name, administrators group, DNS server IPs, or domain-join secret ARN), no ONTAP fsxadmin password, no SVM admin password, and no file contents. |
| `cloudwatch` | Metric alarms, composite alarms, dashboards (name + last modified only), Contributor Insights rules (name + state only), and metric streams with alarm-to-SNS-topic, composite-alarm-to-child-alarm, metric-stream-to-Firehose, and alarm-to-metric (dimension summary) relationships. No dashboard body JSON, no Contributor Insights rule definitions, no metric data points. Customer-tag-named alarm dimensions are routed through the shared redact library. |
| `cloudfront` | Distribution metadata plus ACM certificate and WAF web ACL relationships. |
| `globalaccelerator` | Accelerator (name, ARN, status, IP address type, DNS name, dual-stack DNS name, static IP sets), listener (port ranges, protocol, client affinity), endpoint group (region, traffic dial percentage, health check protocol/path/port/interval, threshold, port overrides), and endpoint (endpoint id reference, weight, client IP preservation, health state) metadata. Relationships: accelerator-to-listener, listener-to-endpoint-group, endpoint-group-to-endpoint, and endpoint-to-target. The endpoint target edge is typed from the reported endpoint id: `aws_elbv2_load_balancer` for an `:elasticloadbalancing:` ARN, `aws_vpc_elastic_ip` for an `eipalloc-` allocation id, `aws_ec2_instance` for an `i-` instance id, and `aws_resource` otherwise; `target_arn` is set only when the endpoint id is ARN-shaped. Global Accelerator is a global-endpoint service whose control plane is reachable only in us-west-2, so schedule the claim for us-west-2; the SDK adapter pins its client region there. The scanner never mutates a Global Accelerator resource; the adapter read surface excludes Create/Update/Delete, BYOIP advertise/withdraw, and custom-routing traffic allow/deny by construction, proven by a reflective guard test on the SDK adapter `apiClient` interface. |
| `wafv2` | Web ACL, customer rule group, IP set (id, name, IP version, address count only), and regex pattern set (id, name, pattern count only) metadata for both the REGIONAL and global CLOUDFRONT scope, plus managed rule set references (vendor + name). Relationships: web-ACL-to-protected-resource (ALB, API Gateway stage, AppSync, App Runner service, Cognito user pool, Amplify, Verified Access), web-ACL-to-rule-group, web-ACL-to-IP-set, and web-ACL-to-regex-pattern-set. CloudFront associations are recorded by the `cloudfront` scanner. No IP set address lists, regex pattern bodies, or rule `Statement` bodies (`AndStatement`/`OrStatement`/`NotStatement`/`ByteMatchStatement` search strings) are read or persisted. WAF Classic (v1) is out of scope by construction; the scanner imports only the WAFv2 SDK. |
| `acm` | Public ACM certificate metadata (ARN, domain name, SANs, status, type, issuer, validity, key and signature algorithms) and certificate-to-using-resource relationships derived from ACM-reported in-use-by ARNs (ELB v2, CloudFront, API Gateway, AppSync, App Runner, and other ARN-shaped targets). No certificate body PEM, no private key material, no `GetCertificate` calls, no `ExportCertificate` calls; ACM Private CA is out of scope. |
| `appmesh` | Service mesh, virtual service, virtual node, virtual router, route, virtual gateway, and gateway route metadata with virtual-service-to-mesh, virtual-node-to-backend-virtual-service, route-to-virtual-router, virtual-gateway-to-mesh, virtual-node-to-ACM-Private-CA-certificate-authority (client TLS validation trust), and virtual-node-to-Cloud-Map-service / DNS-hostname relationships. The client TLS trust edge keys on the ACM Private CA (acm-pca) certificate authority ARN App Mesh reports and targets `aws_acmpca_certificate_authority`; there is no ACM Private CA scanner yet, so the target type is forward-looking. Route shape (path prefix, method, header names and match type) is recorded; sensitive HTTP header match values (Authorization, Cookie, X-Api-Key shaped) are routed through the shared redact library. No client TLS validation certificate bodies, certificate chains, or SDS secret names are read or persisted; client TLS validation is reduced to ACM Private CA certificate authority ARN references. No App Mesh mutation APIs. Requires `ESHU_AWS_REDACTION_KEY`. |
| `apprunner` | Services (name, ARN, status, source-configuration type IMAGE/REPOSITORY, image identifier or code repository URL, autoscaling configuration ARN, observability configuration ARN, health check, instance/access role ARNs, KMS key, VPC connector ARN, egress type, public-accessibility), connections (name, provider type, status), automatic scaling configurations (name, revision, min/max size, max concurrency, default flag), observability configurations (name, revision, trace vendor), VPC connectors (name, revision, subnets, security groups), and VPC ingress connections (name, status, domain name, service ARN, VPC endpoint and VPC id). Relationships: service-to-container-image (ECR image URI, targets `container_image`), service-to-App-Runner-connection, service-to-IAM-role (instance and ECR access role), service-to-KMS-key, service-to-VPC-connector, service-to-autoscaling-configuration, service-to-observability-configuration, service-to-secret-reference (Secrets Manager / SSM ARN ref only), vpc-connector-to-subnet, vpc-connector-to-security-group, and vpc-ingress-connection-to-service. The service `resource_id` is the service ARN, which closes the ACM and WAFv2 dangling edges that target `aws_apprunner_service` by service ARN. Metadata only: source repository credentials and runtime environment-variable values are never read; environment-variable NAMES are kept and runtime secrets are recorded as ARN reference edges only. The scanner never creates, deletes, updates, pauses, resumes, or deploys a service, never deletes a connection, and never mutates any App Runner resource; the SDK adapter read surface excludes those operations by construction, proven by a reflective guard test. No redaction key required. |
| `acm` | Public ACM certificate metadata (ARN, domain name, SANs, status, type, issuer, validity, key and signature algorithms) and certificate-to-using-resource relationships derived from ACM-reported in-use-by ARNs (ELB v2, CloudFront, API Gateway, AppSync, App Runner, and other ARN-shaped targets). No certificate body PEM, no private key material, no `GetCertificate` calls, no `ExportCertificate` calls. ACM Private CA is a separate scanner under `service_kind=acm-pca`. |
| `acm-pca` | ACM Private CA certificate authority metadata (CA ARN, owner account, type ROOT/SUBORDINATE, status, serial, usage mode, key storage security standard, key algorithm, signing algorithm, subject common name, validity, and CRL/OCSP revocation flags). The CA `resource_id` is the CA ARN, which is the join key App Mesh virtual-node client TLS trust edges target (`aws_acmpca_certificate_authority`). Relationships are ARN-driven and conditional: CA-to-KMS-key (target_type `aws_kms_key`, emitted only when AWS reports an ARN-shaped key), subordinate-CA-to-parent-CA (target_type `aws_acmpca_certificate_authority`, emitted only for a SUBORDINATE CA reporting an ARN-shaped parent), and CA-to-S3-CRL-bucket (target_type `aws_s3_bucket`, emitted only when CRL publishing names a bucket). The scanner never synthesizes a KMS, parent, or bucket identity; `DescribeCertificateAuthority` reports no KMS key or parent ARN, so those edges stay unemitted for the standard metadata response. No certificate issuance, no `GetCertificate`, no `GetCertificateAuthorityCsr` (CSR body), no `GetCertificateAuthorityCertificate` (certificate chain body), no private key material, and no CA mutation (Create/Delete/Update/Restore/Import/Revoke); the accepted SDK surface excludes them by construction, proven by a reflective guard test on the SDK adapter `apiClient` interface. Metadata only. |
| `appmesh` | Service mesh, virtual service, virtual node, virtual router, route, virtual gateway, and gateway route metadata with virtual-service-to-mesh, virtual-node-to-backend-virtual-service, route-to-virtual-router, virtual-gateway-to-mesh, virtual-node-to-ACM-Private-CA-certificate-authority (client TLS validation trust), and virtual-node-to-Cloud-Map-service / DNS-hostname relationships. The client TLS trust edge keys on the ACM Private CA (acm-pca) certificate authority ARN App Mesh reports and targets `aws_acmpca_certificate_authority`; the `acm-pca` scanner publishes certificate authority resources keyed by that same CA ARN, so the edge resolves. Route shape (path prefix, method, header names and match type) is recorded; sensitive HTTP header match values (Authorization, Cookie, X-Api-Key shaped) are routed through the shared redact library. No client TLS validation certificate bodies, certificate chains, or SDS secret names are read or persisted; client TLS validation is reduced to ACM Private CA certificate authority ARN references. No App Mesh mutation APIs. Requires `ESHU_AWS_REDACTION_KEY`. |
| `cloudtrail` | Trail (multi-region and per-region), Lake event data store, channel, and Lake dashboard configuration metadata with trail-to-S3-bucket, trail-to-CloudWatch-Logs, trail-to-KMS-key, trail-to-SNS-topic, and event-data-store-to-KMS-key relationships. Event selectors are summarized as counts only; CloudTrail event payloads, Lake query strings, Lake query results, and dashboard widget query SQL are never read or persisted. |
| `cloudformation` | Stack (active and recently deleted), stack set, change set (metadata only), drift detection result (status + per-status counts), stack-set instance, and registered extension type metadata with stack-to-resource-type (from ListStackResources), stack-set-to-instance, stack-to-IAM-role, and stack-to-S3-template-URL relationships. Highest template-body redaction surface in the collector: template bodies (`GetTemplate`/`GetTemplateSummary`), parameter values (including NoEcho and SSM-resolved values, only keys are kept), change-set bodies (`DescribeChangeSet`), stack policies (`GetStackPolicy`), and drift property documents are never read or persisted. Secret-like stack output values are routed through the shared redact library by output key; the stack-set `TemplateBody` is never carried. Requires `ESHU_AWS_REDACTION_KEY`. |
| `apigateway` | REST, HTTP, WebSocket, stage, custom-domain, mapping, access-log, ACM, and integration metadata. |
| `apigatewayv2` | Dedicated HTTP and WebSocket (API Gateway v2) coverage: API (id, name, protocol type, endpoint, disable-execute-api-endpoint), stage (name, auto-deploy), route (route key, route id, target, authorization summary - never request models or parameter mappings), integration (id, type, backend URI/target, connection metadata - never request/response templates, parameter mappings, or the integration credential ARN), authorizer (name, type, identity sources, JWT issuer/audience - never the Lambda authorizer invocation URI, the authorizer credential ARN, or the identity validation expression), custom domain (status, endpoint types, certificate ARNs, API mappings), and VPC link (name, status, subnet ids, security group ids). Relationships: API-to-stage, API-to-route, route-to-integration (derived from the `integrations/<id>` target), integration-to-Lambda-function (joined by function ARN), integration-to-HTTP-endpoint, integration-to-VPC-link, API-to-Cognito-user-pool (joined by the bare pool id parsed from the JWT issuer URL; a non-Cognito issuer becomes an `api-uses-jwt-issuer` edge instead of dangling), domain-to-ACM-certificate (joined by certificate ARN), domain-to-API, and VPC-link-to-subnet/security-group (joined by bare EC2 ids). The classic REST (v1) surface is owned by the `apigateway` scanner. No request/response mapping templates, route request models, authorizer secrets, Lambda authorizer payloads, or stage variable values are read or persisted; the SDK adapter cannot reach ExportApi, GetIntegrationResponse(s), GetRouteResponse(s), GetModel(s), GetModelTemplate, or any mutation API, and the scanner-owned types have no field for those bodies. |
| `appsync` | GraphQL API (id, name, authentication type, log config, X-Ray status, WAF ACL ref), data source (name, type, backing-resource target - never inline credentials or HTTP authorization config), resolver (type name + field name + kind + data-source name + runtime), pipeline function (name, data-source name, runtime), schema metadata (creation status + type count), and API key metadata (id, description, expiry) for each API. Relationships: API-to-data-source, resolver-to-data-source, function-to-data-source, data-source-to-Lambda/DynamoDB-table/OpenSearch-domain/HTTP-endpoint/RDS-cluster, API-to-Cognito-user-pool (joined by bare pool id), and API-to-OIDC-issuer. The schema SDL body (exposes the data model and PII field names), resolver request/response mapping templates (VTL or JS, often containing inline auth conditions), pipeline function code bodies, and API key values are never read or persisted; the SDK adapter cannot reach EvaluateMappingTemplate, EvaluateCode, GetIntrospectionSchema, StartSchemaCreation, or any mutation API, and the scanner-owned types have no field for those bodies. |
| `secretsmanager`, `ssm` | Secret or parameter metadata with KMS relationships; no secret/parameter values. |
| `athena` | Workgroup, data catalog, prepared-statement, and named-query metadata plus workgroup-to-S3-result-bucket, workgroup-to-KMS-key, prepared-statement-to-workgroup, and named-query-to-workgroup relationships. No SQL bodies, query results, query result location object contents, or query history strings. |
| `securityhub` | Hub configuration, enabled standards, controls, member accounts, action targets, insight summaries, and aggregate finding counts; no finding bodies or insight filters. |
| `cognito` | User pools (name, MFA configuration, Lambda trigger ARNs, password-policy summary, deletion protection; the Cognito API has deprecated the user-pool status field so it is not emitted), user pool app clients (id, name, allowed OAuth flows, callback and logout URLs, supported identity providers - never ClientSecret), identity providers (type and provider name only - never ProviderDetails secrets), resource servers, user pool groups, identity pools, and identity-pool role-attachment summaries, with user-pool-client-to-user-pool, user-pool-to-Lambda-trigger, identity-pool-to-user-pool, and identity-pool-to-identity-provider relationships. Covers both Cognito User Pools (`cognito-idp`) and Cognito Identity Pools (`cognito-identity`). The scanner never reads Cognito user records: ListUsers, AdminGetUser, AdminListGroupsForUser, and ListUsersInGroup are unreachable through the adapter. It never persists app-client secrets, identity-provider ProviderDetails (client_secret, google_client_secret, and similar), custom message Lambda templates, or MFA SMS configuration access tokens. Requires `ESHU_AWS_REDACTION_KEY`. |
| `glue` | Data Catalog database, table, crawler, job, trigger, workflow, and connection metadata plus table-in-database, table-to-S3-location, crawler-to-database, crawler-to-IAM-role, job-to-IAM-role, and trigger-to-job relationships. No script bodies, default-argument values, connection passwords, JDBC credential URLs, workflow graphs, table column sample statistics, or classifier custom patterns. |
| `elasticache` | Cache clusters, replication groups, parameter and subnet groups, users, user groups, and snapshot metadata (name/source/status only); cluster-to-VPC, cluster-to-subnet, cluster-to-KMS, replication-group-to-cluster, and user-group-to-user relationships. No AUTH tokens, user passwords, user access strings, cache contents, or snapshot data. |
| `memorydb` | Managed Redis-compatible store (mirrors the ElastiCache shape). Clusters (name, ARN, status, node type, engine version, num shards/replicas, encryption/TLS status, ACL name), subnet groups, parameter groups (name, family, description, and tags - NOT parameter values), users (name, authentication type, password count, and a non-secret access-string-present summary - never the access string or passwords), ACLs (identity, status, member user names), and snapshot metadata (name/source/status only); cluster-to-subnet-group, cluster-to-KMS-key, cluster-to-SNS-topic, and ACL-to-user relationships. No user passwords, AUTH tokens, user access strings, cache contents, or snapshot data. The accepted SDK surface excludes every Create/Delete/Update/Modify/Reset/Failover mutation by construction, proven by a reflective guard test on the SDK adapter `apiClient` interface plus struct-reflection tests on the scanner-owned User, ACL, and snapshot types. |
| `opensearch` | OpenSearch Service provisioned domains (engine version, instance type/count, dedicated master, zone awareness, at-rest and node-to-node encryption status, fine-grained access control summary, VPC config) and OpenSearch Serverless collections (name, type, status, KMS key), security configuration summaries (id, type, version), and managed VPC endpoints under one `service_kind`, plus custom package metadata (name, type, status - never the package body). Relationships: domain-to-VPC, domain-to-subnet, domain-to-security-group, domain-to-KMS-key, domain-to-IAM-role (master-user mapping/access-policy role refs), package-to-domain, and collection-to-KMS-key. No collection-to-VPC-endpoint edge is emitted: Serverless reports no collection-to-endpoint binding in either record, so a per-endpoint edge would be a misleading cross-product. The scanner never reaches the OpenSearch HTTP API (`_search`, `_msearch`, `_index`, `_doc`, `_bulk`, and similar) - that API is reachable only over the domain HTTP endpoint, which the scanner never constructs, and the SDK adapter interface carries no such method (nor `GetIndex` on either service). It never persists master user passwords (`DescribeDomains` does not return the password and the domain model has no password-shaped field), domain endpoint contents, the `Endpoints` map, access policy bodies, custom package bodies, or serverless saved-object bodies. |
| `msk` | MSK cluster, broker configuration, and replicator metadata with subnet, security-group, KMS-key, IAM-role, and configuration relationships; no broker `server.properties` bodies, broker logs, bootstrap broker endpoints, SCRAM secrets, or Kafka topic data. |
| `mq` | Amazon MQ broker and broker-configuration metadata for ActiveMQ and RabbitMQ engines (name, engine type/version, deployment mode, instance type, status, security-group refs, encryption options, broker usernames) with broker-to-subnet, broker-to-security-group, broker-to-KMS-key (customer-managed only), broker-to-configuration, and broker-to-CloudWatch-log-group relationships; no broker user passwords, configuration XML bodies, or queue/topic message contents. |
| `kinesis` | Kinesis Data Streams (name, shard count, retention, stream mode, encryption), Kinesis Data Firehose delivery streams (name, source, destination type, encryption), and Kinesis Video Streams (name, status, KMS key, retention) under one `service_kind`; resource types distinguish the three sub-services. Relationships: data-stream-to-KMS-key, video-stream-to-KMS-key, Firehose-to-S3/Redshift/OpenSearch/Splunk/HTTP-endpoint, Firehose-to-Lambda-transform, and Firehose-to-IAM-role. No stream records, no video media fragments, no Firehose processing-configuration Lambda body, no HTTP endpoint access keys, no Splunk HEC tokens, and no Redshift passwords. |
| `codebuild` | Build projects (name, source type/location, environment image/compute type, timeout), report groups (type, status, S3 export destination), and recent builds (id, status, phase, duration - never build logs) with project-to-IAM-role, project-to-VPC/subnet/security-group, project-to-KMS-key, project-to-S3-source, project-to-Git-source (GitHub/GitHub Enterprise/CodeCommit/Bitbucket/GitLab), project-to-S3-artifact, project-to-Secrets-Manager-secret, and project-to-SSM-Parameter-Store-parameter relationships. Never persists buildspec.yml bodies (`Source.Buildspec`/`BuildspecOverride`), environment-variable PLAINTEXT values (name and type only; the value is routed through the shared redact library), build logs, or source-credential tokens. PARAMETER_STORE and SECRETS_MANAGER environment variables keep only their parameter name or secret reference to drive relationship edges. The adapter never calls CreateProject, UpdateProject, DeleteProject, StartBuild, StopBuild, RetryBuild, BatchDeleteBuilds, ImportSourceCredentials, or DeleteSourceCredentials. Requires `ESHU_AWS_REDACTION_KEY`. |
| `codedeploy` | Applications (name, compute platform), deployment groups (deployment style, auto-rollback config, EC2/on-premises tag-filter summaries), deployment configurations (minimum-healthy-hosts), and recent deployments (id, status, revision summary) with deployment-group-to-application, deployment-group-to-IAM-role, deployment-group-to-Auto-Scaling-group, deployment-group-to-ECS-service, deployment-group-to-Lambda-function, and deployment-group-to-SNS-topic relationships. Never persists appspec.yml bodies (the scanner-owned types have no field for the revision content; only revision-source references are kept); on-premises instance tag values route through the shared redact library. The adapter never creates, updates, deletes, or starts deployments or any CodeDeploy resource. Requires `ESHU_AWS_REDACTION_KEY`. |
| `stepfunctions` | State machine and activity metadata, execution-role relationships, and ARN-only Task-target relationships; no execution payloads, history events, task tokens, or definition literals. |
| `backup` | Backup vault, backup plan, backup selection, recovery point (metadata only - id, source resource ARN, vault, status, creation/expiration), report plan, restore testing plan, and framework metadata with plan-to-selection, selection-to-resource, selection-to-IAM-role, vault-to-KMS-key, recovery-point-to-vault, recovery-point-to-source-resource, and framework-to-control relationships. No recovery point contents, vault access policy bodies, or framework control input parameter values. |
| `accessanalyzer` | Analyzer metadata, archive-rule names, aggregate finding counts, relationships, and unused-access summaries. |
| `kms` | Customer master keys, aliases, and grants with alias-to-key, grant-to-key, and grant-to-grantee-principal relationships. The scanner never calls cryptographic operations (Encrypt, Decrypt, GenerateDataKey, Sign, Verify, ReEncrypt, GenerateMac, VerifyMac, GenerateDataKeyPair, GenerateDataKeyWithoutPlaintext, DeriveSharedSecret, GetPublicKey) or key lifecycle mutations (CreateKey, ScheduleKeyDeletion, EnableKey, DisableKey, EnableKeyRotation, DisableKeyRotation, PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant, ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial). Key policy Statement bodies, grant encryption contexts, and key material stay outside the scan slice. |
| `codeartifact` | Package-registry domains (name, ARN, owner account, encryption-key ARN, S3 asset-bucket ARN, repository count, asset size, status) and repositories (name, ARN, owning domain, administrator account, external connections, upstream repositories). Relationships: repository-to-domain (targets `aws_codeartifact_domain` by the domain name the domain resource publishes as its `resource_id`), domain-to-KMS-key (targets `aws_kms_key` by the encryption-key ARN AWS reports, ARN-keyed so it joins the KMS key node, emitted only when an ARN-shaped key is reported), repository-to-upstream-repository (targets `aws_codeartifact_repository` by the `<domain>/<upstream>` identity within the same domain), and repository-to-external-connection (targets the labeled non-AWS `public_package_registry` identity, for example `public:npmjs`, with no synthesized ARN). The domain `resource_id` is the domain name; the repository `resource_id` is the repository ARN with the `<domain>/<name>` identity and bare name as correlation anchors. Metadata only: the scanner uses only ListDomains, DescribeDomain, ListRepositories, and DescribeRepository; it never reads, downloads, publishes, copies, or deletes a package version or package asset (no GetPackageVersionAsset, GetPackageVersionReadme, ListPackages, ListPackageVersions, PublishPackageVersion, or CopyPackageVersions), proven by reflection guard tests on the scanner-owned `Client` and the SDK adapter `apiClient` interfaces. API-reported ARNs are used directly so they stay partition-aware. No redaction key required. |
| `organizations` | Organization root, OUs, accounts, policy summaries, policy target bindings, and delegated administrators. |
| `ssoadmin` | IAM Identity Center instances, permission sets (name, description, session duration, relay state), account assignments (principal type/id, permission set, account), application instances, trusted token issuers, and group/user principals resolved from the connected identity store. Relationships cover assignment-to-permission-set, assignment-to-account, assignment-to-principal, permission-set-to-managed-policy (ARN ref), and permission-set-to-customer-managed-policy (name ref). No permission set inline policy bodies, permissions boundary bodies, customer-managed policy bodies, or application access-scope group filters. Principal display names are redacted; only the identity store display name is read. |
| `sagemaker` | Notebook instances, models, endpoints, endpoint configs, training jobs, processing jobs, transform jobs, hyperparameter tuning jobs, projects, pipelines, feature groups (metadata only), Studio domains, user profiles, apps, and inference components, with model-to-S3-artifact, model-to-container-image-URI, model-to-IAM-role, endpoint-to-endpoint-config, endpoint-config-to-model, training-job-to-IAM-role, notebook-to-subnet, domain-to-VPC, and user-profile-to-domain relationships. Metadata only: the scanner never invokes endpoints (InvokeEndpoint / InvokeEndpointAsync) and never persists hyperparameter values, training/processing/transform input or output data references, notebook lifecycle-config script bodies, container environment maps, or pipeline definition bodies. |
| `config` | AWS Config configuration recorders (recorded resource-type scope, recording strategy - never recorded configuration item bodies), delivery channels (S3 bucket, optional SNS topic, snapshot delivery interval), config rules (name, ARN, owner: AWS managed / CUSTOM_LAMBDA / CUSTOM_POLICY, managed-rule source identifier or custom-Lambda function ARN, and resource-type scope carried as a rule attribute), conformance packs (name, deployment status, member-rule count), configuration aggregators (source accounts and regions, or organization role), and retention configurations. Relationships: conformance-pack-to-rule (targets the `aws_config_rule` node by rule name), custom-rule-to-Lambda-function (targets the `aws_lambda_function` ARN), and aggregator-to-source-account (targets the `aws_account` root ARN with the partition derived from the aggregator ARN, never a hardcoded `arn:aws:`). The rule resource-type scope is a rule attribute, not an edge to a synthetic resource-type node. Metadata only: the scanner never calls GetResourceConfigHistory, GetComplianceDetailsByConfigRule, GetDiscoveredResourceCounts, or BatchGetResourceConfig, never persists recorded configuration item bodies (full resource snapshots), and never fetches custom-rule Lambda code (GetCustomRulePolicy). Aggregate per-rule compliance is used only to derive the conformance pack member-rule set and count. |
| `bedrock` | Foundation model availability (read-only list), custom models, model customization jobs, provisioned model throughputs, guardrails, agents, agent action groups, and knowledge bases, with custom-model-to-base-model, custom-model-to-S3-output, custom-model-to-customization-job, provisioned-throughput-to-model, agent-to-foundation-model, agent-to-knowledge-base, agent-to-action-group, action-group-to-Lambda-function, and knowledge-base-to-S3/Confluence/SharePoint/web-crawler relationships. Metadata only: the scanner never calls bedrock-runtime (InvokeModel, Converse) or bedrock-agent-runtime (InvokeAgent, Retrieve, RetrieveAndGenerate), and never persists agent instructions (system prompts), prompt-override configurations, guardrail topic or content policy bodies, knowledge base ingested document content, action-group API schema bodies, or custom-model hyperparameter and training-input data. |
| `ram` | Resource shares the account owns (name, ARN, status, allowExternalPrincipals, owning account id, feature set), the resources each share shares (ARN + RAM `service-code:resource-code` type), the principals each share targets (account id / Organizations OU ARN / organization or root ARN), and the managed permissions each share uses (name, ARN, version, type - never the permission policy document body). Relationships: share-to-shared-resource (targets the shared resource ARN with its reported resource type), share-to-principal-account (targets `aws_organizations_account` by bare account id, joining the organizations scanner for cross-account truth), share-to-principal-OU (targets `aws_organizations_organizational_unit` by OU ARN), share-to-principal-organization (targets `aws_organizations_root` by organization or root ARN), and share-to-permission (targets `aws_ram_permission` by permission ARN). Owner-account inventory only (resource owner SELF). Metadata only: the scanner never calls GetPermission (which returns the permission policy document body) and never mutates a RAM resource (no Create/Delete/Update, Associate/Disassociate, Accept/Reject, Enable/Disable, Promote/Replace, Tag/Untag, or SetDefaultPermissionVersion); the adapter read surface excludes those operations by construction. Pairs with the `organizations` scanner. |
| `servicediscovery` | AWS Cloud Map (Service Discovery) namespaces (id, name, type DNS_PUBLIC/DNS_PRIVATE/HTTP, service count, backing Route 53 hosted-zone id for DNS namespaces, HTTP discovery name for HTTP namespaces) and services (id, name, parent namespace id/name, DNS config summary of routing policy and DNS record types/TTLs, and instance COUNT only). Relationships: service-to-namespace (keyed by the Cloud Map namespace id) and namespace-to-Route53-hosted-zone (keyed by the `/hostedzone/<id>` resource id the `route53` scanner emits, DNS namespaces only). The Cloud Map service resource is keyed by its `namespaceName/serviceName` identity with resource type `aws_cloud_map_service`, which resolves the App Mesh virtual-node-to-Cloud-Map-service edge that targets the same namespace/service join key. No namespace-to-VPC edge is emitted: Cloud Map's read API does not report the VPC for a private DNS namespace, so the VPC is reached transitively through the private Route 53 hosted zone the `route53` scanner owns. Instance attribute maps are NEVER read or persisted because they can carry caller-defined secrets; the instance count comes from the service summary. The scanner never calls a Cloud Map mutation API (Create/Update/Delete namespace or service, RegisterInstance, DeregisterInstance, UpdateInstanceCustomHealthStatus, TagResource, UntagResource) and never calls an instance discovery/read API (ListInstances, GetInstance, GetInstancesHealthStatus, DiscoverInstances, DiscoverInstancesRevision); the accepted SDK surface excludes them by construction, proven by a reflection guard test on the adapter interface. |

IAM, Route 53, and CloudFront are global-style families. Use a concrete global
region label such as `aws-global` so claims keep the
`(account_id, region, service_kind)` shape. Organizations and Identity Center
(`ssoadmin`) use the `us-east-1` control-plane endpoint and require
management-account or delegated-administrator credentials. Global Accelerator
(`globalaccelerator`) reports global accelerators but its control plane is
reachable only at the `us-west-2` endpoint, so schedule a `us-west-2` claim; the
SDK adapter pins its client region to `us-west-2` regardless of the claim
region.

WAFv2 is dual-scope: a regional claim scans the REGIONAL scope for its region,
and an `aws-global` claim scans the global CLOUDFRONT scope through the
us-east-1 control-plane endpoint. Schedule both shapes to cover regional and
CloudFront web ACLs.

## Data Boundaries

The collector does not read S3 object contents, SQS messages, DynamoDB table
data, EFS file contents or NFS file system policy bodies, FSx file contents,
FSx Active Directory self-managed credentials (the Windows and ONTAP
self-managed AD password, service-account user name, administrators group, DNS
server IPs, and domain-join secret ARN), FSx ONTAP fsxadmin passwords, FSx
storage virtual machine admin passwords, RDS database
contents, DocumentDB database document contents, DocumentDB collections,
DocumentDB indexes, DocumentDB cluster parameter values, DocumentDB master
user passwords or secrets, DocumentDB snapshot
contents, Neptune master user passwords or secrets, Neptune cluster parameter
values, Neptune snapshot contents, Neptune Analytics graph vertex or edge
contents, Neptune Analytics `ExecuteQuery` results, Redshift warehouse queries, Redshift table data,
Redshift snapshot contents, Redshift master user passwords or admin passwords,
ElastiCache cache keys, cache values, AUTH tokens, user passwords, user access
strings, or snapshot data, MemoryDB cache keys, cache values, AUTH tokens, user
passwords, user access strings, or snapshot data, CloudWatch log events, CloudWatch metric data
points, CloudWatch dashboard body JSON, CloudWatch Contributor Insights rule
definitions, Secrets Manager secret
values, SSM parameter values, API Gateway execution payloads, Lambda code
packages, CloudFront origin payloads, private keys, raw SNS endpoints, raw
EventBridge target inputs, Athena query result rows, Athena named-query SQL
bodies, Athena prepared-statement query bodies, Athena query history strings,
Glue job script bodies, Glue default-argument values, Glue connection
passwords or JDBC credential URLs, Glue workflow graph payloads, Glue table
column statistics with sample values, Glue classifier custom patterns, MSK
Kafka topic or message data, MSK broker logs, MSK broker `server.properties`
bodies, MSK configuration revision bodies, MSK bootstrap broker endpoints, MSK
SCRAM secret material, Amazon MQ broker user passwords, Amazon MQ
configuration XML bodies, Amazon MQ queue or topic message contents, Step
Functions execution input or output, Step
Functions execution history events, Step Functions activity task tokens, AWS
Backup recovery point contents, AWS Backup vault access policy bodies, AWS
Backup framework control input parameter values, AWS Backup recovery-point
restore metadata values, or IAM/resource policy JSON unless a service
package explicitly documents a sanitized metadata-only exception. Step Functions state machine definitions
are persisted only as state names, state types, structural transitions, and
Task Resource ARNs; Parameters, ResultPath, ResultSelector, InputPath,
OutputPath, and Result literal contents are excluded.
GuardDuty finding bodies, GuardDuty filter criteria, GuardDuty threat intel set
list contents, and GuardDuty IP set list contents are also out of scope.
Inspector v2 finding details (CVE, package version, affected host ARN),
Inspector v2 filter criteria expressions, filter descriptions, filter reasons,
and CIS scan results are out of scope; the Inspector v2 scanner emits account
status, enabled scan features, member accounts, filter names, and CIS scan
configuration metadata only, and makes no finding-listing or finding-aggregation
call.

Amazon Macie is the highest-redaction scanner in the collector because Macie's
product is detecting personally identifiable information. Macie sensitive-data
findings (the PII detection results themselves), custom data identifier regular
expression bodies (which are themselves descriptions of the sensitive data the
customer is detecting), allow list contents, findings filter criteria, and
classification-job bucket-criteria expressions and explicit bucket lists are out
of scope. The Macie scanner never calls `GetSensitiveDataOccurrences`,
`GetSensitiveDataOccurrencesAvailability`, `GetFindings`, `ListFindings`,
`GetCustomDataIdentifier`, `BatchGetCustomDataIdentifiers`,
`TestCustomDataIdentifier`, `GetAllowList`, `GetFindingsFilter`,
`DescribeClassificationJob`, `DescribeBuckets`, or any mutation API; the
accepted SDK surface excludes them by construction, proven by a reflective guard
test on the SDK adapter `apiClient` interface and by struct-reflection tests on
the scanner-owned types, which carry identity and counts only and have no field
able to hold a regex body, list contents, finding detail, or criteria. The
scanner emits the Macie session status, member accounts (email-free),
classification-job metadata with an aggregate bucket-criteria summary count,
allow list and custom data identifier identities, findings filter identities,
and aggregate finding counts by severity only. Member email addresses are
personal contact data and are never read.
CloudTrail audit event payloads, Lake query strings, Lake query result rows,
event selector bodies, and dashboard widget query SQL stay outside the
collector contract; the CloudTrail scanner emits trail and Lake configuration
only, summarizing selectors as bounded counts.
AWS Config recorded configuration item bodies (full resource snapshots),
per-resource compliance evaluation result bodies, and custom-rule Lambda code
are out of scope. The Config scanner emits configuration recorder, delivery
channel, config rule, conformance pack, configuration aggregator, and retention
configuration metadata only; it never calls `GetResourceConfigHistory`,
`BatchGetResourceConfig`, `GetDiscoveredResourceCounts`,
`GetComplianceDetailsByConfigRule`, `GetComplianceDetailsByResource`,
`GetCustomRulePolicy`, or any `Put`/`Delete`/`Start`/`Stop` mutation.
Aggregate per-rule compliance from `DescribeConformancePackCompliance` is read
only to derive the conformance pack member-rule set and count. The config rule
resource-type scope is carried as a rule attribute; it is not emitted as an edge
to a synthetic resource-type node. Synthesized account root ARNs for the
aggregator-to-source-account edge derive the partition from the aggregator ARN,
so GovCloud and China edges stay correct.

Transit gateway routes, transit gateway multicast group memberships, and
transit gateway policy table rule bodies are out of scope. The Transit Gateway
scanner reads transit gateway, route table, attachment, peering attachment,
multicast domain, and policy table identity, ownership, state, and bounded
option metadata only; it never calls `SearchTransitGatewayRoutes` or
`GetTransitGatewayPolicyTableEntries`, and never mutates transit gateway
resources or changes route-table association or propagation. Cross-account peer
transit gateway identities are surfaced exactly as AWS reports them and flagged
for org-context resolution; the scanner does not resolve the remote account.

CloudFormation is the highest template-body redaction surface in the collector.
Stack and stack-set template bodies, parameter values (including NoEcho and
SSM-resolved values), change-set bodies, stack policies, and drift property
documents are out of scope. The scanner never calls `GetTemplate`,
`GetTemplateSummary`, `DescribeChangeSet`, `GetStackPolicy`, or any
`Detect*Drift` or mutation API; the accepted SDK surface excludes them by
construction, proven by reflective guard tests on both the scanner-owned
`Client` interface and the SDK adapter `apiClient` interface. Parameter mapping
keeps keys only, the stack-set `TemplateBody` is never carried, drift results
are reduced to per-status counts, and secret-like stack output values are
redacted by output key through the shared redact library. Stack name, ARN,
status, capabilities, role ARN, tags, and timestamps are persisted as
metadata.

ACM certificate body PEM and ACM-issued private key material are out of scope.
The ACM scanner never calls `GetCertificate` or `ExportCertificate`, and ACM
Private CA (acm-pca) APIs are not exercised.

AppSync schema SDL bodies, resolver request/response mapping templates (VTL or
JS), pipeline function code bodies, and API key values are out of scope. The
schema SDL exposes the data model and PII field names; resolver mapping
templates often contain inline authorization conditions; function code is
customer IP; and API key values are bearer credentials. The AppSync scanner
reaches the control plane through `ListGraphqlApis`, `ListDataSources`,
`ListTypes`, `ListResolvers`, `ListFunctions`, `ListApiKeys`, and
`GetSchemaCreationStatus` only. It never calls `EvaluateMappingTemplate`,
`EvaluateCode`, `GetIntrospectionSchema`, `StartSchemaCreation`,
`GetDataSourceIntrospection`, or any mutation API such as
`CreateGraphqlApi`/`UpdateGraphqlApi`/`DeleteGraphqlApi`,
`Create`/`Update`/`Delete` of a resolver, data source, function, or API key. The
accepted SDK surface and the scanner-owned types exclude those bodies by
construction, proven by reflective guard tests on the SDK adapter interface and
a struct-reflection test on the scanner types. The schema is reduced to a
creation status and a type count; type definitions (`Type.Definition`) are never
read. Data-source backing-resource targets (Lambda, DynamoDB, OpenSearch, HTTP,
RDS) are recorded as relationship evidence without the HTTP endpoint
authorization config or the RDS secret store ARN.

KMS key policy Statement bodies, KMS grant encryption contexts, KMS key
material, and the output of any KMS cryptographic operation are out of scope.
The KMS scanner reaches the control plane only through List- and
Describe-class APIs; it never calls Encrypt, Decrypt, GenerateDataKey,
GenerateDataKeyPair, GenerateDataKeyPairWithoutPlaintext,
GenerateDataKeyWithoutPlaintext, Sign, Verify, ReEncrypt, GenerateMac,
VerifyMac, DeriveSharedSecret, GetPublicKey, GenerateRandom, or any key
lifecycle mutation. Only the bounded list of policy revision names from
ListKeyPolicies is persisted; the scanner does not call GetKeyPolicy.

WAFv2 IP set address lists, regex pattern set bodies, and rule `Statement`
bodies are out of scope. The WAFv2 scanner persists the IP set address count
and IP version, the regex pattern count, web ACL and rule group rule counts,
managed rule set references (vendor and name), and reference ARNs only. IP set
addresses (which commonly include private CIDR and threat intel), regex
detection bodies, and `Statement` search strings
(`AndStatement`/`OrStatement`/`NotStatement`/`ByteMatchStatement`) are never
read into facts. The scanner reaches the control plane through List- and
Get-class APIs only; it never calls a mutation API such as `CreateWebACL`,
`UpdateWebACL`, `DeleteWebACL`, `AssociateWebACL`, `DisassociateWebACL`, any
`Create`/`Update`/`Delete` rule group, IP set, or regex pattern set operation,
or `PutLoggingConfiguration`. WAF Classic (v1) is out of scope by construction:
the scanner imports only the WAFv2 SDK, which cannot surface `waf` or
`waf-regional` v1 resources.

Security Hub finding aggregate counts are metadata-only when grouped by bounded
posture fields such as severity, standard, control, compliance status, and
workflow status. Security Hub finding bodies, resource IDs from findings,
resource details, remediation text, product fields, user-defined fields, note
text, network/process details, and insight filter expressions remain outside
the collector contract.

Cognito user records are PII and are out of scope. The Cognito scanner never
calls ListUsers, AdminGetUser, AdminListGroupsForUser, or ListUsersInGroup;
those methods are absent from both the scanner `Client` interface and the SDK
adapter's API interfaces, enforced by reflection tests. App-client ClientSecret,
identity-provider ProviderDetails (client_secret, google_client_secret, and
similar federation secrets), custom message Lambda templates, MFA SMS
configuration access tokens, and identity-pool role-mapping rule bodies are
never persisted. The scanner reports control-plane metadata only and routes
operator-supplied free text (identity-pool developer provider names and group
descriptions) through the shared AWS redaction path.

Organizations policy attachment metadata is in scope: policy ID, policy name,
policy type, and target binding. Policy document bodies, statements,
conditions, action lists, and guardrail text are out of scope by default.
Account email and account name values must pass through the shared AWS
redaction path before persistence.

IAM Identity Center (`ssoadmin`) permission set metadata is in scope: name,
description, session duration, relay state, and managed-policy ARN or
customer-managed-policy name references. Permission set inline policy bodies
(`GetInlinePolicyForPermissionSet`) and permissions boundary bodies
(`GetPermissionsBoundaryForPermissionSet`) are out of scope by default; they
encode the org least-privilege model and live in IAM. Customer-managed policy
bodies are never read; only the attachment name and path are persisted.
Application instance access-scope attributes (`GetApplicationAccessScope`,
`ListApplicationAccessScopes`) are out of scope because they can carry
sensitive group filters. Principal display names resolved from the connected
identity store pass through the shared AWS redaction path; only the identity
store display name is read, never addresses, emails, phone numbers, structured
name, or group memberships.

Kinesis stream records (PutRecord, PutRecords, GetRecords, GetShardIterator
class), Kinesis Video media fragments (GetMedia, PutMedia,
GetMediaForFragmentList), the Firehose processing-configuration Lambda body,
Firehose HTTP endpoint access keys, Firehose Splunk HEC tokens, and Firehose
Redshift passwords or SecretsManager material stay outside the collector
contract. The Kinesis scanner reaches the three sub-service control planes only
through List- and Describe-class APIs and never mutates a data stream, delivery
stream, or video stream.

It also does not call AWS mutation APIs. If a scanner needs a new API family,
update the owning service package README with source APIs, forbidden data
classes, emitted evidence, and verification.

Access Analyzer has an extra security boundary: external finding bodies,
archive-rule filter criteria, policy-generation results, and per-action
unused-access details are not persisted. The scanner keeps aggregate finding
counts by status and resource type, plus per-resource unused-access
last-accessed timestamps.

SageMaker is a metadata-only control-plane scan. The scanner never invokes
endpoints (it never calls InvokeEndpoint or InvokeEndpointAsync, which live in
the separate `sagemakerruntime` API the collector does not link) and never
calls a mutation API. It never persists hyperparameter values (training or
tuning), training/processing/transform input or output data references,
notebook lifecycle-config script bodies, container environment maps, or
pipeline definition bodies. These exclusions are enforced by omission from the
scanner-owned types and by a reflection gate over the SDK adapter that fails
the build if a forbidden method ever becomes reachable.

OpenSearch is a metadata-only control-plane scan that never reaches the
OpenSearch HTTP index/search/data API (`_search`, `_msearch`, `_index`, `_doc`,
`_bulk`, and similar). That API is reachable only over the domain HTTP endpoint,
which the scanner never constructs; the SDK adapter's two interfaces
(`domainAPI` and `serverlessAPI`) carry control-plane reads only and no
`GetIndex` method on either service. A reflection gate over both interfaces
fails the build if a mutation verb, inbound-connection acceptance, or any
index/search/data method ever becomes reachable. Master user passwords are out
of scope: `DescribeDomain` does not return the password, and the scanner-owned
`Domain` type has no password-shaped field, enforced by a struct-shape test.
Domain endpoint contents, the domain `Endpoints` map, the access policy body,
custom package bodies, and serverless saved-object bodies are never persisted.
Only IAM role ARNs referenced by the domain access policy are resolved for
domain-to-IAM-role relationship evidence; the policy body itself is dropped.

Bedrock is a high-redaction metadata-only control-plane scan over the
`bedrock` and `bedrock-agent` SDKs. The collector never links the inference
data-plane modules: it never calls bedrock-runtime (InvokeModel,
InvokeModelWithResponseStream, Converse, ConverseStream) or bedrock-agent-runtime
(InvokeAgent, Retrieve, RetrieveAndGenerate), and it never calls a mutation API.
Three high-IP surfaces are filtered: agent instructions (the system prompt) and
prompt-override configurations, guardrail topic and content policy bodies, and
knowledge base ingested document content and chunks. The scanner also drops
action-group API schema bodies and custom-model hyperparameter and training-input
data. These exclusions are enforced three ways: the scanner-owned types have no
field for any forbidden payload (proven by a struct-reflection gate), the SDK
adapter never calls the operations that return policy bodies or ingested content
(GetGuardrail, GetKnowledgeBaseDocuments), and a reflection gate over both SDK
adapter interfaces fails the build if any inference or mutation method ever
becomes reachable.

AWS Batch is a metadata-only control-plane scan. The scanner never submits,
cancels, or terminates jobs, never registers or deregisters job definitions,
and never mutates a compute environment, job queue, job definition, or
scheduling policy. Those operations are absent from both the scanner `Client`
interface and the SDK adapter's API interface, enforced by a reflection gate
that fails the build if a forbidden method becomes reachable. Job-definition
container command lists (`ContainerProperties.Command`) and job parameters are
never read: the scanner-owned `Container` and `JobDefinition` types do not
declare those fields. Container environment values may carry secrets, so they
are never persisted in clear text; they route through the shared AWS redaction
path and persist only as HMAC markers, which is why the scanner requires
`ESHU_AWS_REDACTION_KEY`. Container secret references persist only as the
Secrets Manager or SSM Parameter Store ARN reference (a relationship edge); the
resolved secret value is never read. Scheduling policy fair-share weight state
(`FairsharePolicy`) is never persisted because priority weights may reveal
business-sensitive tenant ranking. Recent jobs are listed only across active
states (SUBMITTED, PENDING, RUNNABLE, STARTING, RUNNING), bounded per state, and
persist identity, status, and the job-definition reference only.

## Evidence And Telemetry

Scanner packages emit reported `aws_resource`, `aws_relationship`,
`aws_image_reference`, `aws_dns_record`, and `aws_warning` facts. Reducers must
corroborate them before promoting workload, deployment, ownership, drift, or
unmanaged-resource truth.

Runtime spans include `aws.collector.claim.process`,
`aws.credentials.assume_role`, `aws.service.scan`, and
`aws.service.pagination.page`. The metric catalog lives in
[Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md).

## Change Rules

When adding or widening a scanner, keep it metadata-only unless an active design
record says otherwise. Add scanner tests, SDK-adapter tests, command-side target
validation tests, and registry support through `awsruntime.SupportedServiceKinds`.
Run the performance evidence gate when the scanner adds pagination fanout, claim
concurrency, batch sizing, queue pressure, or downstream reducer work.

## Related Docs

- [AWS Cloud Collector](collector-aws-cloud.md)
- [AWS Collector Security And Config](collector-aws-cloud-security.md)
- [Collector Runtime Services](../deployment/service-runtimes-collectors.md)
