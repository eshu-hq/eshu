// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ram_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// resourceTypeFromEnvelope returns the resource_type payload field for a
// resource fact, used by scanner tests that assert on emitted resource kinds.
func resourceTypeFromEnvelope(t *testing.T, envelope facts.Envelope) string {
	t.Helper()
	return payloadString(envelope, "resource_type")
}

// payloadString returns the top-level string payload field for key, or the
// empty string when it is absent or not a string.
func payloadString(envelope facts.Envelope, key string) string {
	value, _ := envelope.Payload[key].(string)
	return value
}

// payloadAttribute returns the string value nested under the relationship or
// resource payload's "attributes" map for key, or the empty string when it is
// absent or not a string. It lets scanner tests assert on attribute-only fields
// such as a RAM-reported resource_type that is not promoted to the edge target.
func payloadAttribute(t *testing.T, envelope facts.Envelope, key string) string {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := attributes[key].(string)
	return value
}
