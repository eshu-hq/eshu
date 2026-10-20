// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mq

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

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
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

// configurationARNByID maps each configuration ID to its ARN so the
// broker→configuration relationship can target the ARN the configuration
// resource publishes as its ResourceID. Entries without both an ID and an ARN
// are skipped because they cannot resolve a broker reference.
func configurationARNByID(configurations []Configuration) map[string]string {
	if len(configurations) == 0 {
		return nil
	}
	byID := make(map[string]string, len(configurations))
	for _, configuration := range configurations {
		id := strings.TrimSpace(configuration.ID)
		arn := strings.TrimSpace(configuration.ARN)
		if id == "" || arn == "" {
			continue
		}
		byID[id] = arn
	}
	if len(byID) == 0 {
		return nil
	}
	return byID
}

// isARN reports whether value begins with the AWS ARN prefix after trimming.
// Amazon MQ reports the encryption KMS key as a customer master key ARN when a
// customer-managed key is used; the KMS relationship targets the ARN form so
// reducers receive a globally addressable target identity.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}
