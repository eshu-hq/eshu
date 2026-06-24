// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codecommit

import (
	"net/url"
	"strings"
	"time"
)

// firstNonEmpty returns the first trimmed non-empty value, or the empty string
// when every value is blank.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time, or nil when the time is the zero value, so a
// missing timestamp serializes as a null attribute rather than the Go zero
// date.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

// isARN reports whether value has the canonical AWS ARN prefix. The scanner
// uses it to decide whether a reported KMS key id or trigger destination is an
// ARN-keyed join target.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// arnService returns the service segment (the third colon-delimited field) of
// an AWS ARN, or "" when value is not an ARN with a service segment. Matching
// the service segment exactly avoids mis-classifying an ARN whose resource
// portion merely contains a service-like substring (e.g. a Lambda function ARN
// whose name embeds "sns").
func arnService(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 4)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// isSNSTopicARN reports whether value is an SNS topic ARN. CodeCommit trigger
// destinations are most commonly SNS topics; only those produce a typed
// repository-to-SNS-topic edge. It matches the ARN service segment exactly so a
// non-SNS ARN that merely contains "sns" in its resource portion is not
// promoted to an SNS edge.
func isSNSTopicARN(value string) bool {
	return isARN(value) && arnService(value) == "sns"
}

// cloneURLHost extracts the host segment of a clone URL so the scanner records
// the clone endpoint host without persisting credentials, paths, or any
// user:password userinfo that a clone URL string could otherwise carry. It
// returns the empty string when value is blank or has no resolvable host.
func cloneURLHost(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

// cloneStringMap returns a copy of input with blank keys dropped, or nil when no
// usable entries remain so omitempty-style payload behavior stays consistent.
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

// cloneStringSlice returns a copy of input with blank values dropped, or nil
// when no usable entries remain.
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
