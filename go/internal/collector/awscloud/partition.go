// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import "strings"

// AWS partition identifiers. An ARN's second segment is its partition, and it is
// NOT optional: a GovCloud or China resource ARN carries aws-us-gov / aws-cn,
// not the commercial aws. Synthesizing an ARN with a hardcoded aws partition
// produces an identity that never joins the real resource node in those
// partitions, silently dangling the graph edge. Scanners MUST derive the
// partition from the scan boundary or a source ARN instead of hardcoding it.
const (
	// PartitionAWS is the commercial partition (the default for unknown or
	// blank regions).
	PartitionAWS = "aws"
	// PartitionGovCloud is the AWS GovCloud (US) partition.
	PartitionGovCloud = "aws-us-gov"
	// PartitionChina is the AWS China partition.
	PartitionChina = "aws-cn"
)

// PartitionForRegion returns the AWS partition for an AWS region: us-gov-* maps
// to aws-us-gov, cn-* maps to aws-cn, and every other (including blank) region
// maps to the commercial aws partition. Scanners use it to synthesize ARNs that
// match the real resource node's partition in GovCloud and China instead of
// dangling the graph join.
func PartitionForRegion(region string) string {
	region = strings.TrimSpace(region)
	switch {
	case strings.HasPrefix(region, "us-gov-"):
		return PartitionGovCloud
	case strings.HasPrefix(region, "cn-"):
		return PartitionChina
	default:
		return PartitionAWS
	}
}

// PartitionForBoundary returns the AWS partition for a scan boundary's region.
// It is the boundary-keyed convenience wrapper over PartitionForRegion for the
// common case where a scanner synthesizes an ARN for a resource in its own
// claimed boundary.
func PartitionForBoundary(boundary Boundary) string {
	return PartitionForRegion(boundary.Region)
}

// PartitionFromARN returns the partition segment of an AWS ARN (aws, aws-cn,
// aws-us-gov), or aws when value is not an ARN with a non-empty partition
// segment. Scanners use it to make a synthesized ARN inherit the partition of a
// source ARN observed in the same describe response (e.g. a model or cluster
// ARN that references an S3 bucket), so the synthesized identity matches the
// real partition without consulting the region.
func PartitionFromARN(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return PartitionAWS
	}
	parts := strings.SplitN(trimmed, ":", 3)
	if len(parts) < 3 {
		return PartitionAWS
	}
	if partition := strings.TrimSpace(parts[1]); partition != "" {
		return partition
	}
	return PartitionAWS
}
