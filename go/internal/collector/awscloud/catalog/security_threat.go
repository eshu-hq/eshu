// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Security-threat-category constants consolidated from constants_guardduty.go (#6696).

const (
	// ServiceGuardDuty identifies the regional Amazon GuardDuty metadata scan
	// slice.
	ServiceGuardDuty = "guardduty"
)

const (
	// ResourceTypeGuardDutyDetector identifies a GuardDuty detector metadata
	// resource.
	ResourceTypeGuardDutyDetector = "aws_guardduty_detector"
	// ResourceTypeGuardDutyMemberAccount identifies a GuardDuty member account
	// reported by an administrator detector.
	ResourceTypeGuardDutyMemberAccount = "aws_guardduty_member_account"
	// ResourceTypeGuardDutyFilter identifies a GuardDuty saved filter summary.
	ResourceTypeGuardDutyFilter = "aws_guardduty_filter"
	// ResourceTypeGuardDutyPublishingDestination identifies a GuardDuty finding
	// publishing destination.
	ResourceTypeGuardDutyPublishingDestination = "aws_guardduty_publishing_destination"
	// ResourceTypeGuardDutyThreatIntelSet identifies a GuardDuty threat intel
	// set metadata summary.
	ResourceTypeGuardDutyThreatIntelSet = "aws_guardduty_threat_intel_set"
	// ResourceTypeGuardDutyIPSet identifies a GuardDuty IP set metadata summary.
	ResourceTypeGuardDutyIPSet = "aws_guardduty_ip_set"
)

const (
	// RelationshipGuardDutyDetectorHasMemberAccount records a GuardDuty
	// administrator detector's member account.
	RelationshipGuardDutyDetectorHasMemberAccount = "guardduty_detector_has_member_account"
	// RelationshipGuardDutyDetectorPublishesToDestination records a detector's
	// finding publishing destination.
	RelationshipGuardDutyDetectorPublishesToDestination = "guardduty_detector_publishes_to_destination"
	// RelationshipGuardDutyDetectorUsesThreatIntelSet records a detector's threat
	// intel set membership.
	RelationshipGuardDutyDetectorUsesThreatIntelSet = "guardduty_detector_uses_threat_intel_set"
	// RelationshipGuardDutyDetectorUsesIPSet records a detector's IP set
	// membership.
	RelationshipGuardDutyDetectorUsesIPSet = "guardduty_detector_uses_ip_set"
)

// Security-threat-category constants consolidated from constants_securityhub.go (#6696).

const (
	// ServiceSecurityHub identifies the regional AWS Security Hub metadata
	// posture scan slice.
	ServiceSecurityHub = "securityhub"
)

const (
	// ResourceTypeSecurityHubHub identifies a Security Hub hub configuration.
	ResourceTypeSecurityHubHub = "aws_securityhub_hub"
	// ResourceTypeSecurityHubStandard identifies an enabled Security Hub
	// standards subscription.
	ResourceTypeSecurityHubStandard = "aws_securityhub_standard"
	// ResourceTypeSecurityHubControl identifies a Security Hub standards
	// control.
	ResourceTypeSecurityHubControl = "aws_securityhub_control"
	// ResourceTypeSecurityHubMemberAccount identifies a Security Hub member
	// account reported by an administrator account.
	ResourceTypeSecurityHubMemberAccount = "aws_securityhub_member_account"
	// ResourceTypeSecurityHubActionTarget identifies a custom Security Hub
	// action target.
	ResourceTypeSecurityHubActionTarget = "aws_securityhub_action_target"
	// ResourceTypeSecurityHubInsight identifies a custom Security Hub insight
	// summary.
	ResourceTypeSecurityHubInsight = "aws_securityhub_insight"
	// ResourceTypeSecurityHubFindingAggregate identifies an aggregate Security
	// Hub finding posture bucket.
	ResourceTypeSecurityHubFindingAggregate = "aws_securityhub_finding_aggregate"
)

const (
	// RelationshipSecurityHubHubHasMember records Security Hub administrator
	// membership evidence.
	RelationshipSecurityHubHubHasMember = "securityhub_hub_has_member"
	// RelationshipSecurityHubStandardHasControl records Security Hub standard
	// control membership.
	RelationshipSecurityHubStandardHasControl = "securityhub_standard_has_control"
	// RelationshipSecurityHubInsightGroupsControl records a custom insight that
	// groups by Security Hub control id.
	RelationshipSecurityHubInsightGroupsControl = "securityhub_insight_groups_control"
)

// Security-threat-category constants consolidated from constants_securitylake.go (#6696).

const (
	// ServiceSecurityLake identifies the regional Amazon Security Lake
	// metadata-only scan slice. The scanner reads control-plane configuration
	// through the Security Lake management APIs (ListDataLakes, ListLogSources,
	// ListSubscribers) and never reads ingested security log records, object
	// contents, subscriber credentials, or any data-plane payload, and never
	// mutates Security Lake state.
	ServiceSecurityLake = "securitylake"
)

const (
	// ResourceTypeSecurityLakeDataLake identifies an Amazon Security Lake data
	// lake configuration in one Region. The scanner emits the data lake ARN, the
	// backing S3 bucket ARN reference, the KMS encryption key reference, the
	// create/update status, and lifecycle retention summary only.
	ResourceTypeSecurityLakeDataLake = "aws_securitylake_data_lake"
	// ResourceTypeSecurityLakeLogSource identifies one Amazon Security Lake log
	// source (an AWS-native source name/version or a third-party custom source
	// name) collected for an account and Region. The scanner emits the source
	// identity and collection scope only, never the ingested log records.
	ResourceTypeSecurityLakeLogSource = "aws_securitylake_log_source"
	// ResourceTypeSecurityLakeSubscriber identifies an Amazon Security Lake
	// subscriber. The scanner emits the subscriber ARN, id, name, access types,
	// status, principal account id, and resolvable role/bucket references only.
	// The subscriber external id, endpoint, and any credential material are never
	// persisted.
	ResourceTypeSecurityLakeSubscriber = "aws_securitylake_subscriber"
)

const (
	// RelationshipSecurityLakeDataLakeUsesS3Bucket records a Security Lake data
	// lake's backing S3 bucket. AWS reports the bucket ARN, which matches the S3
	// scanner's published bucket resource_id, so the edge joins the bucket node.
	RelationshipSecurityLakeDataLakeUsesS3Bucket = "securitylake_data_lake_uses_s3_bucket"
	// RelationshipSecurityLakeDataLakeUsesKMSKey records a Security Lake data
	// lake's reported KMS encryption key dependency. The target is keyed by the
	// reported key id or ARN, matching the KMS scanner's published key
	// resource_id.
	RelationshipSecurityLakeDataLakeUsesKMSKey = "securitylake_data_lake_uses_kms_key"
	// RelationshipSecurityLakeDataLakeRegisteredInLakeFormation records that a
	// Security Lake data lake registers its backing S3 bucket with Lake
	// Formation. The target is keyed by the bucket ARN, which matches how the
	// Lake Formation scanner publishes a registered-resource node's resource_id.
	RelationshipSecurityLakeDataLakeRegisteredInLakeFormation = "securitylake_data_lake_registered_in_lake_formation"
	// RelationshipSecurityLakeLogSourceInDataLake records a log source's
	// membership in the Region's data lake. The target is keyed by the data lake
	// ARN the data lake node publishes.
	RelationshipSecurityLakeLogSourceInDataLake = "securitylake_log_source_in_data_lake"
	// RelationshipSecurityLakeLogSourceUsesIAMRole records a third-party custom
	// log source's log-provider IAM role. AWS reports the role ARN, which matches
	// the IAM scanner's published role resource_id.
	RelationshipSecurityLakeLogSourceUsesIAMRole = "securitylake_log_source_uses_iam_role"
	// RelationshipSecurityLakeSubscriberUsesIAMRole records a subscriber's
	// access IAM role. AWS reports the role ARN, which matches the IAM scanner's
	// published role resource_id.
	RelationshipSecurityLakeSubscriberUsesIAMRole = "securitylake_subscriber_uses_iam_role"
	// RelationshipSecurityLakeSubscriberUsesS3Bucket records a subscriber's
	// backing S3 bucket. AWS reports the bucket ARN, which matches the S3
	// scanner's published bucket resource_id.
	RelationshipSecurityLakeSubscriberUsesS3Bucket = "securitylake_subscriber_uses_s3_bucket"
)

// Security-threat-category constants consolidated from constants_detective.go (#6696).

const (
	// ServiceDetective identifies the regional Amazon Detective metadata scan
	// slice. Detective is a regional service, so one boundary covers one
	// account and region.
	ServiceDetective = "detective"
)

const (
	// ResourceTypeDetectiveGraph identifies an Amazon Detective behavior graph
	// metadata resource. Investigation, finding-group, and indicator data are
	// not part of this resource.
	ResourceTypeDetectiveGraph = "aws_detective_graph"
	// ResourceTypeDetectiveMemberAccount identifies an account enrolled in a
	// Detective behavior graph as reported by the graph administrator. It
	// carries membership status only, never the member's contact email.
	ResourceTypeDetectiveMemberAccount = "aws_detective_member_account"
)

const (
	// RelationshipDetectiveGraphHasMemberAccount records a Detective behavior
	// graph's enrolled member account. The edge targets the member account's
	// AWS Organizations account node so graph membership joins org context.
	RelationshipDetectiveGraphHasMemberAccount = "detective_graph_has_member_account"
	// RelationshipDetectiveGraphSourcesGuardDutyDetector records that a
	// Detective behavior graph ingests data from a GuardDuty detector. The edge
	// targets the GuardDuty detector node and is emitted only when a real
	// detector id is resolvable for the graph.
	RelationshipDetectiveGraphSourcesGuardDutyDetector = "detective_graph_sources_guardduty_detector"
)

// Security-threat-category constants consolidated from constants_inspector2.go (#6696).

const (
	// ServiceInspector2 identifies the regional Amazon Inspector v2 metadata
	// scan slice.
	ServiceInspector2 = "inspector2"
)

const (
	// ResourceTypeInspector2Account identifies an Amazon Inspector v2 account
	// status record with its enabled scan features.
	ResourceTypeInspector2Account = "aws_inspector2_account"
	// ResourceTypeInspector2MemberAccount identifies an Amazon Inspector v2
	// member account reported by a delegated administrator account.
	ResourceTypeInspector2MemberAccount = "aws_inspector2_member_account"
	// ResourceTypeInspector2Filter identifies an Amazon Inspector v2 findings
	// filter summary. Only the filter name and non-criteria metadata are
	// persisted; filter criteria expressions are never stored.
	ResourceTypeInspector2Filter = "aws_inspector2_filter"
	// ResourceTypeInspector2CisScanConfiguration identifies an Amazon Inspector
	// v2 CIS scan configuration metadata summary.
	ResourceTypeInspector2CisScanConfiguration = "aws_inspector2_cis_scan_configuration"
)

const (
	// RelationshipInspector2MemberManagedByAdministrator records that an Amazon
	// Inspector v2 member account is managed by a delegated administrator
	// account.
	RelationshipInspector2MemberManagedByAdministrator = "inspector2_member_managed_by_administrator"
	// RelationshipInspector2CisScanConfigurationTargetsAccount records that an
	// Amazon Inspector v2 CIS scan configuration targets a member account.
	RelationshipInspector2CisScanConfigurationTargetsAccount = "inspector2_cis_scan_configuration_targets_account"
)

// Security-threat-category constants consolidated from constants_macie.go (#6696).

const (
	// ServiceMacie identifies the regional Amazon Macie metadata scan slice.
	// Amazon Macie's SDK service id is macie2; the collector service_kind is
	// "macie2" so metric labels match the AWS SDK service identity.
	ServiceMacie = "macie2"
)

const (
	// ResourceTypeMacieSession identifies an Amazon Macie account session
	// status record (enabled or paused) for one account and region. The session
	// is the account-level Macie resource; member accounts attach to it.
	ResourceTypeMacieSession = "aws_macie_session"
	// ResourceTypeMacieClassificationJob identifies an Amazon Macie sensitive
	// data discovery (classification) job. Only job identity, type, status, and
	// a bucket-criteria-summary count are persisted; the job's bucket-criteria
	// expressions and the explicit bucket list are never stored.
	ResourceTypeMacieClassificationJob = "aws_macie_classification_job"
	// ResourceTypeMacieAllowList identifies an Amazon Macie allow list. Only the
	// list identity (id, name) is persisted; allow-list contents (the literal
	// text or S3-hosted regex that suppresses matches) are never stored.
	ResourceTypeMacieAllowList = "aws_macie_allow_list"
	// ResourceTypeMacieCustomDataIdentifier identifies an Amazon Macie custom
	// data identifier. Only the identity (id, name) is persisted; the regular
	// expression body is never stored because it is itself a description of the
	// sensitive data the customer is detecting.
	ResourceTypeMacieCustomDataIdentifier = "aws_macie_custom_data_identifier"
	// ResourceTypeMacieFindingsFilter identifies an Amazon Macie findings
	// filter. Only the identity (id, name, action) is persisted; the filter
	// criteria expressions are never stored.
	ResourceTypeMacieFindingsFilter = "aws_macie_findings_filter"
	// ResourceTypeMacieMemberAccount identifies an Amazon Macie member account
	// reported by a delegated administrator account.
	ResourceTypeMacieMemberAccount = "aws_macie_member_account"
)

const (
	// RelationshipMacieMemberManagedByAdministrator records that an Amazon Macie
	// member account is managed by a delegated administrator account. The edge
	// targets the administrator account's Macie session resource.
	RelationshipMacieMemberManagedByAdministrator = "macie_member_managed_by_administrator"
)

// Security-threat-category constants consolidated from constants_shield.go (#6696).

const (
	// ServiceShield identifies the AWS Shield Advanced metadata scan slice.
	// Shield is a global service: one claim per account observes every
	// protection and the account subscription summary regardless of region.
	ServiceShield = "shield"
)

const (
	// ResourceTypeShieldProtection identifies an AWS Shield Advanced protection
	// metadata resource. The protection's resource_id is its protection ARN, and
	// the resource carries the protected resource ARN as an attribute so the
	// protection-to-protected-resource edge can join the protected node.
	ResourceTypeShieldProtection = "aws_shield_protection"
	// ResourceTypeShieldSubscription identifies the per-account AWS Shield
	// Advanced subscription summary resource. It carries subscription state and
	// auto-renew metadata only; no billing detail is read or persisted.
	ResourceTypeShieldSubscription = "aws_shield_subscription"
)

const (
	// RelationshipShieldProtectionProtectsResource records a Shield Advanced
	// protection association to the resource it protects, keyed by the protected
	// resource ARN reported by AWS. The edge's target_type is derived from the
	// protected ARN's service segment (ELBv2 load balancer, CloudFront
	// distribution, EIP, Route 53 hosted zone, or Global Accelerator
	// accelerator), and its target_resource_id matches how that target scanner
	// publishes its resource_id.
	RelationshipShieldProtectionProtectsResource = "shield_protection_protects_resource"
)

// Security-threat-category constants consolidated from constants_fms.go (#6696).

const (
	// ServiceFMS identifies the AWS Firewall Manager (FMS) metadata scan slice.
	// FMS is an organization-wide control plane reachable only from the FMS
	// administrator account, so one claim scans the administrator account's
	// policy fleet rather than a per-account resource set.
	ServiceFMS = "fms"
)

const (
	// ResourceTypeFMSPolicy identifies an AWS Firewall Manager policy metadata
	// resource. The resource carries policy identity, the governing security
	// service type, the in-scope resource type label, and the remediation flag;
	// the policy rule payload (SecurityServicePolicyData managed service data)
	// is never read or persisted.
	ResourceTypeFMSPolicy = "aws_fms_policy"
)

const (
	// RelationshipFMSPolicyAppliesToAccount records that a Firewall Manager
	// policy applies to (is evaluated against) an Organizations member account.
	// The edge keys on the bare 12-digit member account id, matching the
	// resource_id the organizations scanner publishes for an
	// aws_organizations_account node, so the edge joins instead of dangling.
	RelationshipFMSPolicyAppliesToAccount = "fms_policy_applies_to_account"
)
