// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"strings"
	"testing"
)

// TestParseEnvelopeRoundTrips proves the parser reads the canonical envelope
// emitted by `eshu first-run --json` (top-level data/truth/error).
func TestParseEnvelopeRoundTrips(t *testing.T) {
	raw := `{
  "data": {
    "command": "first-run",
    "runtime_shape": "local_binaries",
    "service_url": "http://localhost:8080",
    "repo_indexed": "complete",
    "repo_target": "/ws/demo",
    "readiness": "indexing complete",
    "query_answered": true,
    "query_summary": "repositories query returned 1 (e.g. demo)",
    "steps": []
  },
  "truth": {"level": "runtime", "completeness": "complete", "freshness": "current"},
  "error": null
}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope error: %v", err)
	}
	if !env.Data.QueryAnswered {
		t.Fatal("Data.QueryAnswered = false, want true")
	}
	if env.Truth["completeness"] != "complete" {
		t.Fatalf("Truth completeness = %v, want complete", env.Truth["completeness"])
	}
	if env.Error != nil {
		t.Fatalf("Error = %v, want nil", env.Error)
	}
}

// TestParseEnvelopeCapturesError proves an error envelope round-trips so a
// consumer can reject failed runs.
func TestParseEnvelopeCapturesError(t *testing.T) {
	raw := `{"data":{"command":"first-run","query_answered":false},"truth":{"completeness":"partial"},"error":{"message":"verify runtime: no reachable API"}}`
	env, err := ParseEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("ParseEnvelope error: %v", err)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "no reachable API") {
		t.Fatalf("Error = %v, want message containing 'no reachable API'", env.Error)
	}
}

// TestParseEnvelopeRejectsMalformedPayload proves malformed input fails loudly
// instead of being silently consumed, including type mismatches nested inside
// blocks a consumer never reads (steps, diagnosis). The decode must stay as
// strict as the canonical first-run envelope so a corrupt artifact cannot pass
// the benchmark or the evidence report by accident.
func TestParseEnvelopeRejectsMalformedPayload(t *testing.T) {
	cases := map[string]string{
		"not json":           `{"data":`,
		"steps wrong type":   `{"data":{"command":"first-run","steps":"broken"},"truth":{},"error":null}`,
		"diagnosis mistyped": `{"data":{"command":"first-run","diagnosis":{"class":5}},"truth":{},"error":null}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseEnvelope([]byte(raw)); err == nil {
				t.Fatalf("ParseEnvelope(%s) error = nil, want decode failure", name)
			}
		})
	}
}
