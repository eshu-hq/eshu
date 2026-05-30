package awssdk

import "strings"

// bucketARN synthesizes the S3 bucket ARN for the claim region's partition. S3
// buckets have no ARN in the API response, so the adapter synthesizes one; it
// must carry the real partition (aws / aws-cn / aws-us-gov) because the bucket
// node identity is what partition-aware consumers join against. A blank region
// falls back to the commercial partition.
func bucketARN(region, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + partitionForRegion(region) + ":s3:::" + name
}

// partitionForRegion maps an AWS region to its partition: us-gov-* -> aws-us-gov,
// cn-* -> aws-cn, everything else (including blank) -> aws.
func partitionForRegion(region string) string {
	region = strings.TrimSpace(region)
	switch {
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	default:
		return "aws"
	}
}
