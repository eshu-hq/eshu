// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceS3 identifies the regional Amazon Simple Storage Service bucket
	// metadata scan slice.
	ServiceS3 = "s3"
)

const (
	// ResourceTypeS3Bucket identifies an S3 bucket metadata resource.
	ResourceTypeS3Bucket = "aws_s3_bucket"
)

const (
	// RelationshipS3BucketLogsToBucket records S3 server-access-log delivery
	// metadata from a source bucket to its target bucket.
	RelationshipS3BucketLogsToBucket = "s3_bucket_logs_to_bucket"
)
