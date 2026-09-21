// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appflow

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// secretsManagerService is the ARN service segment a Secrets Manager secret
// carries. The connector-profile-to-secret edge is emitted only when the
// reported credentials ARN parses as a Secrets Manager ARN, matched by exact
// segment equality rather than substring containment so an unrelated ARN that
// merely contains the word never produces a dangling edge.
const secretsManagerService = "secretsmanager"

// bucketARN synthesizes the S3 bucket ARN the S3 scanner publishes as its
// resource_id (`arn:<partition>:s3:::<bucket>`). The partition is derived from
// the flow ARN observed in the same describe response so a GovCloud or China
// flow joins the real bucket node instead of dangling on a hardcoded `aws`.
// When the flow ARN is absent the boundary region supplies the partition.
func bucketARN(boundary awscloud.Boundary, flowARN, bucket string) string {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return ""
	}
	partition := awscloud.PartitionForBoundary(boundary)
	if arn := strings.TrimSpace(flowARN); arn != "" {
		partition = awscloud.PartitionFromARN(arn)
	}
	return "arn:" + partition + ":s3:::" + bucket
}

// isSecretsManagerARN reports whether value is an ARN whose service segment is
// exactly secretsmanager. It parses the colon-delimited ARN fields rather than
// using substring containment, so an IAM or KMS ARN that incidentally contains
// the substring does not pass.
func isSecretsManagerARN(value string) bool {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return false
	}
	// ARN shape: arn:partition:service:region:account:resource. The service is
	// the third colon-delimited field (index 2).
	fields := strings.SplitN(trimmed, ":", 6)
	if len(fields) < 3 {
		return false
	}
	return strings.TrimSpace(fields[2]) == secretsManagerService
}

// isARN reports whether value has the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// timeOrNil returns the UTC time, or nil for the zero value, so empty
// timestamps render as a missing attribute rather than the Go zero time.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
