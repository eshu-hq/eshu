// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesis

import (
	"strings"
	"time"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

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

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// isARN reports whether value begins with the AWS ARN prefix after trimming.
// Kinesis relationships that target IAM, KMS, S3, or OpenSearch anchors emit
// only when AWS reports the ARN form so reducers receive a globally
// addressable target identity, matching the MSK precedent.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}
