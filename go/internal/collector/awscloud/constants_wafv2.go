// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
	RelationshipWAFv2WebACLProtectsResource = "wafv2_web_acl_protects_resource"
	// RelationshipWAFv2WebACLUsesRuleGroup records a web ACL reference to a
	// customer-owned rule group by ARN.
	RelationshipWAFv2WebACLUsesRuleGroup = "wafv2_web_acl_uses_rule_group"
	// RelationshipWAFv2WebACLUsesIPSet records a web ACL reference to an IP set
	// by ARN.
	RelationshipWAFv2WebACLUsesIPSet = "wafv2_web_acl_uses_ip_set"
	// RelationshipWAFv2WebACLUsesRegexPatternSet records a web ACL reference to a
	// regex pattern set by ARN.
	RelationshipWAFv2WebACLUsesRegexPatternSet = "wafv2_web_acl_uses_regex_pattern_set"
)

// WAF Classic (v1) is out of scope by construction: the scanner imports only
// the WAFv2 SDK (github.com/aws/aws-sdk-go-v2/service/wafv2), which cannot
// surface waf or waf-regional v1 resources. There is therefore no runtime path
// that observes a v1 resource to skip. If v1 visibility is ever required, add a
// dedicated WAF Classic scanner rather than extending this slice.
