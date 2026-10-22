// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// ServiceWAFv2 identifies the AWS WAFv2 metadata scan slice. One claim
	// scans either the REGIONAL scope for its boundary region or the global
	// CLOUDFRONT scope when the boundary uses the global region label.
	ServiceWAFv2 = "wafv2"
)

const (
	// ResourceTypeWAFv2WebACL identifies a WAFv2 web ACL metadata resource.
	ResourceTypeWAFv2WebACL = "aws_wafv2_web_acl"
	// ResourceTypeWAFv2RuleGroup identifies a WAFv2 customer-owned rule group
	// metadata resource.
	ResourceTypeWAFv2RuleGroup = "aws_wafv2_rule_group"
	// ResourceTypeWAFv2IPSet identifies a WAFv2 IP set metadata resource. The
	// resource carries the address count only; the address list is never
	// persisted because it commonly contains private CIDR and threat-intel data.
	ResourceTypeWAFv2IPSet = "aws_wafv2_ip_set"
	// ResourceTypeWAFv2RegexPatternSet identifies a WAFv2 regex pattern set
	// metadata resource. The resource carries the pattern count only; the regex
	// bodies are never persisted because they encode customer-detection rules.
	ResourceTypeWAFv2RegexPatternSet = "aws_wafv2_regex_pattern_set"
)

const (
	// RelationshipWAFv2WebACLProtectsResource records a web ACL association to a
	// protected resource (ALB, CloudFront distribution, API Gateway stage,
	// AppSync GraphQL API, App Runner service, or Cognito user pool) reported by
	// AWS.
	RelationshipWAFv2WebACLProtectsResource = "wafv2_web_acl_protects_resource" // #nosec G101 -- relationship-type identifier, not a credential
	// RelationshipWAFv2WebACLUsesRuleGroup records a web ACL reference to a
	// customer-owned rule group by ARN.
	RelationshipWAFv2WebACLUsesRuleGroup = "wafv2_web_acl_uses_rule_group" // #nosec G101 -- relationship-type identifier, not a credential
	// RelationshipWAFv2WebACLUsesIPSet records a web ACL reference to an IP set
	// by ARN.
	RelationshipWAFv2WebACLUsesIPSet = "wafv2_web_acl_uses_ip_set" // #nosec G101 -- relationship-type identifier, not a credential
	// RelationshipWAFv2WebACLUsesRegexPatternSet records a web ACL reference to a
	// regex pattern set by ARN.
	RelationshipWAFv2WebACLUsesRegexPatternSet = "wafv2_web_acl_uses_regex_pattern_set" // #nosec G101 -- relationship-type identifier, not a credential
)

// WAF Classic (v1) is out of scope by construction: the scanner imports only
// the WAFv2 SDK (github.com/aws/aws-sdk-go-v2/service/wafv2), which cannot
// surface waf or waf-regional v1 resources. There is therefore no runtime path
// that observes a v1 resource to skip. If v1 visibility is ever required, add a
// dedicated WAF Classic scanner rather than extending this slice.

const (
	// ServiceMGN identifies the regional AWS Application Migration Service (MGN)
	// metadata-only scan slice. The scanner reads migration control-plane
	// metadata through the MGN management APIs (DescribeSourceServers,
	// ListApplications, GetLaunchConfiguration, DescribeJobs) and never reads or
	// persists replication-agent credentials, replication configuration secrets,
	// or any data-plane replication payload, and never mutates MGN state.
	ServiceMGN = "mgn"
)

const (
	// ResourceTypeMGNSourceServer identifies an AWS Application Migration Service
	// source server metadata resource. The scanner emits identity, lifecycle and
	// data-replication state, the recommended target instance type, the launched
	// cutover/test EC2 instance id, and non-secret identification hints only.
	// Replication-agent credentials and replicated disk contents stay outside the
	// contract.
	ResourceTypeMGNSourceServer = "aws_mgn_source_server"
	// ResourceTypeMGNApplication identifies an AWS Application Migration Service
	// application metadata resource. The scanner emits identity, description,
	// archival status, wave reference, and the aggregated migration status only.
	ResourceTypeMGNApplication = "aws_mgn_application"
	// ResourceTypeMGNLaunchConfiguration identifies an AWS Application Migration
	// Service launch configuration for one source server. The scanner emits the
	// launch disposition, boot mode, target-instance right-sizing method, the
	// referenced EC2 launch template id, and copy-tag/copy-private-ip flags only.
	ResourceTypeMGNLaunchConfiguration = "aws_mgn_launch_configuration"
	// ResourceTypeMGNJob identifies an AWS Application Migration Service job
	// metadata resource (a test or cutover launch run). The scanner emits the
	// job identity, type, status, initiator, and timestamps only; participating
	// server launch details beyond identity are not persisted.
	ResourceTypeMGNJob = "aws_mgn_job"
)

const (
	// RelationshipMGNApplicationContainsSourceServer records an MGN application's
	// membership of a source server. The target is keyed by the source server id
	// the source-server node publishes, so the edge joins that node exactly.
	RelationshipMGNApplicationContainsSourceServer = "mgn_application_contains_source_server"
	// RelationshipMGNSourceServerLaunchedEC2Instance records the cutover/test EC2
	// instance MGN launched for a source server. The target is keyed by the bare
	// EC2 instance id (i-...) the EC2 instance family is published under, a
	// forward reference until a dedicated EC2 instance scanner exists.
	RelationshipMGNSourceServerLaunchedEC2Instance = "mgn_source_server_launched_ec2_instance"
	// RelationshipMGNLaunchConfigurationUsesLaunchTemplate records the EC2 launch
	// template a source server's launch configuration references. The target is
	// keyed by the launch template id (lt-...) MGN reports, matching how the
	// launch-template family is published.
	RelationshipMGNLaunchConfigurationUsesLaunchTemplate = "mgn_launch_configuration_uses_launch_template"
	// RelationshipMGNJobTargetsSourceServer records that an MGN job acts on a
	// participating source server. The target is keyed by the source server id
	// the source-server node publishes, so the edge joins that node exactly.
	RelationshipMGNJobTargetsSourceServer = "mgn_job_targets_source_server"
)

const (
	// ServiceLocation identifies the regional Amazon Location Service
	// metadata-only scan slice. The scanner reads control-plane describe/list
	// management APIs for maps, place indexes, trackers, geofence collections,
	// and route calculators and never reads device positions, geofence
	// geometries, place-search results, route calculations, or map tiles, and
	// never mutates Location Service state.
	ServiceLocation = "location"
)

const (
	// ResourceTypeLocationMap identifies an Amazon Location Service map
	// resource. The scanner emits identity, the map style data source, the
	// configured map style and political view, and lifecycle timestamps only.
	// Map tiles, glyphs, sprites, and style descriptors stay outside the
	// contract.
	ResourceTypeLocationMap = "aws_location_map"
	// ResourceTypeLocationPlaceIndex identifies an Amazon Location Service place
	// index resource. The scanner emits identity, the geocoding data source, the
	// intended-use configuration, and lifecycle timestamps only. Place-search
	// queries and geocoding results stay outside the contract.
	ResourceTypeLocationPlaceIndex = "aws_location_place_index"
	// ResourceTypeLocationTracker identifies an Amazon Location Service tracker
	// resource. The scanner emits identity, the KMS encryption key identifier,
	// the position-filtering mode, the EventBridge and geospatial-query flags,
	// and lifecycle timestamps only. Device positions and position history stay
	// outside the contract.
	ResourceTypeLocationTracker = "aws_location_tracker"
	// ResourceTypeLocationGeofenceCollection identifies an Amazon Location
	// Service geofence collection resource. The scanner emits identity, the KMS
	// encryption key identifier, the geofence count, and lifecycle timestamps
	// only. Geofence geometries stay outside the contract.
	ResourceTypeLocationGeofenceCollection = "aws_location_geofence_collection"
	// ResourceTypeLocationRouteCalculator identifies an Amazon Location Service
	// route calculator resource. The scanner emits identity, the routing data
	// source, and lifecycle timestamps only. Route calculations and route
	// matrices stay outside the contract.
	ResourceTypeLocationRouteCalculator = "aws_location_route_calculator"
)

const (
	// RelationshipLocationTrackerUsesKMSKey records an Amazon Location Service
	// tracker's reported KMS encryption key dependency. The target is keyed by
	// the reported key identifier and targets aws_kms_key.
	RelationshipLocationTrackerUsesKMSKey = "location_tracker_uses_kms_key"
	// RelationshipLocationGeofenceCollectionUsesKMSKey records an Amazon Location
	// Service geofence collection's reported KMS encryption key dependency. The
	// target is keyed by the reported key identifier and targets aws_kms_key.
	RelationshipLocationGeofenceCollectionUsesKMSKey = "location_geofence_collection_uses_kms_key"
	// RelationshipLocationTrackerConsumesGeofenceCollection records an Amazon
	// Location Service tracker's consumer association with a geofence collection.
	// ListTrackerConsumers reports the geofence collection ARN, which the
	// geofence-collection node also publishes as its resource_id, so the edge
	// joins that node. The association is the control-plane link that evaluates
	// device positions against a collection's geofences; the scanner never reads
	// the geofence geometries or the device positions themselves.
	RelationshipLocationTrackerConsumesGeofenceCollection = "location_tracker_consumes_geofence_collection"
)

const (
	// ServiceDirectoryService identifies the regional AWS Directory Service
	// metadata-only scan slice. One scanner covers AWS Managed Microsoft AD,
	// Simple AD, and AD Connector directories plus their trust relationships,
	// shared-directory invitations, and LDAPS settings metadata.
	ServiceDirectoryService = "ds"
)

const (
	// ResourceTypeDSDirectory identifies an AWS Directory Service directory
	// metadata resource (type MicrosoftAD/SimpleAD/ADConnector, edition, size,
	// stage, and VPC settings). The scanner records the bare directory ID
	// (d-xxxxxxxxxx) as the resource_id so FSx Active Directory join edges, which
	// target the bare directory ID, resolve against it. Directory admin
	// passwords and RADIUS shared secrets are never read.
	ResourceTypeDSDirectory = "aws_ds_directory"
	// ResourceTypeDSTrust identifies an AWS Directory Service trust relationship
	// metadata resource (trust id, direction, type, state, and the FQDN of the
	// external domain). The scanner records the trust id (t-xxxxxxxxxx) as the
	// resource_id.
	ResourceTypeDSTrust = "aws_ds_trust"
	// ResourceTypeDSSharedDirectory identifies an AWS Directory Service shared
	// directory metadata resource (owner account, owner directory, shared
	// account, shared directory, share method, and share status). The scanner
	// records the owner directory id paired with the shared account id as the
	// resource_id so each share invitation is distinct.
	ResourceTypeDSSharedDirectory = "aws_ds_shared_directory"
)

const (
	// RelationshipDSDirectoryInVPC records a directory's reported VPC placement.
	// The target is the bare vpc-id so it joins the VPC scanner's resource_id.
	RelationshipDSDirectoryInVPC = "ds_directory_in_vpc"
	// RelationshipDSDirectoryInSubnet records a directory's reported subnet
	// placement (one edge per subnet). The target is the bare subnet-id so it
	// joins the VPC scanner's subnet resource_id.
	RelationshipDSDirectoryInSubnet = "ds_directory_in_subnet"
	// RelationshipDSTrustTargetsDirectory records the local directory a trust
	// relationship belongs to. The target is the bare directory id so it joins
	// the directory resource fact emitted in the same scan.
	RelationshipDSTrustTargetsDirectory = "ds_trust_targets_directory"
	// RelationshipDSSharedDirectoryTargetsOwnerDirectory records the owner
	// directory a shared-directory invitation references. The target is the bare
	// owner directory id so it joins the directory resource fact when the owner
	// directory is in scope.
	RelationshipDSSharedDirectoryTargetsOwnerDirectory = "ds_shared_directory_targets_owner_directory"
	// RelationshipDSSharedDirectoryTargetsOwnerAccount records the AWS account
	// that owns the shared directory. The target is the bare 12-digit account id
	// so it joins an account resource by id; target_arn is never synthesized.
	RelationshipDSSharedDirectoryTargetsOwnerAccount = "ds_shared_directory_targets_owner_account"
)

const (
	// ServiceAuditManager identifies the regional AWS Audit Manager metadata-only
	// scan slice. The scanner reads assessment, framework, and control
	// control-plane metadata through Audit Manager list/get management APIs
	// (ListAssessments, GetAssessment, ListAssessmentFrameworks, ListControls,
	// GetSettings, ListTagsForResource) and never reads collected audit evidence,
	// evidence finder records, change logs, delegation comments, control
	// narratives, or assessment report URLs, and never mutates Audit Manager
	// state.
	ServiceAuditManager = "auditmanager"
)

const (
	// ResourceTypeAuditManagerAssessment identifies an AWS Audit Manager
	// assessment metadata resource. The scanner emits identity, compliance
	// standard, status, framework reference, in-scope account ids/service names,
	// the assessment-reports S3 destination location, and lifecycle timestamps
	// only. Collected evidence, evidence folders, delegation comments, and the
	// assessment description free-text are never persisted.
	ResourceTypeAuditManagerAssessment = "aws_auditmanager_assessment"
	// ResourceTypeAuditManagerFramework identifies an AWS Audit Manager
	// assessment framework metadata resource. The scanner emits identity,
	// compliance standard, framework type (Standard/Custom), control-set and
	// control counts, and lifecycle timestamps only. The framework description
	// free-text and control narrative bodies are never persisted.
	ResourceTypeAuditManagerFramework = "aws_auditmanager_framework"
	// ResourceTypeAuditManagerControl identifies an AWS Audit Manager control
	// metadata resource. The scanner emits identity, control type
	// (Standard/Custom/Core), and evidence data-source category names only.
	// Control testing-information, action-plan instructions, control-mapping
	// source bodies, and Suricata/keyword evidence rules are never persisted.
	ResourceTypeAuditManagerControl = "aws_auditmanager_control"
)

const (
	// RelationshipAuditManagerAssessmentUsesFramework records an assessment's
	// dependency on the Audit Manager framework it was created from. The target
	// is keyed by the framework ARN so the edge joins the framework node the
	// scanner publishes.
	RelationshipAuditManagerAssessmentUsesFramework = "auditmanager_assessment_uses_framework"
	// RelationshipAuditManagerAssessmentReportsToS3 records an assessment's
	// configured assessment-reports S3 destination bucket. Audit Manager reports
	// an s3:// destination URI, so the scanner synthesizes the partition-aware
	// bucket ARN to match the S3 scanner's published bucket resource_id.
	RelationshipAuditManagerAssessmentReportsToS3 = "auditmanager_assessment_reports_to_s3"
	// RelationshipAuditManagerAssessmentEncryptedWithKMSKey records the KMS key
	// Audit Manager uses to encrypt the assessment's evidence and reports. Audit
	// Manager configures one account-level customer managed key (GetSettings), so
	// the edge keys the reported key ARN, matching how the KMS scanner publishes
	// its key resource_id.
	RelationshipAuditManagerAssessmentEncryptedWithKMSKey = "auditmanager_assessment_encrypted_with_kms_key"
	// RelationshipAuditManagerAssessmentInAccount records an AWS account included
	// in the assessment scope. The target keys the partition-aware account root
	// ARN (arn:<partition>:iam::<account-id>:root) so the edge joins the
	// aws_account identity the config, access-analyzer, and ds scanners also
	// target. In-scope service names are deprecated by AWS (the API returns them
	// empty) and are recorded as an assessment attribute, never an edge.
	RelationshipAuditManagerAssessmentInAccount = "auditmanager_assessment_in_account"
)

const (
	// ServiceAppFlow identifies the regional Amazon AppFlow metadata-only scan
	// slice covering flows and connector profiles. The slice never reads flow
	// records, field mappings, connector credentials, or OAuth tokens.
	ServiceAppFlow = "appflow"
)

const (
	// ResourceTypeAppFlowFlow identifies an Amazon AppFlow flow metadata
	// resource. Field mappings (task transforms) and flow run record contents
	// stay outside the contract because they can carry transferred data values.
	ResourceTypeAppFlowFlow = "aws_appflow_flow"
	// ResourceTypeAppFlowConnectorProfile identifies an Amazon AppFlow connector
	// profile metadata resource. Connector credentials and OAuth tokens stay
	// outside the contract; only the Secrets Manager credentials ARN reference
	// is recorded so the profile can join its secret node.
	ResourceTypeAppFlowConnectorProfile = "aws_appflow_connector_profile"
)

const (
	// RelationshipAppFlowFlowReadsFromS3Bucket records an AppFlow flow whose
	// source connector is Amazon S3, targeting the source bucket node.
	RelationshipAppFlowFlowReadsFromS3Bucket = "appflow_flow_reads_from_s3_bucket"
	// RelationshipAppFlowFlowWritesToS3Bucket records an AppFlow flow whose
	// destination connector is Amazon S3, targeting the destination bucket node.
	RelationshipAppFlowFlowWritesToS3Bucket = "appflow_flow_writes_to_s3_bucket"
	// RelationshipAppFlowFlowUsesConnectorProfile records an AppFlow flow's
	// reported source or destination connector profile dependency, keyed by the
	// connector profile name the connector-profile scanner publishes.
	RelationshipAppFlowFlowUsesConnectorProfile = "appflow_flow_uses_connector_profile"
	// RelationshipAppFlowFlowUsesKMSKey records an AppFlow flow's customer
	// provided KMS key used to encrypt the transferred data, targeting the KMS
	// key node by ARN.
	RelationshipAppFlowFlowUsesKMSKey = "appflow_flow_uses_kms_key"
	// RelationshipAppFlowConnectorProfileUsesSecret records an AppFlow connector
	// profile's reference to the Secrets Manager secret that stores its
	// credentials, targeting the secret node by ARN only. The credential values
	// themselves are never read.
	RelationshipAppFlowConnectorProfileUsesSecret = "appflow_connector_profile_uses_secret" // #nosec G101 -- relationship-type identifier recording a secret ARN reference, not a credential value
)

const (
	// ServiceAmplify identifies the regional AWS Amplify metadata scan slice.
	ServiceAmplify = "amplify"
)

const (
	// ResourceTypeAmplifyApp identifies an AWS Amplify app.
	ResourceTypeAmplifyApp = "aws_amplify_app"
	// ResourceTypeAmplifyBranch identifies an AWS Amplify branch that belongs to
	// an Amplify app.
	ResourceTypeAmplifyBranch = "aws_amplify_branch"
)

const (
	// RelationshipAmplifyAppDeploysFromRepository records the external Git source
	// repository an Amplify app deploys from. The repository is a non-AWS-resource
	// endpoint labeled with the shared git_repository join anchor, mirroring how
	// CodeBuild labels its source repositories.
	RelationshipAmplifyAppDeploysFromRepository = "amplify_app_deploys_from_repository"
	// RelationshipAmplifyAppUsesIAMRole records the IAM service role an Amplify app
	// assumes (the service role or, for SSR apps, the compute role).
	RelationshipAmplifyAppUsesIAMRole = "amplify_app_uses_iam_role"
	// RelationshipAmplifyAppServesCustomDomainViaHostedZone records a custom domain
	// an Amplify app serves whose apex resolves to a Route 53 hosted zone.
	RelationshipAmplifyAppServesCustomDomainViaHostedZone = "amplify_app_serves_custom_domain_via_hosted_zone"
	// RelationshipAmplifyAppServesCustomDomainViaCloudFront records a custom domain
	// an Amplify app serves whose subdomain DNS record resolves to a CloudFront
	// distribution domain.
	RelationshipAmplifyAppServesCustomDomainViaCloudFront = "amplify_app_serves_custom_domain_via_cloudfront"
	// RelationshipAmplifyBranchBelongsToApp records the Amplify app a branch
	// belongs to.
	RelationshipAmplifyBranchBelongsToApp = "amplify_branch_belongs_to_app"
)

const (
	// ServiceVerifiedPermissions identifies the regional Amazon Verified
	// Permissions metadata-only scan slice. The scanner reads policy store,
	// policy, and identity source control-plane metadata through the
	// verifiedpermissions management APIs (ListPolicyStores, GetPolicyStore,
	// ListPolicies, ListIdentitySources) and never reads or persists Cedar
	// policy statement bodies, schema bodies, policy template bodies, or any
	// authorization-request payload, and never mutates Verified Permissions
	// state.
	ServiceVerifiedPermissions = "verifiedpermissions"
)

const (
	// ResourceTypeVerifiedPermissionsPolicyStore identifies an Amazon Verified
	// Permissions policy store metadata resource. The scanner emits identity,
	// the validation mode, deletion-protection and encryption configuration
	// flags, Cedar language version, and lifecycle timestamps only. The Cedar
	// schema body stays outside the contract.
	ResourceTypeVerifiedPermissionsPolicyStore = "aws_verifiedpermissions_policy_store"
	// ResourceTypeVerifiedPermissionsPolicy identifies an Amazon Verified
	// Permissions policy metadata resource. The scanner emits the policy id,
	// policy type (STATIC or TEMPLATE_LINKED), effect, and parent policy store
	// id only. The Cedar policy statement body is never read or persisted.
	ResourceTypeVerifiedPermissionsPolicy = "aws_verifiedpermissions_policy"
	// ResourceTypeVerifiedPermissionsIdentitySource identifies an Amazon
	// Verified Permissions identity source metadata resource. The scanner emits
	// the identity source id, the principal Cedar entity type, the configured
	// provider kind (Cognito user pool or OIDC), and the OIDC issuer URL or
	// Cognito user pool reference only. Application client secrets and token
	// payloads are never read or persisted.
	ResourceTypeVerifiedPermissionsIdentitySource = "aws_verifiedpermissions_identity_source"
)

const (
	// RelationshipVerifiedPermissionsPolicyInStore records a Verified
	// Permissions policy's membership in its parent policy store. The target is
	// keyed by the policy store id the policy store node publishes, so the edge
	// joins the policy store node exactly.
	RelationshipVerifiedPermissionsPolicyInStore = "verifiedpermissions_policy_in_store"
	// RelationshipVerifiedPermissionsIdentitySourceInStore records a Verified
	// Permissions identity source's membership in its parent policy store. The
	// target is keyed by the policy store id the policy store node publishes.
	RelationshipVerifiedPermissionsIdentitySourceInStore = "verifiedpermissions_identity_source_in_store"
	// RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool records a
	// Verified Permissions identity source's dependency on an Amazon Cognito
	// user pool. The target is keyed by the bare Cognito user pool id parsed
	// from the reported user pool ARN, matching the resource_id the Cognito
	// scanner publishes for a user pool node.
	RelationshipVerifiedPermissionsIdentitySourceUsesCognitoUserPool = "verifiedpermissions_identity_source_uses_cognito_user_pool"
)

const (
	// ServiceRolesAnywhere identifies the regional AWS IAM Roles Anywhere
	// metadata-only scan slice. The scanner reads trust-anchor, profile, and
	// certificate-revocation-list (CRL) control-plane metadata through the
	// rolesanywhere management APIs (ListTrustAnchors, ListProfiles, ListCrls,
	// ListTagsForResource). It never reads or persists certificate private
	// material, PEM certificate bundles, CRL bodies, session policy documents, or
	// vended session credentials, and never mutates Roles Anywhere state.
	ServiceRolesAnywhere = "rolesanywhere"
)

const (
	// ResourceTypeRolesAnywhereTrustAnchor identifies an AWS IAM Roles Anywhere
	// trust anchor metadata resource. The scanner emits identity, enabled state,
	// and the trust-anchor source type (AWS_ACM_PCA, CERTIFICATE_BUNDLE, or
	// SELF_SIGNED_REPOSITORY) only. The PEM x509 certificate bundle and ACM PCA
	// root certificate material stay outside the contract.
	ResourceTypeRolesAnywhereTrustAnchor = "aws_rolesanywhere_trust_anchor"
	// ResourceTypeRolesAnywhereProfile identifies an AWS IAM Roles Anywhere
	// profile metadata resource. The scanner emits identity, enabled state,
	// session duration, the assumable IAM role and managed-policy ARNs, and
	// boolean flags only. The inline session policy document and certificate
	// attribute-mapping rules stay outside the contract.
	ResourceTypeRolesAnywhereProfile = "aws_rolesanywhere_profile"
	// ResourceTypeRolesAnywhereCRL identifies an AWS IAM Roles Anywhere imported
	// certificate revocation list (CRL) metadata resource. The scanner emits
	// identity, enabled state, and the associated trust-anchor ARN only. The
	// CRL body bytes are never read or persisted.
	ResourceTypeRolesAnywhereCRL = "aws_rolesanywhere_crl"
)

const (
	// RelationshipRolesAnywhereProfileAssumesRole records an AWS IAM Roles
	// Anywhere profile's reported dependency on an IAM role it can vend session
	// credentials for. The target is keyed by the role ARN, which matches how the
	// IAM scanner publishes its role resource_id, so the edge joins the IAM role
	// node instead of dangling.
	RelationshipRolesAnywhereProfileAssumesRole = "rolesanywhere_profile_assumes_role"
	// RelationshipRolesAnywhereTrustAnchorUsesACMPCA records an AWS IAM Roles
	// Anywhere trust anchor's reported dependency on an AWS Private CA (ACM PCA)
	// certificate authority. It is emitted only for trust anchors whose source is
	// AWS_ACM_PCA. The target is keyed by the CA ARN, which matches how the
	// acmpca scanner publishes its certificate-authority resource_id.
	RelationshipRolesAnywhereTrustAnchorUsesACMPCA = "rolesanywhere_trust_anchor_uses_acmpca"
	// RelationshipRolesAnywhereCRLValidatesTrustAnchor records that an imported
	// certificate revocation list (CRL) provides revocation for a trust anchor.
	// The target is keyed by the trust-anchor ARN, which is the resource_id the
	// trust-anchor node publishes, so the edge joins an internal scanner node.
	RelationshipRolesAnywhereCRLValidatesTrustAnchor = "rolesanywhere_crl_validates_trust_anchor"
)
