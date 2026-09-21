// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firehose

import (
	"strings"
	"time"
)

// destinationKindS3 classifies an Amazon S3 (or extended S3) Firehose
// destination.
const destinationKindS3 = "s3"

// destinationKindRedshift classifies an Amazon Redshift Firehose destination.
const destinationKindRedshift = "redshift"

// destinationKindOpenSearch classifies an Amazon OpenSearch Service (or legacy
// Elasticsearch) Firehose destination.
const destinationKindOpenSearch = "opensearch"

// destinationKindSplunk classifies a Splunk Firehose destination.
const destinationKindSplunk = "splunk"

// destinationKindHTTPEndpoint classifies a generic HTTP endpoint Firehose
// destination.
const destinationKindHTTPEndpoint = "http_endpoint"

// firstNonEmpty returns the first trimmed, non-empty string in values, or "" if
// every value is blank. The scanner uses it to prefer an ARN identity over a
// bare name without persisting an empty identity.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// isARN reports whether value has the canonical AWS ARN prefix. The scanner
// emits ARN-keyed relationships only when the reported target identity is
// ARN-shaped so the edge joins the ARN-keyed target node.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// timeOrNil returns value in UTC, or nil when the timestamp is the zero value,
// so the scanner never persists a synthetic "0001-01-01" creation time.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a defensive copy of input with blank keys dropped, or
// nil when no entries remain. The scanner uses it so AWS tag maps cannot be
// mutated through the emitted fact payload.
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

// dedupeNonEmpty returns the input order-preserving with blank entries dropped
// and duplicates collapsed, or nil when nothing remains. The scanner uses it
// for the per-stream destination-kind summary.
func dedupeNonEmpty(input []string) []string {
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
