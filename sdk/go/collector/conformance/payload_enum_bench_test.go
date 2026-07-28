// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"encoding/json"
	"testing"
)

var benchmarkEnumMatchSink bool

func BenchmarkCandidateStringEnumLookup(b *testing.B) {
	enumValues := map[string]struct{}{
		"vex_statement":      {},
		"eshu_policy":        {},
		"provider_dismissal": {},
	}
	b.Run("property_without_enum", func(b *testing.B) {
		var noEnum map[string]struct{}
		for b.Loop() {
			if noEnum != nil {
				_, benchmarkEnumMatchSink = noEnum["eshu_policy"]
			}
		}
	})
	b.Run("property_with_enum", func(b *testing.B) {
		for b.Loop() {
			_, benchmarkEnumMatchSink = enumValues["eshu_policy"]
		}
	})
}

func BenchmarkPayloadSchemaValidationWithStringEnum(b *testing.B) {
	schema, err := compileSchema(json.RawMessage(`{
      "type": "object",
      "additionalProperties": true,
      "required": ["source", "justification"],
      "properties": {
        "source": {"type": "string", "enum": ["vex_statement", "eshu_policy", "provider_dismissal"]},
        "justification": {"type": "string", "enum": ["not_affected", "accepted_risk", "false_positive", "ignored", "provider_dismissed"]}
      }
    }`))
	if err != nil {
		b.Fatalf("compileSchema() error = %v", err)
	}
	payload := map[string]any{
		"source":        "eshu_policy",
		"justification": "accepted_risk",
	}
	b.ReportAllocs()
	for b.Loop() {
		benchmarkValidationErrSink = schema.validatePayload(payload)
		if benchmarkValidationErrSink != nil {
			b.Fatalf("validatePayload() error = %v", benchmarkValidationErrSink)
		}
	}
}
