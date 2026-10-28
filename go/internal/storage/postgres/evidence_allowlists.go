// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// awsCloudInventoryAttributeAllowlist is the closed image/version allowlist
// for aws_resource attributes (issues #5449 and #9298). It surfaces AWS
// runtime and patch-level fields the collector observes -- an ECS task's
// running container image and digest plus its owning task definition; a
// Lambda function's image URI/digest, code version, code_sha256, package_type,
// and runtime; EKS platform, nodegroup release and add-on versions; and EC2
// instance AMI id -- while dropping every other AWS attribute key (cluster_arn,
// role_arn, kms_key_arn, network_interfaces, environment, vpc_config, and any
// key not named here). package_type is the bounded Lambda packaging
// discriminator ("Zip"/"Image") the readback uses to attach the #5454
// deployment-code correlation limitation to a zip-packaged Lambda whose
// code_sha256 has no collected CI/package/OCI counterpart; it is a
// low-cardinality enum, never a locator or secret.
var awsCloudInventoryAttributeAllowlist = cloudInventoryAttributeAllowlist{
	scalarKeys: map[string]struct{}{
		"task_definition_arn": {},
		"image_uri":           {},
		"resolved_image_uri":  {},
		"code_sha256":         {},
		"package_type":        {},
		"version":             {},
		"platform_version":    {},
		"release_version":     {},
		"addon_version":       {},
		"ami_id":              {},
		"runtime":             {},
	},
	nestedArrayKeys: map[string]map[string]struct{}{
		// containers' sub-key set is intentionally maintained independently of
		// cloudInventoryContainerAttributeKeys in
		// go/internal/query/cloud_inventory_read_model.go, which applies the same
		// {image, image_digest} sub-key set as a second, independent gate on the
		// already-filtered value. There is no shared constant and no test tying
		// the two together across packages: this loader is the sole upstream of
		// that projector's input (every containers value it ever sees already
		// passed through this filter), so drift between the two sets can only
		// ever make the read model MORE restrictive than this loader -- i.e.
		// silently drop container data the loader already allowed -- never leak
		// a raw sub-key this loader dropped. The query package pins its own set
		// with TestCloudInventoryContainerAttributeKeysIsImageAndDigestOnly so a
		// change there is caught by a test even without a cross-package tie.
		"containers": {
			"image":        {},
			"image_digest": {},
		},
	},
}

// azureCloudInventoryAttributeAllowlist is the closed image/version allowlist
// for azure_cloud_resource attributes (issue #5449). It is intentionally EMPTY
// today: the azure_cloud_resource fact's attributes map carries only identity
// and boundary fields (arm_resource_id, subscription_id, resource_group,
// tenant_id, tags, the redacted "extension" object, ...), never an image or
// version field -- Azure's runtime image evidence is emitted as a separate
// azure_image_reference fact kind that this admission mapping does not
// consume. The allowlist mechanism is wired now so Azure image/version keys
// can be added the moment a source fact carries them, without touching the
// filter path again; see TestCloudInventoryRecordFromRowAzureAttributesAlwaysDropped
// for the regression guard that every current Azure key stays dropped.
var azureCloudInventoryAttributeAllowlist = cloudInventoryAttributeAllowlist{}

const awsCloudInventoryAppSyncGraphQLAPIResourceType = "aws_appsync_graphql_api"

// awsCloudInventoryAppSyncGraphQLAPIAttributeAllowlist exposes only the
// non-secret count and lifecycle timestamps from an AppSync GraphQL API.
// API key Id values and descriptions never enter cloud inventory readback.
var awsCloudInventoryAppSyncGraphQLAPIAttributeAllowlist = cloudInventoryAttributeAllowlist{
	scalarKeys: map[string]struct{}{
		"api_key_count": {},
	},
	nestedArrayKeys: map[string]map[string]struct{}{
		"api_key_lifecycles": {
			"expires": {},
			"deletes": {},
		},
	},
}
