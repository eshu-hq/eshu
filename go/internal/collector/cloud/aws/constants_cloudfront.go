// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCloudFront identifies the global Amazon CloudFront metadata scan
	// slice.
	ServiceCloudFront = "cloudfront"
)

const (
	// ResourceTypeCloudFrontDistribution identifies a CloudFront distribution
	// metadata resource.
	ResourceTypeCloudFrontDistribution = "aws_cloudfront_distribution"
)

const (
	// RelationshipCloudFrontDistributionUsesACMCertificate records a
	// CloudFront distribution's reported ACM certificate dependency.
	RelationshipCloudFrontDistributionUsesACMCertificate = "cloudfront_distribution_uses_acm_certificate"
	// RelationshipCloudFrontDistributionUsesWAFWebACL records a CloudFront
	// distribution's reported WAF web ACL dependency.
	RelationshipCloudFrontDistributionUsesWAFWebACL = "cloudfront_distribution_uses_waf_web_acl"
)
