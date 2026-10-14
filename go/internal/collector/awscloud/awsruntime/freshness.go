// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"encoding/json"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// awsFreshnessHint returns the stable content digest for one AWS service scan
// (#8003). Each emitted envelope contributes its fact kind, stable key, and
// full payload, so the hint covers identity plus state, tags, and attributes:
// an identity-only hint would mask drift that keeps the same resource. A
// payload-only contribution for warning facts keeps them comparable too —
// warning stable keys embed the generation ID and could never match across
// ticks. Every entry is JSON-encoded before sorting, so the hint is
// insensitive to scanner emission order while nested attribute maps stay
// canonical through StableID's JSON encoding.
func awsFreshnessHint(envelopes []facts.Envelope) string {
	encoded := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		entry := map[string]any{
			"fact_kind": envelope.FactKind,
			"payload":   envelope.Payload,
		}
		if envelope.FactKind != cloud.AWSWarningFactKind {
			entry["stable_key"] = envelope.StableFactKey
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return ""
		}
		encoded = append(encoded, string(raw))
	}
	sort.Strings(encoded)
	return facts.StableID("aws.collection.freshness", map[string]any{"envelopes": encoded})
}
