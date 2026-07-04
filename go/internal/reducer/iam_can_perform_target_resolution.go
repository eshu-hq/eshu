// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
)

// iamCanPerformResourceTypeOfARN classifies a non-IAM AWS resource ARN to the
// matching CAN_PERFORM resource_type token, so target resolution can require the
// resolved node be the right service family. It generalizes the escalation
// iamResourceTypeOfARN classifier from IAM to the S3/KMS/SecretsManager/SSM/
// DynamoDB/EC2/RDS/Lambda services the catalog covers. Returns "" for an
// unrecognized or out-of-catalog ARN. Resolution still requires the ARN be a
// scanned node, so a classification alone never fabricates an edge.
func iamCanPerformResourceTypeOfARN(arn string) string {
	// ARN form: arn:partition:service:region:account:resource (S3 omits region and
	// account, so the resource segment is everything after the service's colons).
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ""
	}
	service := parts[2]
	resource := parts[5]
	switch service {
	case "s3":
		// arn:aws:s3:::bucket[/key] — a bucket node is keyed on the bucket ARN with
		// no object key, so only a bucket-shaped ARN (no "/") classifies.
		if resource != "" && !strings.Contains(resource, "/") {
			return iamCanPerformResourceTypeS3Bucket
		}
	case "kms":
		if strings.HasPrefix(resource, "key/") {
			return iamCanPerformResourceTypeKMSKey
		}
	case "secretsmanager":
		if strings.HasPrefix(resource, "secret:") {
			return iamCanPerformResourceTypeSecret
		}
	case "ssm":
		if strings.HasPrefix(resource, "parameter/") {
			return iamCanPerformResourceTypeSSMParam
		}
	case "dynamodb":
		// arn:aws:dynamodb:region:acct:table/Name — only the table itself (not an
		// index or stream sub-resource) is a scanned CloudResource node.
		if strings.HasPrefix(resource, "table/") && !strings.Contains(strings.TrimPrefix(resource, "table/"), "/") {
			return iamCanPerformResourceTypeDynamoDB
		}
	case "ec2":
		if strings.HasPrefix(resource, "instance/") {
			return iamCanPerformResourceTypeEC2Instance
		}
	case "rds":
		if strings.HasPrefix(resource, "db:") {
			return iamCanPerformResourceTypeRDSInstance
		}
	case "lambda":
		functionName := strings.TrimPrefix(resource, "function:")
		if functionName != resource && functionName != "" && !strings.Contains(functionName, ":") {
			return iamCanPerformResourceTypeLambdaFunc
		}
	}
	return ""
}

// resolveIAMCanPerformTarget reads the resource ARNs from the statements that
// granted a catalog action and resolves them against the scanned CloudResource
// join index, requiring the matched node classify as the catalog entry's expected
// resource type. The resolution ladder mirrors CAN_ESCALATE_TO: exact ARN ->
// single prefix/glob -> wildcard/many (ambiguous) -> zero (unresolved). It returns
// the resolved uid, the resolution mode (exact_arn / single_glob), and the status.
func resolveIAMCanPerformTarget(
	index cloudResourceJoinIndex,
	grant iamPrincipalGrant,
	entry iamCanPerformAction,
) (string, string, iamTargetStatus) {
	resources := collectTrustedResources(grant.statementsCovering(entry.Action))
	if len(resources) == 0 {
		return "", "", iamTargetUnresolved
	}

	exactMatches := make(map[string]struct{})
	globMatches := make(map[string]struct{})
	sawWildcard := false
	for _, pattern := range resources {
		if pattern == "*" {
			sawWildcard = true
			continue
		}
		if strings.ContainsAny(pattern, "*?") {
			for arn, uid := range index.byARN {
				if iamCanPerformResourceTypeOfARN(arn) != entry.ExpectedResourceType {
					continue
				}
				if globMatch(pattern, arn) {
					globMatches[uid] = struct{}{}
				}
			}
			continue
		}
		if uid, ok := index.byARN[pattern]; ok && iamCanPerformResourceTypeOfARN(pattern) == entry.ExpectedResourceType {
			exactMatches[uid] = struct{}{}
		}
	}

	// An exact-ARN match is the most confident resolution: prefer it and report
	// exact_arn. Only when there is no exact match do glob matches decide.
	switch {
	case len(exactMatches) == 1:
		return singleUID(exactMatches), iamCanPerformResolutionExactARN, iamTargetResolved
	case len(exactMatches) > 1:
		return "", "", iamTargetAmbiguous
	}

	merged := make(map[string]struct{}, len(globMatches))
	for uid := range globMatches {
		merged[uid] = struct{}{}
	}
	switch {
	case len(merged) == 1:
		return singleUID(merged), iamCanPerformResolutionSingleGlob, iamTargetResolved
	case len(merged) > 1:
		return "", "", iamTargetAmbiguous
	case sawWildcard:
		// A bare "*" (or only-glob with no scanned match) names no single node.
		return "", "", iamTargetAmbiguous
	}
	return "", "", iamTargetUnresolved
}

// singleUID returns the only uid in a single-element set. The caller guarantees
// len(set) == 1.
func singleUID(set map[string]struct{}) string {
	for uid := range set {
		return uid
	}
	return ""
}
