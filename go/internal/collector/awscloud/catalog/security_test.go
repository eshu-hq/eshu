// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestSecurityConstantsMatchAWSCloudValues pins every security-category constant
// to the exact value it carried in package awscloud before the #6696 catalog
// consolidation. The consolidation moves declarations only: any value drift
// here is a behavior change, not a move.
func TestSecurityConstantsMatchAWSCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "IAMPolicySourceAttachedManaged", got: IAMPolicySourceAttachedManaged, want: "attached_managed"},
		{name: "IAMPolicySourceInline", got: IAMPolicySourceInline, want: "inline"},
		{name: "IAMPolicySourcePermissionBoundary", got: IAMPolicySourcePermissionBoundary, want: "permission_boundary"},
		{name: "IAMPolicySourceTrust", got: IAMPolicySourceTrust, want: "trust"},
		{name: "RelationshipACMCertificateUsedByResource", got: RelationshipACMCertificateUsedByResource, want: "acm_certificate_used_by_resource"},
		{name: "RelationshipACMPCACertificateAuthorityPublishesCRLToBucket", got: RelationshipACMPCACertificateAuthorityPublishesCRLToBucket, want: "acmpca_certificate_authority_publishes_crl_to_bucket"},
		{name: "RelationshipACMPCACertificateAuthorityUsesKMSKey", got: RelationshipACMPCACertificateAuthorityUsesKMSKey, want: "acmpca_certificate_authority_uses_kms_key"},
		{name: "RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent", got: RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent, want: "acmpca_subordinate_certificate_authority_issued_by_parent"},
		{name: "RelationshipDetectiveGraphHasMemberAccount", got: RelationshipDetectiveGraphHasMemberAccount, want: "detective_graph_has_member_account"},
		{name: "RelationshipDetectiveGraphSourcesGuardDutyDetector", got: RelationshipDetectiveGraphSourcesGuardDutyDetector, want: "detective_graph_sources_guardduty_detector"},
		{name: "RelationshipFMSPolicyAppliesToAccount", got: RelationshipFMSPolicyAppliesToAccount, want: "fms_policy_applies_to_account"},
		{name: "RelationshipGuardDutyDetectorHasMemberAccount", got: RelationshipGuardDutyDetectorHasMemberAccount, want: "guardduty_detector_has_member_account"},
		{name: "RelationshipGuardDutyDetectorPublishesToDestination", got: RelationshipGuardDutyDetectorPublishesToDestination, want: "guardduty_detector_publishes_to_destination"},
		{name: "RelationshipGuardDutyDetectorUsesIPSet", got: RelationshipGuardDutyDetectorUsesIPSet, want: "guardduty_detector_uses_ip_set"},
		{name: "RelationshipGuardDutyDetectorUsesThreatIntelSet", got: RelationshipGuardDutyDetectorUsesThreatIntelSet, want: "guardduty_detector_uses_threat_intel_set"},
		{name: "RelationshipIAMRoleAttachedPolicy", got: RelationshipIAMRoleAttachedPolicy, want: "iam_role_attached_policy"},
		{name: "RelationshipIAMRoleInInstanceProfile", got: RelationshipIAMRoleInInstanceProfile, want: "iam_role_in_instance_profile"},
		{name: "RelationshipIAMRoleTrustsPrincipal", got: RelationshipIAMRoleTrustsPrincipal, want: "iam_role_trusts_principal"},
		{name: "RelationshipInspector2CisScanConfigurationTargetsAccount", got: RelationshipInspector2CisScanConfigurationTargetsAccount, want: "inspector2_cis_scan_configuration_targets_account"},
		{name: "RelationshipInspector2MemberManagedByAdministrator", got: RelationshipInspector2MemberManagedByAdministrator, want: "inspector2_member_managed_by_administrator"},
		{name: "RelationshipKMSAliasTargetsKey", got: RelationshipKMSAliasTargetsKey, want: "kms_alias_targets_key"},
		{name: "RelationshipKMSGrantForGrantee", got: RelationshipKMSGrantForGrantee, want: "kms_grant_for_grantee"},
		{name: "RelationshipKMSGrantOnKey", got: RelationshipKMSGrantOnKey, want: "kms_grant_on_key"},
		{name: "RelationshipMacieMemberManagedByAdministrator", got: RelationshipMacieMemberManagedByAdministrator, want: "macie_member_managed_by_administrator"},
		{name: "RelationshipSSOAdminApplicationInInstance", got: RelationshipSSOAdminApplicationInInstance, want: "identitycenter_application_in_instance"},
		{name: "RelationshipSSOAdminAssignmentGrantsPrincipal", got: RelationshipSSOAdminAssignmentGrantsPrincipal, want: "identitycenter_assignment_grants_principal"},
		{name: "RelationshipSSOAdminAssignmentTargetsAccount", got: RelationshipSSOAdminAssignmentTargetsAccount, want: "identitycenter_assignment_targets_account"},
		{name: "RelationshipSSOAdminAssignmentUsesPermissionSet", got: RelationshipSSOAdminAssignmentUsesPermissionSet, want: "identitycenter_assignment_uses_permission_set"},
		{name: "RelationshipSSOAdminPermissionSetInInstance", got: RelationshipSSOAdminPermissionSetInInstance, want: "identitycenter_permission_set_in_instance"},
		{name: "RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy", got: RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy, want: "identitycenter_permission_set_uses_customer_managed_policy"},
		{name: "RelationshipSSOAdminPermissionSetUsesManagedPolicy", got: RelationshipSSOAdminPermissionSetUsesManagedPolicy, want: "identitycenter_permission_set_uses_managed_policy"},
		{name: "RelationshipSecurityHubHubHasMember", got: RelationshipSecurityHubHubHasMember, want: "securityhub_hub_has_member"},
		{name: "RelationshipSecurityHubInsightGroupsControl", got: RelationshipSecurityHubInsightGroupsControl, want: "securityhub_insight_groups_control"},
		{name: "RelationshipSecurityHubStandardHasControl", got: RelationshipSecurityHubStandardHasControl, want: "securityhub_standard_has_control"},
		{name: "RelationshipSecurityLakeDataLakeRegisteredInLakeFormation", got: RelationshipSecurityLakeDataLakeRegisteredInLakeFormation, want: "securitylake_data_lake_registered_in_lake_formation"},
		{name: "RelationshipSecurityLakeDataLakeUsesKMSKey", got: RelationshipSecurityLakeDataLakeUsesKMSKey, want: "securitylake_data_lake_uses_kms_key"},
		{name: "RelationshipSecurityLakeDataLakeUsesS3Bucket", got: RelationshipSecurityLakeDataLakeUsesS3Bucket, want: "securitylake_data_lake_uses_s3_bucket"},
		{name: "RelationshipSecurityLakeLogSourceInDataLake", got: RelationshipSecurityLakeLogSourceInDataLake, want: "securitylake_log_source_in_data_lake"},
		{name: "RelationshipSecurityLakeLogSourceUsesIAMRole", got: RelationshipSecurityLakeLogSourceUsesIAMRole, want: "securitylake_log_source_uses_iam_role"},
		{name: "RelationshipSecurityLakeSubscriberUsesIAMRole", got: RelationshipSecurityLakeSubscriberUsesIAMRole, want: "securitylake_subscriber_uses_iam_role"},
		{name: "RelationshipSecurityLakeSubscriberUsesS3Bucket", got: RelationshipSecurityLakeSubscriberUsesS3Bucket, want: "securitylake_subscriber_uses_s3_bucket"},
		{name: "RelationshipShieldProtectionProtectsResource", got: RelationshipShieldProtectionProtectsResource, want: "shield_protection_protects_resource"},
		{name: "RelationshipSignerProfileUsesACMCertificate", got: RelationshipSignerProfileUsesACMCertificate, want: "signer_profile_uses_acm_certificate"},
		{name: "RelationshipSignerProfileUsesSigningPlatform", got: RelationshipSignerProfileUsesSigningPlatform, want: "signer_profile_uses_signing_platform"},
		{name: "ResourceTypeACMCertificate", got: ResourceTypeACMCertificate, want: "aws_acm_certificate"},
		{name: "ResourceTypeDetectiveGraph", got: ResourceTypeDetectiveGraph, want: "aws_detective_graph"},
		{name: "ResourceTypeDetectiveMemberAccount", got: ResourceTypeDetectiveMemberAccount, want: "aws_detective_member_account"},
		{name: "ResourceTypeFMSPolicy", got: ResourceTypeFMSPolicy, want: "aws_fms_policy"},
		{name: "ResourceTypeGuardDutyDetector", got: ResourceTypeGuardDutyDetector, want: "aws_guardduty_detector"},
		{name: "ResourceTypeGuardDutyFilter", got: ResourceTypeGuardDutyFilter, want: "aws_guardduty_filter"},
		{name: "ResourceTypeGuardDutyIPSet", got: ResourceTypeGuardDutyIPSet, want: "aws_guardduty_ip_set"},
		{name: "ResourceTypeGuardDutyMemberAccount", got: ResourceTypeGuardDutyMemberAccount, want: "aws_guardduty_member_account"},
		{name: "ResourceTypeGuardDutyPublishingDestination", got: ResourceTypeGuardDutyPublishingDestination, want: "aws_guardduty_publishing_destination"},
		{name: "ResourceTypeGuardDutyThreatIntelSet", got: ResourceTypeGuardDutyThreatIntelSet, want: "aws_guardduty_threat_intel_set"},
		{name: "ResourceTypeIAMGroup", got: ResourceTypeIAMGroup, want: "aws_iam_group"},
		{name: "ResourceTypeIAMInstanceProfile", got: ResourceTypeIAMInstanceProfile, want: "aws_iam_instance_profile"},
		{name: "ResourceTypeIAMPolicy", got: ResourceTypeIAMPolicy, want: "aws_iam_policy"},
		{name: "ResourceTypeIAMPrincipal", got: ResourceTypeIAMPrincipal, want: "aws_iam_principal"},
		{name: "ResourceTypeIAMRole", got: ResourceTypeIAMRole, want: "aws_iam_role"},
		{name: "ResourceTypeIAMUser", got: ResourceTypeIAMUser, want: "aws_iam_user"},
		{name: "ResourceTypeInspector2Account", got: ResourceTypeInspector2Account, want: "aws_inspector2_account"},
		{name: "ResourceTypeInspector2CisScanConfiguration", got: ResourceTypeInspector2CisScanConfiguration, want: "aws_inspector2_cis_scan_configuration"},
		{name: "ResourceTypeInspector2Filter", got: ResourceTypeInspector2Filter, want: "aws_inspector2_filter"},
		{name: "ResourceTypeInspector2MemberAccount", got: ResourceTypeInspector2MemberAccount, want: "aws_inspector2_member_account"},
		{name: "ResourceTypeKMSAlias", got: ResourceTypeKMSAlias, want: "aws_kms_alias"},
		{name: "ResourceTypeKMSGrant", got: ResourceTypeKMSGrant, want: "aws_kms_grant"},
		{name: "ResourceTypeKMSKey", got: ResourceTypeKMSKey, want: "aws_kms_key"},
		{name: "ResourceTypeMacieAllowList", got: ResourceTypeMacieAllowList, want: "aws_macie_allow_list"},
		{name: "ResourceTypeMacieClassificationJob", got: ResourceTypeMacieClassificationJob, want: "aws_macie_classification_job"},
		{name: "ResourceTypeMacieCustomDataIdentifier", got: ResourceTypeMacieCustomDataIdentifier, want: "aws_macie_custom_data_identifier"},
		{name: "ResourceTypeMacieFindingsFilter", got: ResourceTypeMacieFindingsFilter, want: "aws_macie_findings_filter"},
		{name: "ResourceTypeMacieMemberAccount", got: ResourceTypeMacieMemberAccount, want: "aws_macie_member_account"},
		{name: "ResourceTypeMacieSession", got: ResourceTypeMacieSession, want: "aws_macie_session"},
		{name: "ResourceTypeSSOAdminAccountAssignment", got: ResourceTypeSSOAdminAccountAssignment, want: "aws_identitycenter_account_assignment"},
		{name: "ResourceTypeSSOAdminApplication", got: ResourceTypeSSOAdminApplication, want: "aws_identitycenter_application"},
		{name: "ResourceTypeSSOAdminInstance", got: ResourceTypeSSOAdminInstance, want: "aws_identitycenter_instance"},
		{name: "ResourceTypeSSOAdminPermissionSet", got: ResourceTypeSSOAdminPermissionSet, want: "aws_identitycenter_permission_set"},
		{name: "ResourceTypeSSOAdminPrincipal", got: ResourceTypeSSOAdminPrincipal, want: "aws_identitycenter_principal"},
		{name: "ResourceTypeSSOAdminTrustedTokenIssuer", got: ResourceTypeSSOAdminTrustedTokenIssuer, want: "aws_identitycenter_trusted_token_issuer"},
		{name: "ResourceTypeSecurityHubActionTarget", got: ResourceTypeSecurityHubActionTarget, want: "aws_securityhub_action_target"},
		{name: "ResourceTypeSecurityHubControl", got: ResourceTypeSecurityHubControl, want: "aws_securityhub_control"},
		{name: "ResourceTypeSecurityHubFindingAggregate", got: ResourceTypeSecurityHubFindingAggregate, want: "aws_securityhub_finding_aggregate"},
		{name: "ResourceTypeSecurityHubHub", got: ResourceTypeSecurityHubHub, want: "aws_securityhub_hub"},
		{name: "ResourceTypeSecurityHubInsight", got: ResourceTypeSecurityHubInsight, want: "aws_securityhub_insight"},
		{name: "ResourceTypeSecurityHubMemberAccount", got: ResourceTypeSecurityHubMemberAccount, want: "aws_securityhub_member_account"},
		{name: "ResourceTypeSecurityHubStandard", got: ResourceTypeSecurityHubStandard, want: "aws_securityhub_standard"},
		{name: "ResourceTypeSecurityLakeDataLake", got: ResourceTypeSecurityLakeDataLake, want: "aws_securitylake_data_lake"},
		{name: "ResourceTypeSecurityLakeLogSource", got: ResourceTypeSecurityLakeLogSource, want: "aws_securitylake_log_source"},
		{name: "ResourceTypeSecurityLakeSubscriber", got: ResourceTypeSecurityLakeSubscriber, want: "aws_securitylake_subscriber"},
		{name: "ResourceTypeShieldProtection", got: ResourceTypeShieldProtection, want: "aws_shield_protection"},
		{name: "ResourceTypeShieldSubscription", got: ResourceTypeShieldSubscription, want: "aws_shield_subscription"},
		{name: "ResourceTypeSignerSigningPlatform", got: ResourceTypeSignerSigningPlatform, want: "aws_signer_signing_platform"},
		{name: "ResourceTypeSignerSigningProfile", got: ResourceTypeSignerSigningProfile, want: "aws_signer_signing_profile"},
		{name: "ServiceACM", got: ServiceACM, want: "acm"},
		{name: "ServiceACMPCA", got: ServiceACMPCA, want: "acm-pca"},
		{name: "ServiceDetective", got: ServiceDetective, want: "detective"},
		{name: "ServiceFMS", got: ServiceFMS, want: "fms"},
		{name: "ServiceGuardDuty", got: ServiceGuardDuty, want: "guardduty"},
		{name: "ServiceIAM", got: ServiceIAM, want: "iam"},
		{name: "ServiceInspector2", got: ServiceInspector2, want: "inspector2"},
		{name: "ServiceKMS", got: ServiceKMS, want: "kms"},
		{name: "ServiceMacie", got: ServiceMacie, want: "macie2"},
		{name: "ServiceSSOAdmin", got: ServiceSSOAdmin, want: "ssoadmin"},
		{name: "ServiceSecurityHub", got: ServiceSecurityHub, want: "securityhub"},
		{name: "ServiceSecurityLake", got: ServiceSecurityLake, want: "securitylake"},
		{name: "ServiceShield", got: ServiceShield, want: "shield"},
		{name: "ServiceSigner", got: ServiceSigner, want: "signer"},
	}
	for _, tt := range tests {
		tt, name := tt, tt.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q (byte-identical to pre-consolidation awscloud value)", tt.name, tt.got, tt.want)
			}
		})
	}
}
