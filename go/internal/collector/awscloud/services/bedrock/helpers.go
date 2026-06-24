// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bedrock

import (
	"strings"
	"time"
)

// firstNonEmpty returns the first trimmed non-empty value, or "" when every
// candidate is blank. Scanner identity selection prefers ARNs over names.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// isARN reports whether value looks like an AWS ARN. The scanner uses it to
// guard relationship targets so a free-form name is never emitted as an ARN.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// parseS3URL splits an `s3://bucket[/key]` URL into the bucket name and
// optional object key. It returns ok=false when the input is not an `s3://`
// URL or carries no bucket segment.
func parseS3URL(url string) (bucket string, key string, ok bool) {
	trimmed := strings.TrimSpace(url)
	if !strings.HasPrefix(trimmed, "s3://") {
		return "", "", false
	}
	remainder := strings.TrimPrefix(trimmed, "s3://")
	if remainder == "" {
		return "", "", false
	}
	bucket, key, _ = strings.Cut(remainder, "/")
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return "", "", false
	}
	return bucket, strings.TrimSpace(key), true
}

// s3BucketARN synthesizes an `arn:<partition>:s3:::<bucket>` ARN, taking the
// partition from sourceARN so GovCloud and China resources do not get a
// commercial-partition target.
func s3BucketARN(partition, bucket string) string {
	return "arn:" + partition + ":s3:::" + bucket
}

// cloneStringMap copies a string map, dropping blank keys, and returns nil for
// an empty result so emitted payloads stay stable across observations.
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

// cloneStrings copies a slice, dropping blank entries and de-duplicating, and
// returns nil for an empty result.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// timeOrNil returns the UTC time for a non-zero value or nil so emitted
// payloads omit unknown timestamps rather than carrying a zero value.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
