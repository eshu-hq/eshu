// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceLakeFormation identifies the regional AWS Lake Formation
	// metadata-only scan slice covering data-lake settings, registered data
	// locations, and the principal/resource permission grants that govern the
	// Glue Data Catalog. Permission policy bodies, LF-Tag values, and principal
	// credentials stay outside the scan slice; only grant identities, principal
	// identifiers, and resource ARNs are emitted.
	ServiceLakeFormation = "lakeformation"
)

const (
	// ResourceTypeLakeFormationResource identifies an AWS Lake Formation
	// registered data location (a `ListResources` entry). Its resource_id is the
	// registered location ARN AWS reports, which is the join key the registered
	// resource's S3-bucket and IAM-role edges resolve against.
	ResourceTypeLakeFormationResource = "aws_lakeformation_resource"
	// ResourceTypeLakeFormationSettings identifies the AWS Lake Formation
	// data-lake settings resource for one account and region. It carries the
	// data-lake administrator and read-only administrator principal identifiers
	// only; no permission body, LF-Tag value, or credential is persisted.
	ResourceTypeLakeFormationSettings = "aws_lakeformation_settings"
	// ResourceTypeLakeFormationPermission identifies one AWS Lake Formation
	// principal/resource permission grant (a `ListPermissions` entry). It carries
	// the grant identity: the principal identifier, the governed resource
	// reference, and the bounded AWS privilege enum names (SELECT, ALTER, ...).
	// No condition expression, LF-Tag value, or policy body is persisted.
	ResourceTypeLakeFormationPermission = "aws_lakeformation_permission"
)

const (
	// RelationshipLakeFormationResourceAtS3Bucket records a Lake Formation
	// registered data location's S3 bucket, derived from the registered location
	// ARN. The target ARN inherits the registered ARN's partition so GovCloud and
	// China joins resolve to the bucket node the S3 scanner publishes.
	RelationshipLakeFormationResourceAtS3Bucket = "lakeformation_resource_at_s3_bucket"
	// RelationshipLakeFormationResourceUsesIAMRole records the IAM role that
	// registered a Lake Formation data location.
	RelationshipLakeFormationResourceUsesIAMRole = "lakeformation_resource_uses_iam_role"
	// RelationshipLakeFormationPermissionOnGlueDatabase records a Lake Formation
	// grant governing a Glue Data Catalog database. The target_resource_id is the
	// bare database name the Glue scanner publishes as its database resource_id.
	RelationshipLakeFormationPermissionOnGlueDatabase = "lakeformation_permission_on_glue_database"
	// RelationshipLakeFormationPermissionOnGlueTable records a Lake Formation
	// grant governing a Glue Data Catalog table. The target_resource_id is the
	// `database/table` identifier the Glue scanner publishes as its table
	// resource_id.
	RelationshipLakeFormationPermissionOnGlueTable = "lakeformation_permission_on_glue_table"
	// RelationshipLakeFormationPermissionGrantedToPrincipal records the IAM
	// principal a Lake Formation grant is granted to, emitted only when the
	// principal identifier is an IAM role ARN.
	RelationshipLakeFormationPermissionGrantedToPrincipal = "lakeformation_permission_granted_to_principal"
)
