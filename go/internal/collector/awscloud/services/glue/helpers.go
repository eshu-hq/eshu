// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package glue

import (
	"strings"
	"time"
)

// secretKeyFragments collects the substrings the scanner treats as evidence
// that a default-argument or connection-property key may carry secret
// material. The scanner drops any matching key before persisting evidence.
var secretKeyFragments = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"apikey",
	"api-key",
	"api_key",
	"auth",
	"credential",
	"private-key",
	"private_key",
	"privatekey",
	"client-secret",
	"client_secret",
	"clientsecret",
	"signing",
	"oauth",
	"sasl",
}

// isSecretShapedKey reports whether key looks like it carries credential
// material. The check is case-insensitive and substring-based so that AWS
// argument naming variants like "--AwsSecretAccessKey", "AUTH_TOKEN", or
// "ENCRYPTED_PASSWORD" all redact.
func isSecretShapedKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return true
	}
	for _, fragment := range secretKeyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// filterSafeKeys returns a copy of input with any secret-shaped key dropped.
// The result preserves the input order and is nil when no safe keys remain so
// that the omitempty-style payload behavior stays consistent across services.
func filterSafeKeys(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, key := range input {
		if trimmed := strings.TrimSpace(key); trimmed != "" && !isSecretShapedKey(trimmed) {
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

func cloneStringSlice(input []string) []string {
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

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}
