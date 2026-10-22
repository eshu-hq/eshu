// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestG2ConstantsMatchAWSCloudValues pins every G2-category constant
// to the exact value it carried in package awscloud before the #6696 catalog
// consolidation. The consolidation moves declarations only: any value drift
// here is a behavior change, not a move.
func TestG2ConstantsMatchAWSCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "ServiceWAFv2", got: ServiceWAFv2, want: "wafv2"},
		{name: "ResourceTypeWAFv2WebACL", got: ResourceTypeWAFv2WebACL, want: "aws_wafv2_web_acl"},
		{name: "ResourceTypeWAFv2RuleGroup", got: ResourceTypeWAFv2RuleGroup, want: "aws_wafv2_rule_group"},
		{name: "ResourceTypeWAFv2IPSet", got: ResourceTypeWAFv2IPSet, want: "aws_wafv2_ip_set"},
		{name: "ResourceTypeWAFv2RegexPatternSet", got: ResourceTypeWAFv2RegexPatternSet, want: "aws_wafv2_regex_pattern_set"},
		{name: "RelationshipWAFv2WebACLProtectsResource", got: RelationshipWAFv2WebACLProtectsResource, want: "wafv2_web_acl_protects_resource"},
		{name: "RelationshipWAFv2WebACLUsesRuleGroup", got: RelationshipWAFv2WebACLUsesRuleGroup, want: "wafv2_web_acl_uses_rule_group"},
		{name: "RelationshipWAFv2WebACLUsesIPSet", got: RelationshipWAFv2WebACLUsesIPSet, want: "wafv2_web_acl_uses_ip_set"},
		{name: "RelationshipWAFv2WebACLUsesRegexPatternSet", got: RelationshipWAFv2WebACLUsesRegexPatternSet, want: "wafv2_web_acl_uses_regex_pattern_set"},
		{name: "ServiceMGN", got: ServiceMGN, want: "mgn"},
		{name: "ResourceTypeMGNSourceServer", got: ResourceTypeMGNSourceServer, want: "aws_mgn_source_server"},
		{name: "ResourceTypeMGNApplication", got: ResourceTypeMGNApplication, want: "aws_mgn_application"},
		{name: "ResourceTypeMGNLaunchConfiguration", got: ResourceTypeMGNLaunchConfiguration, want: "aws_mgn_launch_configuration"},
		{name: "ResourceTypeMGNJob", got: ResourceTypeMGNJob, want: "aws_mgn_job"},
		{name: "RelationshipMGNApplicationContainsSourceServer", got: RelationshipMGNApplicationContainsSourceServer, want: "mgn_application_contains_source_server"},
		{name: "RelationshipMGNSourceServerLaunchedEC2Instance", got: RelationshipMGNSourceServerLaunchedEC2Instance, want: "mgn_source_server_launched_ec2_instance"},
		{name: "RelationshipMGNLaunchConfigurationUsesLaunchTemplate", got: RelationshipMGNLaunchConfigurationUsesLaunchTemplate, want: "mgn_launch_configuration_uses_launch_template"},
		{name: "RelationshipMGNJobTargetsSourceServer", got: RelationshipMGNJobTargetsSourceServer, want: "mgn_job_targets_source_server"},
		{name: "ServiceLocation", got: ServiceLocation, want: "location"},
		{name: "ResourceTypeLocationMap", got: ResourceTypeLocationMap, want: "aws_location_map"},
		{name: "ResourceTypeLocationPlaceIndex", got: ResourceTypeLocationPlaceIndex, want: "aws_location_place_index"},
		{name: "ResourceTypeLocationTracker", got: ResourceTypeLocationTracker, want: "aws_location_tracker"},
		{name: "ResourceTypeLocationGeofenceCollection", got: ResourceTypeLocationGeofenceCollection, want: "aws_location_geofence_collection"},
		{name: "ResourceTypeLocationRouteCalculator", got: ResourceTypeLocationRouteCalculator, want: "aws_location_route_calculator"},
		{name: "RelationshipLocationTrackerUsesKMSKey", got: RelationshipLocationTrackerUsesKMSKey, want: "location_tracker_uses_kms_key"},
		{name: "RelationshipLocationGeofenceCollectionUsesKMSKey", got: RelationshipLocationGeofenceCollectionUsesKMSKey, want: "location_geofence_collection_uses_kms_key"},
		{name: "RelationshipLocationTrackerConsumesGeofenceCollection", got: RelationshipLocationTrackerConsumesGeofenceCollection, want: "location_tracker_consumes_geofence_collection"},
		{name: "ServiceDirectoryService", got: ServiceDirectoryService, want: "ds"},
		{name: "ResourceTypeDSDirectory", got: ResourceTypeDSDirectory, want: "aws_ds_directory"},
		{name: "ResourceTypeDSTrust", got: ResourceTypeDSTrust, want: "aws_ds_trust"},
		{name: "ResourceTypeDSSharedDirectory", got: ResourceTypeDSSharedDirectory, want: "aws_ds_shared_directory"},
		{name: "RelationshipDSDirectoryInVPC", got: RelationshipDSDirectoryInVPC, want: "ds_directory_in_vpc"},
		{name: "RelationshipDSDirectoryInSubnet", got: RelationshipDSDirectoryInSubnet, want: "ds_directory_in_subnet"},
		{name: "RelationshipDSTrustTargetsDirectory", got: RelationshipDSTrustTargetsDirectory, want: "ds_trust_targets_directory"},
		{name: "RelationshipDSSharedDirectoryTargetsOwnerDirectory", got: RelationshipDSSharedDirectoryTargetsOwnerDirectory, want: "ds_shared_directory_targets_owner_directory"},
		{name: "RelationshipDSSharedDirectoryTargetsOwnerAccount", got: RelationshipDSSharedDirectoryTargetsOwnerAccount, want: "ds_shared_directory_targets_owner_account"},
		{name: "ServiceAuditManager", got: ServiceAuditManager, want: "auditmanager"},
		{name: "ResourceTypeAuditManagerAssessment", got: ResourceTypeAuditManagerAssessment, want: "aws_auditmanager_assessment"},
		{name: "ResourceTypeAuditManagerFramework", got: ResourceTypeAuditManagerFramework, want: "aws_auditmanager_framework"},
		{name: "ResourceTypeAuditManagerControl", got: ResourceTypeAuditManagerControl, want: "aws_auditmanager_control"},
		{name: "RelationshipAuditManagerAssessmentUsesFramework", got: RelationshipAuditManagerAssessmentUsesFramework, want: "auditmanager_assessment_uses_framework"},
		{name: "RelationshipAuditManagerAssessmentReportsToS3", got: RelationshipAuditManagerAssessmentReportsToS3, want: "auditmanager_assessment_reports_to_s3"},
		{name: "RelationshipAuditManagerAssessmentEncryptedWithKMSKey", got: RelationshipAuditManagerAssessmentEncryptedWithKMSKey, want: "auditmanager_assessment_encrypted_with_kms_key"},
		{name: "RelationshipAuditManagerAssessmentInAccount", got: RelationshipAuditManagerAssessmentInAccount, want: "auditmanager_assessment_in_account"},
		{name: "ServiceAppFlow", got: ServiceAppFlow, want: "appflow"},
		{name: "ResourceTypeAppFlowFlow", got: ResourceTypeAppFlowFlow, want: "aws_appflow_flow"},
		{name: "ResourceTypeAppFlowConnectorProfile", got: ResourceTypeAppFlowConnectorProfile, want: "aws_appflow_connector_profile"},
		{name: "RelationshipAppFlowFlowReadsFromS3Bucket", got: RelationshipAppFlowFlowReadsFromS3Bucket, want: "appflow_flow_reads_from_s3_bucket"},
		{name: "RelationshipAppFlowFlowWritesToS3Bucket", got: RelationshipAppFlowFlowWritesToS3Bucket, want: "appflow_flow_writes_to_s3_bucket"},
		{name: "RelationshipAppFlowFlowUsesConnectorProfile", got: RelationshipAppFlowFlowUsesConnectorProfile, want: "appflow_flow_uses_connector_profile"},
		{name: "RelationshipAppFlowFlowUsesKMSKey", got: RelationshipAppFlowFlowUsesKMSKey, want: "appflow_flow_uses_kms_key"},
		{name: "RelationshipAppFlowConnectorProfileUsesSecret", got: RelationshipAppFlowConnectorProfileUsesSecret, want: "appflow_connector_profile_uses_secret"},
		{name: "ServiceAmplify", got: ServiceAmplify, want: "amplify"},
		{name: "ResourceTypeAmplifyApp", got: ResourceTypeAmplifyApp, want: "aws_amplify_app"},
		{name: "ResourceTypeAmplifyBranch", got: ResourceTypeAmplifyBranch, want: "aws_amplify_branch"},
		{name: "RelationshipAmplifyAppDeploysFromRepository", got: RelationshipAmplifyAppDeploysFromRepository, want: "amplify_app_deploys_from_repository"},
		{name: "RelationshipAmplifyAppUsesIAMRole", got: RelationshipAmplifyAppUsesIAMRole, want: "amplify_app_uses_iam_role"},
		{name: "RelationshipAmplifyAppServesCustomDomainViaHostedZone", got: RelationshipAmplifyAppServesCustomDomainViaHostedZone, want: "amplify_app_serves_custom_domain_via_hosted_zone"},
		{name: "RelationshipAmplifyAppServesCustomDomainViaCloudFront", got: RelationshipAmplifyAppServesCustomDomainViaCloudFront, want: "amplify_app_serves_custom_domain_via_cloudfront"},
		{name: "RelationshipAmplifyBranchBelongsToApp", got: RelationshipAmplifyBranchBelongsToApp, want: "amplify_branch_belongs_to_app"},
		{name: "ServiceVerifiedPermissions", got: ServiceVerifiedPermissions, want: "verifiedpermissions"},
		{name: "ResourceTypeVerifiedPermissionsPolicyStore", got: ResourceTypeVerifiedPermissionsPolicyStore, want: "aws_verifiedpermissions_policy_store"},
		{name: "ResourceTypeVerifiedPermissionsPolicy", got: ResourceTypeVerifiedPermissionsPolicy, want: "aws_verifiedpermissions_policy"},
		{name: "ResourceTypeVerifiedPermissionsIdentitySource", got: ResourceTypeVerifiedPermissionsIdentitySource, want: "aws_verifiedpermissions_identity_source"},
		{name: "RelationshipVerifiedPermissionsPolicyInStore", got: RelationshipVerifiedPermissionsPolicyInStore, want: "verifiedpermissions_policy_in_store"},
		{name: "RelationshipVerifiedPermissionsIdentitySourceInStore", got: RelationshipVerifiedPermissionsIdentitySourceInStore, want: "verifiedpermissions_identity_source_in_store"},
		{name: "RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool", got: RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool, want: "verifiedpermissions_identity_source_uses_cognito_user_pool"},
		{name: "ServiceRolesAnywhere", got: ServiceRolesAnywhere, want: "rolesanywhere"},
		{name: "ResourceTypeRolesAnywhereTrustAnchor", got: ResourceTypeRolesAnywhereTrustAnchor, want: "aws_rolesanywhere_trust_anchor"},
		{name: "ResourceTypeRolesAnywhereProfile", got: ResourceTypeRolesAnywhereProfile, want: "aws_rolesanywhere_profile"},
		{name: "ResourceTypeRolesAnywhereCRL", got: ResourceTypeRolesAnywhereCRL, want: "aws_rolesanywhere_crl"},
		{name: "RelationshipRolesAnywhereProfileAssumesRole", got: RelationshipRolesAnywhereProfileAssumesRole, want: "rolesanywhere_profile_assumes_role"},
		{name: "RelationshipRolesAnywhereTrustAnchorUsesACMPCA", got: RelationshipRolesAnywhereTrustAnchorUsesACMPCA, want: "rolesanywhere_trust_anchor_uses_acmpca"},
		{name: "RelationshipRolesAnywhereCRLValidatesTrustAnchor", got: RelationshipRolesAnywhereCRLValidatesTrustAnchor, want: "rolesanywhere_crl_validates_trust_anchor"},
		{name: "ServiceCloudHSMV2", got: ServiceCloudHSMV2, want: "cloudhsmv2"},
		{name: "ResourceTypeCloudHSMV2Cluster", got: ResourceTypeCloudHSMV2Cluster, want: "aws_cloudhsmv2_cluster"},
		{name: "ResourceTypeCloudHSMV2Backup", got: ResourceTypeCloudHSMV2Backup, want: "aws_cloudhsmv2_backup"},
		{name: "RelationshipCloudHSMV2ClusterInVPC", got: RelationshipCloudHSMV2ClusterInVPC, want: "cloudhsmv2_cluster_in_vpc"},
		{name: "RelationshipCloudHSMV2ClusterInSubnet", got: RelationshipCloudHSMV2ClusterInSubnet, want: "cloudhsmv2_cluster_in_subnet"},
		{name: "RelationshipCloudHSMV2ClusterUsesSecurityGroup", got: RelationshipCloudHSMV2ClusterUsesSecurityGroup, want: "cloudhsmv2_cluster_uses_security_group"},
		{name: "RelationshipCloudHSMV2BackupOfCluster", got: RelationshipCloudHSMV2BackupOfCluster, want: "cloudhsmv2_backup_of_cluster"},
		{name: "ServiceAccessAnalyzer", got: ServiceAccessAnalyzer, want: "accessanalyzer"},
		{name: "ResourceTypeAccessAnalyzerAnalyzer", got: ResourceTypeAccessAnalyzerAnalyzer, want: "aws_accessanalyzer_analyzer"},
		{name: "ResourceTypeAccessAnalyzerArchiveRule", got: ResourceTypeAccessAnalyzerArchiveRule, want: "aws_accessanalyzer_archive_rule"},
		{name: "ResourceTypeAccessAnalyzerFindingCount", got: ResourceTypeAccessAnalyzerFindingCount, want: "aws_accessanalyzer_finding_count"},
		{name: "ResourceTypeAccessAnalyzerUnusedAccessSummary", got: ResourceTypeAccessAnalyzerUnusedAccessSummary, want: "aws_accessanalyzer_unused_access_summary"},
		{name: "RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount", got: RelationshipAccessAnalyzerAnalyzerScopesOrganizationAccount, want: "accessanalyzer_analyzer_scopes_organization_account"},
		{name: "RelationshipAccessAnalyzerAnalyzerHasArchiveRule", got: RelationshipAccessAnalyzerAnalyzerHasArchiveRule, want: "accessanalyzer_analyzer_has_archive_rule"},
		{name: "ServiceOutposts", got: ServiceOutposts, want: "outposts"},
		{name: "ResourceTypeOutpostsOutpost", got: ResourceTypeOutpostsOutpost, want: "aws_outposts_outpost"},
		{name: "ResourceTypeOutpostsSite", got: ResourceTypeOutpostsSite, want: "aws_outposts_site"},
		{name: "ResourceTypeOutpostsAsset", got: ResourceTypeOutpostsAsset, want: "aws_outposts_asset"},
		{name: "RelationshipOutpostsOutpostInSite", got: RelationshipOutpostsOutpostInSite, want: "outposts_outpost_in_site"},
		{name: "RelationshipOutpostsAssetInOutpost", got: RelationshipOutpostsAssetInOutpost, want: "outposts_asset_in_outpost"},
		{name: "ServiceDRS", got: ServiceDRS, want: "drs"},
		{name: "ResourceTypeDRSSourceServer", got: ResourceTypeDRSSourceServer, want: "aws_drs_source_server"},
		{name: "ResourceTypeDRSRecoveryInstance", got: ResourceTypeDRSRecoveryInstance, want: "aws_drs_recovery_instance"},
		{name: "ResourceTypeDRSReplicationConfigurationTemplate", got: ResourceTypeDRSReplicationConfigurationTemplate, want: "aws_drs_replication_configuration_template"},
		{name: "RelationshipDRSSourceServerRecoversToInstance", got: RelationshipDRSSourceServerRecoversToInstance, want: "drs_source_server_recovers_to_instance"},
		{name: "RelationshipDRSRecoveryInstanceRunsOnEC2Instance", got: RelationshipDRSRecoveryInstanceRunsOnEC2Instance, want: "drs_recovery_instance_runs_on_ec2_instance"},
		{name: "ResourcePolicySourceResource", got: ResourcePolicySourceResource, want: "resource"},
		{name: "ResourcePolicyPrincipalTypeAWS", got: ResourcePolicyPrincipalTypeAWS, want: "aws"},
		{name: "ResourcePolicyPrincipalTypeService", got: ResourcePolicyPrincipalTypeService, want: "service"},
		{name: "ResourcePolicyPrincipalTypeFederated", got: ResourcePolicyPrincipalTypeFederated, want: "federated"},
		{name: "ResourcePolicyPrincipalTypeCanonical", got: ResourcePolicyPrincipalTypeCanonical, want: "canonical"},
		{name: "ServiceGrafana", got: ServiceGrafana, want: "grafana"},
		{name: "ResourceTypeGrafanaWorkspace", got: ResourceTypeGrafanaWorkspace, want: "aws_grafana_workspace"},
		{name: "RelationshipGrafanaWorkspaceUsesIAMRole", got: RelationshipGrafanaWorkspaceUsesIAMRole, want: "grafana_workspace_uses_iam_role"},
		{name: "RelationshipGrafanaWorkspaceInSubnet", got: RelationshipGrafanaWorkspaceInSubnet, want: "grafana_workspace_in_subnet"},
		{name: "RelationshipGrafanaWorkspaceUsesSecurityGroup", got: RelationshipGrafanaWorkspaceUsesSecurityGroup, want: "grafana_workspace_uses_security_group"},
		{name: "ServiceSecretsManager", got: ServiceSecretsManager, want: "secretsmanager"},
		{name: "ResourceTypeSecretsManagerSecret", got: ResourceTypeSecretsManagerSecret, want: "aws_secretsmanager_secret"},
		{name: "RelationshipSecretsManagerSecretUsesKMSKey", got: RelationshipSecretsManagerSecretUsesKMSKey, want: "secretsmanager_secret_uses_kms_key"},
		{name: "RelationshipSecretsManagerSecretUsesRotationLambda", got: RelationshipSecretsManagerSecretUsesRotationLambda, want: "secretsmanager_secret_uses_rotation_lambda"},
		{name: "ServiceSSM", got: ServiceSSM, want: "ssm"},
		{name: "ResourceTypeSSMParameter", got: ResourceTypeSSMParameter, want: "aws_ssm_parameter"},
		{name: "RelationshipSSMParameterUsesKMSKey", got: RelationshipSSMParameterUsesKMSKey, want: "ssm_parameter_uses_kms_key"},
		{name: "ResourceTypeAWSAccount", got: ResourceTypeAWSAccount, want: "aws_account"},
		{name: "ResourceTypeGeneric", got: ResourceTypeGeneric, want: "aws_resource"},
	}

	if len(tests) != 119 {
		t.Fatalf("pin table has %d rows, want 119 (one per moved G2 constant)", len(tests))
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
