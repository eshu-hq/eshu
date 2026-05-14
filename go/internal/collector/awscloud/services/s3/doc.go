// Package s3 maps Amazon S3 bucket metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence bucket resources plus relationships
// for server-access-log delivery targets. Object inventory, bucket policy JSON,
// ACL grants, replication rules, lifecycle rules, notification configuration,
// and mutation APIs stay outside this package contract.
package s3
