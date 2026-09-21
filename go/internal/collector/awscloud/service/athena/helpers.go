// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package athena

import (
	"strings"
	"time"
)

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// outputBucketARN converts an Athena workgroup OutputLocation `s3://bucket[/...]`
// URI into an `arn:<partition>:s3:::bucket` ARN. The partition is supplied by the
// caller (derived from the scan boundary) so the synthesized ARN matches the S3
// bucket scanner's resource_id in GovCloud and China instead of dangling. It
// returns an empty string when the location is missing, malformed, or not an S3
// URI so result-object contents never leak into the relationship payload.
func outputBucketARN(partition, outputLocation string) string {
	trimmed := strings.TrimSpace(outputLocation)
	if trimmed == "" {
		return ""
	}
	const prefix = "s3://"
	if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
		return ""
	}
	remainder := trimmed[len(prefix):]
	bucket := remainder
	if slash := strings.IndexByte(remainder, '/'); slash >= 0 {
		bucket = remainder[:slash]
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return ""
	}
	return "arn:" + partition + ":s3:::" + bucket
}
