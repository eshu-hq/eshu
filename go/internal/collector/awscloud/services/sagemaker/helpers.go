package sagemaker

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

// arnPartition returns the partition segment of an AWS ARN (aws, aws-cn,
// aws-us-gov), or "aws" when value is not an ARN with a partition segment. The
// scanner uses it so a synthesized S3 bucket ARN inherits the partition of the
// model that referenced it instead of hardcoding the commercial partition,
// which would dangle the model->artifact edge in GovCloud and China.
func arnPartition(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return "aws"
	}
	parts := strings.SplitN(trimmed, ":", 3)
	if len(parts) < 3 {
		return "aws"
	}
	if partition := strings.TrimSpace(parts[1]); partition != "" {
		return partition
	}
	return "aws"
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
