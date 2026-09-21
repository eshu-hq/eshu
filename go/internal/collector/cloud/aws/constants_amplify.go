// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
